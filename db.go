package db

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

// Main public interface of this key-value database.
type DB interface {
	Get(key []byte) (io.Reader, error)
	Put(key []byte, data io.Reader) error
}

type database struct {
	indexFile   *os.File // index file
	dataFile    *os.File // data file
	keyMinSize  int
	keyMaxSize  int
	keyIterator func(key []byte) iter.Seq2[int, byte]
	newTrieNode func() trieNode
	trieType    TrieType
	trieSize    int64
}

type trieNode interface {
	GetVariants() []int64
}

// 4-bit prefix tree node
type trieNode4 struct {
	Variants [16]int64
}

const int64Size = 8

func (t *trieNode4) GetVariants() []int64 {
	return t.Variants[:]
}

// 8-bit prefix tree node
type trieNode8 struct {
	Variants [256]int64
}

func (t *trieNode8) GetVariants() []int64 {
	return t.Variants[:]
}

var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyAlreadyExists = errors.New("key already exists")
	ErrInvalidKeySize   = errors.New("key size invalid")
	ErrCorruptedIndex   = errors.New("corrupted index file")
)

func keyIter4Bit(key []byte) iter.Seq2[int, byte] {
	return func(yield func(int, byte) bool) {
		for i, b := range key {
			if !yield(i*2, b>>4) {
				break
			}
			if !yield(i*2+1, b&0xF) {
				break
			}
		}
	}
}

func keyIter8Bit(key []byte) iter.Seq2[int, byte] {
	return func(yield func(int, byte) bool) {
		for i, b := range key {
			if !yield(i, b) {
				break
			}
		}
	}
}

func NewDatabase(name, dir string, keyMinSize, keyMaxSize int, trieType TrieType) (DB, error) {
	if keyMinSize < 1 || keyMaxSize < 1 || keyMinSize > keyMaxSize {
		return nil, errors.New("wrong key limits")
	}

	var keyIter func(key []byte) iter.Seq2[int, byte]
	var newTrieNode func() trieNode
	switch trieType {
	case TrieType4Bit:
		keyIter = keyIter4Bit
		newTrieNode = func() trieNode { return &trieNode4{} }
	case TrieType8Bit:
		keyIter = keyIter8Bit
		newTrieNode = func() trieNode { return &trieNode8{} }
	default:
		return nil, fmt.Errorf("illegal TrieType value %d", trieType)
	}

	trieSize := int64(reflect.TypeOf(newTrieNode()).Elem().Size())

	idx, err := os.OpenFile(filepath.Join(dir, name+".idx"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	fi, err := idx.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		rootNode := newTrieNode()
		binary.Write(idx, binary.LittleEndian, rootNode)
	} else if fi.Size()%trieSize != 0 {
		return nil, ErrCorruptedIndex
	}

	data, err := os.OpenFile(filepath.Join(dir, name+".dat"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return &database{
		indexFile:   idx,
		dataFile:    data,
		keyMinSize:  keyMinSize,
		keyMaxSize:  keyMaxSize,
		keyIterator: keyIter,
		newTrieNode: newTrieNode,
		trieType:    trieType,
		trieSize:    trieSize,
	}, nil
}

// Get implements DB interface
func (d *database) Get(key []byte) (io.Reader, error) {
	// intentionally less strict key size validation
	if len(key) > d.keyMaxSize {
		return nil, ErrInvalidKeySize
	}

	var index int64
	var node trieNode = d.newTrieNode()

	for _, b := range d.keyIterator(key) {
		_, err := d.indexFile.Seek(index*d.trieSize, io.SeekStart)
		if err != nil {
			return nil, err
		}

		err = binary.Read(d.indexFile, binary.LittleEndian, node)
		if err != nil {
			return nil, err
		}

		v := node.GetVariants()[b]

		if v > 0 {
			index = v
			continue
		}

		if v == 0 {
			return nil, ErrKeyNotFound
		}

		// decoded offset of data block
		dataOffset := -v - 1

		fullKey, err := d.readKeyInData(dataOffset)
		if err != nil {
			return nil, err
		}

		if !slices.Equal(key, fullKey[:len(key)]) {
			return nil, ErrKeyNotFound
		}

		var size int64
		err = binary.Read(d.dataFile, binary.LittleEndian, &size)
		if err != nil {
			return nil, err
		}

		return io.NewSectionReader(d.dataFile, dataOffset+1+int64(len(fullKey))+int64Size, size), nil
	}

	return nil, ErrCorruptedIndex
}

// Put implements DB interface
func (d *database) Put(key []byte, data io.Reader) error {
	// strict key size validation
	if len(key) < d.keyMinSize || len(key) > d.keyMaxSize {
		return ErrInvalidKeySize
	}

	// get file info of the data file
	fi, err := d.dataFile.Stat()
	if err != nil {
		return err
	}

	dataOffset := fi.Size()

	// store the key in the index file
	err = d.putKey(key, dataOffset)
	if err != nil {
		return err
	}

	// append the data with header into the data file
	return d.putData(key, data)
}

func (d *database) putData(key []byte, data io.Reader) error {
	// header format
	// 0: key size as 0 -> keyMinSize, 1 -> keyMinSize+1, 2 -> keyMinSize+2 ... 255 -> keyMinSize+255
	// 1..len(key)+1: full key
	// len(key)+2..len(key)+2+int64Size: data size
	dataHeader := make([]byte, 1+len(key)+int64Size)

	// Set key size encoding at position 0
	dataHeader[0] = byte(len(key) - d.keyMinSize)

	// Copy key bytes starting at position 1
	copy(dataHeader[1:], key)

	// prepare for writing the new data by seeking end of the data file
	_, err := d.dataFile.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	// write data header to data file first
	_, err = d.dataFile.Write(dataHeader)
	if err != nil {
		return err
	}

	// copy the data and take its size
	size, err := io.Copy(d.dataFile, data)
	if err != nil {
		return err
	}

	// seek back to the reserved space for the data size
	_, err = d.dataFile.Seek(-(size + int64Size), io.SeekCurrent)
	if err != nil {
		return err
	}

	// write the data size in the reserved space
	err = binary.Write(d.dataFile, binary.LittleEndian, size)
	if err != nil {
		return err
	}

	return d.dataFile.Sync()
}

// Put hash key into the index file and store the offset of the data in the data file
// in the last byte of the hash key.
func (d *database) putKey(key []byte, dataOffset int64) error {
	var (
		key2        []byte
		dataOffset2 int64
		node        trieNode = d.newTrieNode()
		index       int64
	)

	// we can't use possible zero and any positive value, so we encode this offset
	// as negative an smaller by one
	dataOffset = -dataOffset - 1

	for i, k := range d.keyIterator(key) {
		if key2 == nil {
			err := d.readIndex(node, index)
			if err != nil {
				return err
			}

			if node.GetVariants()[k] > 0 {
				index = node.GetVariants()[k]
				continue
			} else if node.GetVariants()[k] < 0 {
				dataOffset2 = node.GetVariants()[k]
				key2, err = d.readKeyInData(-dataOffset2 - 1)
				if err != nil {
					return err
				}
				if slices.Equal(key, key2) {
					return ErrKeyAlreadyExists
				}
			}
		} else {
			node = d.newTrieNode()
			err := d.appendIndex(node)
			if err != nil {
				return err
			}
		}

		var k2 byte

		if key2 != nil {
			k2 = d.nibble(key2, i)
			if k == k2 {
				fi, err := d.indexFile.Stat()
				if err != nil {
					return err
				}
				index = fi.Size() / d.trieSize

				node.GetVariants()[k] = index
				err = d.rewritePreviousNode(node)
				if err != nil {
					return err
				}
				continue
			}
		}

		node.GetVariants()[k] = dataOffset
		if key2 != nil {
			node.GetVariants()[k2] = dataOffset2
		}

		err := d.rewritePreviousNode(node)
		if err != nil {
			return err
		}
		break
	}

	return d.indexFile.Sync()
}

func (d *database) nibble(key []byte, idx int) byte {
	if d.trieType == TrieType8Bit {
		return key[idx]
	}

	i := idx / 2
	r := idx % 2
	if r == 0 {
		return key[i] >> 4
	}

	return key[i] & 0xF
}

func (d *database) readIndex(node trieNode, index int64) error {
	_, err := d.indexFile.Seek(index*d.trieSize, io.SeekStart)
	if err != nil {
		return err
	}
	err = binary.Read(d.indexFile, binary.LittleEndian, node)
	if err != nil {
		return err
	}
	return nil
}

func (d *database) appendIndex(node trieNode) error {
	_, err := d.indexFile.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	err = binary.Write(d.indexFile, binary.LittleEndian, node)
	if err != nil {
		return err
	}
	return nil
}

func (d *database) readKeyInData(dataOffset int64) ([]byte, error) {
	_, err := d.dataFile.Seek(dataOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	var kSize byte
	if err := binary.Read(d.dataFile, binary.LittleEndian, &kSize); err != nil {
		return nil, err
	}
	// define as int because value could be bigger than max byte
	keySize := int(kSize) + d.keyMinSize

	var key = make([]byte, keySize)
	if err := binary.Read(d.dataFile, binary.LittleEndian, key); err != nil {
		return nil, err
	}

	return key, nil
}

func (d *database) rewritePreviousNode(node trieNode) error {
	_, err := d.indexFile.Seek(-d.trieSize, io.SeekCurrent)
	if err != nil {
		return err
	}
	err = binary.Write(d.indexFile, binary.LittleEndian, node)
	if err != nil {
		return err
	}
	return nil
}

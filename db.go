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
	keySize     int
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

func NewDatabase(name, dir string, keySize int, trieType TrieType) (DB, error) {
	var keyIter func(key []byte) iter.Seq2[int, byte]
	var trieNodeF func() trieNode
	switch trieType {
	case TrieType4Bit:
		keyIter = keyIter4Bit
		trieNodeF = func() trieNode { return &trieNode4{} }
	case TrieType8Bit:
		keyIter = keyIter8Bit
		trieNodeF = func() trieNode { return &trieNode8{} }
	default:
		return nil, fmt.Errorf("illegal TrieType value %d", trieType)
	}

	trieSize := int64(reflect.TypeOf(trieNodeF()).Elem().Size())

	idx, err := os.OpenFile(filepath.Join(dir, name+".idx"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	fi, err := idx.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		rootNode := trieNodeF()
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
		keySize:     keySize,
		keyIterator: keyIter,
		newTrieNode: trieNodeF,
		trieType:    trieType,
		trieSize:    trieSize,
	}, nil
}

// Get implements DB interface
func (d *database) Get(key []byte) (io.Reader, error) {
	// intentionally less strict key size validation
	if len(key) > d.keySize {
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

		dataOffset := -v
		_, err = d.dataFile.Seek(dataOffset, io.SeekStart)
		if err != nil {
			return nil, err
		}

		realKey := make([]byte, d.keySize)
		var size int64

		err = binary.Read(d.dataFile, binary.LittleEndian, realKey)
		if err != nil {
			return nil, err
		}

		if !slices.Equal(key, realKey[:len(key)]) {
			return nil, ErrKeyNotFound
		}

		err = binary.Read(d.dataFile, binary.LittleEndian, &size)
		if err != nil {
			return nil, err
		}

		return io.NewSectionReader(d.dataFile, dataOffset+int64(len(key)+int64Size), size), nil
	}

	return nil, ErrCorruptedIndex
}

// Put implements DB interface
func (d *database) Put(key []byte, data io.Reader) error {
	// strict key size validation
	if len(key) != d.keySize {
		return ErrInvalidKeySize
	}

	// get file info of the data file
	fi, err := d.dataFile.Stat()
	if err != nil {
		return err
	}

	dataOffset := fi.Size()
	if dataOffset == 0 {
		dataOffset = 1 // the very first byte of the data file is reserved
	}

	// create a new key with the offset of data to be saved later in the data file
	err = d.putKey(key, dataOffset)
	if err != nil {
		return err
	}

	_, err = d.dataFile.Seek(dataOffset, io.SeekStart)
	if err != nil {
		return err
	}

	// write the full key to data file first
	_, err = d.dataFile.Write(key)
	if err != nil {
		return err
	}

	// reserve space for the data size
	_, err = d.dataFile.Seek(int64Size, io.SeekCurrent)
	if err != nil {
		return err
	}

	// copy the data and take its size
	size, err := io.Copy(d.dataFile, data)
	if err != nil {
		return err
	}

	// seek back to the reserved space for the data size
	_, err = d.dataFile.Seek(dataOffset+int64(d.keySize), io.SeekStart)
	if err != nil {
		return err
	}

	// write the data size in the reserved space
	err = binary.Write(d.dataFile, binary.LittleEndian, size)
	if err != nil {
		return err
	}

	// seek forward to the end of the data file
	_, err = d.dataFile.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	return nil
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

	dataOffset = -dataOffset // always encode offset in the index node as negative value

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
				key2, err = d.readKeyInData(-dataOffset2)
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

	return nil
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
	var key = make([]byte, d.keySize)

	_, err := d.dataFile.Seek(dataOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}
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

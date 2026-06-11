package db

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Main public interface of this key-value database.
type DB interface {
	Get(key []byte) (io.Reader, error)
	Put(key []byte, data io.Reader) error
	Close() error
}

type database struct {
	indexFile *os.File
	dataFile  *os.File
	trieType  TrieType
	mu        sync.RWMutex
}

const int64Size = 8

var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyEmpty         = errors.New("key is empty")
	ErrKeyTooBig        = errors.New("key is too big")
	ErrKeyAlreadyExists = errors.New("key already exists")
	ErrKeyConflict      = errors.New("existing key is a prefix of the new longer key")
	ErrCorruptedIndex   = errors.New("corrupted index file")
)

func NewDatabase(name, dir string, trieType TrieType) (DB, error) {
	idx, err := os.OpenFile(filepath.Join(dir, name+".idx"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	fi, err := idx.Stat()
	if err != nil {
		idx.Close()
		return nil, err
	}
	if fi.Size() == 0 {
		rootNode := trieType.NewTrieNode()
		if err := binary.Write(idx, binary.LittleEndian, rootNode); err != nil {
			idx.Close()
			return nil, err
		}
		if err := idx.Sync(); err != nil {
			idx.Close()
			return nil, err
		}
	} else if fi.Size()%trieType.NodeSize() != 0 {
		idx.Close()
		return nil, ErrCorruptedIndex
	}

	data, err := os.OpenFile(filepath.Join(dir, name+".dat"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		idx.Close()
		return nil, err
	}
	return &database{
		indexFile: idx,
		dataFile:  data,
		trieType:  trieType,
	}, nil
}

// Get looks up a key and returns an io.SectionReader backed by the underlying data file.
// The returned reader uses ReadAt (positional reads) and is safe to use concurrently
// with Put calls. If a Delete function is added, this io.SectionReader could become
// unsafe to use concurrently.
func (d *database) Get(key []byte) (io.Reader, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(key) == 0 {
		return nil, ErrKeyEmpty
	}
	if len(key) > 256 {
		return nil, ErrKeyTooBig
	}

	var nextIndex int64
	var node trieNode = d.trieType.NewTrieNode()

	for _, b := range node.keyIter(key) {
		if err := d.readIndex(node, nextIndex); err != nil {
			return nil, err
		}

		v := node.getVariants()[b]

		if v > 0 {
			nextIndex = v
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

		if len(fullKey) < len(key) || !slices.Equal(key, fullKey[:len(key)]) {
			return nil, ErrKeyNotFound
		}

		var sizeBuf [8]byte
		sizeOffset := dataOffset + 1 + int64(len(fullKey))
		if _, err := d.dataFile.ReadAt(sizeBuf[:], sizeOffset); err != nil {
			return nil, err
		}
		size := int64(binary.LittleEndian.Uint64(sizeBuf[:]))

		return io.NewSectionReader(d.dataFile, sizeOffset+int64Size, size), nil
	}

	return nil, ErrCorruptedIndex
}

// Put implements DB interface
func (d *database) Put(key []byte, data io.Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(key) == 0 {
		return ErrKeyEmpty
	}
	if len(key) > 256 {
		return ErrKeyTooBig
	}

	// get file info of the data file
	fi, err := d.dataFile.Stat()
	if err != nil {
		return err
	}

	dataOffset := fi.Size()

	// append the data with header into the data file first
	// so the index never points to missing data after a crash
	if err := d.putData(key, data); err != nil {
		return err
	}

	// store the key in the index file
	return d.putKey(key, dataOffset)
}

func (d *database) putData(key []byte, data io.Reader) error {
	// header format
	// 0: key size minus one, supports actual sizes from 1 up to 256, i.e. up to 2048 bit
	// 1..len(key)+1: full key
	// len(key)+2..len(key)+2+int64Size: data size
	dataHeader := make([]byte, 1+len(key)+int64Size)

	// Set key size encoding at position 0
	dataHeader[0] = byte(len(key) - 1)

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
		key2         []byte
		dataOffset2  int64
		node         trieNode = d.trieType.NewTrieNode()
		nextIndex    int64
		currentIndex int64
	)

	// we can't use possible zero and any positive value, so we encode this offset
	// as negative an smaller by one
	dataOffset = -dataOffset - 1

	for i, k := range node.keyIter(key) {
		if key2 == nil {
			err := d.readIndex(node, nextIndex)
			if err != nil {
				return err
			}
			currentIndex = nextIndex

			if node.getVariants()[k] > 0 {
				nextIndex = node.getVariants()[k]
				continue
			} else if node.getVariants()[k] < 0 {
				dataOffset2 = node.getVariants()[k]
				key2, err = d.readKeyInData(-dataOffset2 - 1)
				if err != nil {
					return err
				}
				if slices.Equal(key, key2) {
					return ErrKeyAlreadyExists
				}
				// The existing key is an exact prefix of the new longer key.
				// The trie does not support inserting a longer key when its
				// prefix already exists as a separate key.
				if len(key) > len(key2) {
					return ErrKeyConflict
				}
			}
		} else {
			node = d.trieType.NewTrieNode()
			var err error
			currentIndex, err = d.appendIndex(node)
			if err != nil {
				return err
			}
		}

		var k2 byte

		if key2 != nil {
			k2 = d.keySymbolAt(key2, i)
			if k == k2 {
				fi, err := d.indexFile.Stat()
				if err != nil {
					return err
				}
				nextIndex = fi.Size() / d.trieType.NodeSize()

				node.getVariants()[k] = nextIndex
				err = d.rewriteNode(node, currentIndex)
				if err != nil {
					return err
				}
				continue
			}
		}

		node.getVariants()[k] = dataOffset
		if key2 != nil {
			node.getVariants()[k2] = dataOffset2
		}

		err := d.rewriteNode(node, currentIndex)
		if err != nil {
			return err
		}
		break
	}

	return d.indexFile.Sync()
}

func (d *database) keySymbolAt(key []byte, idx int) byte {
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
	buf := make([]byte, d.trieType.NodeSize())
	if _, err := d.indexFile.ReadAt(buf, index*d.trieType.NodeSize()); err != nil {
		return err
	}
	return binary.Read(bytes.NewReader(buf), binary.LittleEndian, node)
}

func (d *database) appendIndex(node trieNode) (int64, error) {
	fi, err := d.indexFile.Stat()
	if err != nil {
		return 0, err
	}
	index := fi.Size() / d.trieType.NodeSize()

	if _, err := d.indexFile.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	if err := binary.Write(d.indexFile, binary.LittleEndian, node); err != nil {
		return 0, err
	}
	return index, nil
}

func (d *database) readKeyInData(dataOffset int64) ([]byte, error) {
	var kSize [1]byte
	if _, err := d.dataFile.ReadAt(kSize[:], dataOffset); err != nil {
		return nil, err
	}
	keySize := int(kSize[0]) + 1

	var key = make([]byte, keySize)
	if _, err := d.dataFile.ReadAt(key, dataOffset+1); err != nil {
		return nil, err
	}

	return key, nil
}

func (d *database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return errors.Join(d.indexFile.Close(), d.dataFile.Close())
}

func (d *database) rewriteNode(node trieNode, index int64) error {
	_, err := d.indexFile.Seek(index*d.trieType.NodeSize(), io.SeekStart)
	if err != nil {
		return err
	}
	return binary.Write(d.indexFile, binary.LittleEndian, node)
}

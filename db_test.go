package db

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func newTestDB(t *testing.T, trieType TrieType) DB {
	t.Helper()
	dir := t.TempDir()
	db, err := NewDatabase("test", dir, trieType)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDatabase_GetKeyNotFound(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	_, err := db.Get([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	if err != ErrKeyNotFound {
		t.Fatalf("expected %v, got %v", ErrKeyNotFound, err)
	}
}

func TestDatabase_PutAndGetExactKey(t *testing.T) {
	for _, trieType := range []TrieType{TrieType4Bit, TrieType8Bit} {
		t.Run(reflect.TypeOf(trieType.NewTrieNode()).Elem().Name(), func(t *testing.T) {
			db := newTestDB(t, trieType)

			key := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80}
			value := []byte("hello world")

			if err := db.Put(key, bytes.NewReader(value)); err != nil {
				t.Fatalf("Put failed: %v", err)
			}

			r, err := db.Get(key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}

			if got := readAll(t, r); !bytes.Equal(got, value) {
				t.Fatalf("expected %q, got %q", value, got)
			}
		})
	}
}

func TestDatabase_GetByUniquePrefix(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	key := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x10, 0x20, 0x30, 0x40}
	value := []byte("value")
	if err := db.Put(key, bytes.NewReader(value)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	r, err := db.Get([]byte{0xAA, 0xBB, 0xCC})
	if err != nil {
		t.Fatalf("Get by prefix failed: %v", err)
	}

	if got := readAll(t, r); !bytes.Equal(got, value) {
		t.Fatalf("expected %q, got %q", value, got)
	}
}

func TestDatabase_GetPrefixNotFound(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	if err := db.Put([]byte{0x01, 0x02, 0x03, 0x04, 0x05}, bytes.NewReader([]byte("one"))); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	_, err := db.Get([]byte{0xFF, 0xEE, 0xDD})
	if err != ErrKeyNotFound {
		t.Fatalf("expected %v, got %v", ErrKeyNotFound, err)
	}
}

func TestDatabase_PutDuplicateKey(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	key := []byte{0x21, 0x22, 0x23, 0x24, 0x25}
	if err := db.Put(key, bytes.NewReader([]byte("first"))); err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	err := db.Put(key, bytes.NewReader([]byte("second")))
	if err != ErrKeyAlreadyExists {
		t.Fatalf("expected %v, got %v", ErrKeyAlreadyExists, err)
	}
}

func TestDatabase_KeyValidation(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	t.Run("empty key put", func(t *testing.T) {
		if err := db.Put(nil, bytes.NewReader([]byte("x"))); err != ErrKeyEmpty {
			t.Fatalf("expected %v, got %v", ErrKeyEmpty, err)
		}
	})

	t.Run("empty key get", func(t *testing.T) {
		if _, err := db.Get(nil); err != ErrKeyEmpty {
			t.Fatalf("expected %v, got %v", ErrKeyEmpty, err)
		}
	})

	t.Run("256-byte key accepted", func(t *testing.T) {
		key := bytes.Repeat([]byte{0xAB}, 256)
		value := []byte("max-key")
		if err := db.Put(key, bytes.NewReader(value)); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		r, err := db.Get(key[:32])
		if err != nil {
			t.Fatalf("Get by unique prefix failed: %v", err)
		}
		if got := readAll(t, r); !bytes.Equal(got, value) {
			t.Fatalf("expected %q, got %q", value, got)
		}
	})

	t.Run("257-byte key rejected on put", func(t *testing.T) {
		key := bytes.Repeat([]byte{0xCD}, 257)
		if err := db.Put(key, bytes.NewReader([]byte("x"))); err != ErrKeyTooBig {
			t.Fatalf("expected %v, got %v", ErrKeyTooBig, err)
		}
	})

	t.Run("257-byte key rejected on get", func(t *testing.T) {
		key := bytes.Repeat([]byte{0xCD}, 257)
		if _, err := db.Get(key); err != ErrKeyTooBig {
			t.Fatalf("expected %v, got %v", ErrKeyTooBig, err)
		}
	})
}

func TestDatabase_ReopenExistingFiles(t *testing.T) {
	dir := t.TempDir()

	db1, err := NewDatabase("test", dir, TrieType8Bit)
	if err != nil {
		t.Fatalf("first NewDatabase failed: %v", err)
	}

	key := []byte{0x90, 0x91, 0x92, 0x93, 0x94, 0x95}
	value := []byte("persistent-data")
	if err := db1.Put(key, bytes.NewReader(value)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := db1.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	db2, err := NewDatabase("test", dir, TrieType8Bit)
	if err != nil {
		t.Fatalf("second NewDatabase failed: %v", err)
	}
	defer db2.Close()

	r, err := db2.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got := readAll(t, r); !bytes.Equal(got, value) {
		t.Fatalf("expected %q, got %q", value, got)
	}
}

func TestDatabase_CorruptedIndex(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "test.idx")

	f, err := os.Create(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1, 2, 3, 4, 5, 6, 7}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = NewDatabase("test", dir, TrieType8Bit)
	if err != ErrCorruptedIndex {
		t.Fatalf("expected %v, got %v", ErrCorruptedIndex, err)
	}
}

func TestDatabase_NodeExpansionAndRetrieval(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	keys := [][]byte{
		{0xAA, 0x00, 0x10, 0x01},
		{0xAA, 0x01, 0x10, 0x02},
		{0xAA, 0x02, 0x10, 0x03},
		{0xAA, 0x03, 0x10, 0x04},
		{0xAA, 0x04, 0x10, 0x05},
		{0xAA, 0x05, 0x10, 0x06},
	}

	for i, key := range keys {
		value := []byte{byte('0' + i)}
		if err := db.Put(key, bytes.NewReader(value)); err != nil {
			t.Fatalf("Put failed for %q: %v", key, err)
		}
	}

	for i, key := range keys {
		r, err := db.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %q: %v", key, err)
		}
		expected := []byte{byte('0' + i)}
		if got := readAll(t, r); !bytes.Equal(got, expected) {
			t.Fatalf("for key %q expected %q, got %q", key, expected, got)
		}
	}
}

func TestDatabase_PutLongerKeyAfterShorterKey(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	// Insert a shorter key first
	shortKey := []byte{0xAA, 0xBB}
	if err := db.Put(shortKey, bytes.NewReader([]byte("short"))); err != nil {
		t.Fatalf("Put short key failed: %v", err)
	}

	// Inserting a longer key with the same prefix as an existing shorter key
	// is not supported — the trie cannot split a data pointer into a child
	// node when the existing key is shorter than the new key.
	longKey := []byte{0xAA, 0xBB, 0xCC}
	err := db.Put(longKey, bytes.NewReader([]byte("long")))
	if err != ErrKeyConflict {
		t.Fatalf("expected %v, got %v", ErrKeyConflict, err)
	}

	// Verify the original short key is still retrievable
	r, err := db.Get(shortKey)
	if err != nil {
		t.Fatalf("Get short key failed: %v", err)
	}
	if got := readAll(t, r); !bytes.Equal(got, []byte("short")) {
		t.Fatalf("short key: expected %q, got %q", "short", got)
	}
}

func TestDatabase_ConcurrentReadsAndWrites(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	// Pre-populate some keys
	const numStaticKeys = 10
	for i := 0; i < numStaticKeys; i++ {
		key := []byte{0xAA, byte(i)}
		val := []byte{byte('A' + i)}
		if err := db.Put(key, bytes.NewReader(val)); err != nil {
			t.Fatalf("pre-population failed for index %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	const numReaders = 20
	const numWriters = 5
	const iterations = 50

	errc := make(chan error)

	// Start concurrent readers
	for readerID := range numReaders {
		wg.Go(func() {
			for iter := 0; iter < iterations; iter++ {
				// Query static keys
				for i := 0; i < numStaticKeys; i++ {
					key := []byte{0xAA, byte(i)}
					expectedVal := []byte{byte('A' + i)}
					reader, err := db.Get(key)
					if err != nil {
						errc <- fmt.Errorf("[Reader %d] Get key %v failed: %w", readerID, key, err)
						return
					}
					got, err := io.ReadAll(reader)
					if err != nil {
						errc <- fmt.Errorf("[Reader %d] ReadAll failed: %w", readerID, err)
						return
					}
					if !bytes.Equal(got, expectedVal) {
						errc <- fmt.Errorf("[Reader %d] key %v: expected %v, got %v", readerID, key, expectedVal, got)
						return
					}
				}
			}
		})
	}

	// Start concurrent writers (adding new/unique keys)
	for writerID := range numWriters {
		wg.Go(func() {
			for iter := 0; iter < iterations; iter++ {
				key := []byte{0xBB, byte(writerID), byte(iter)}
				val := []byte{byte(writerID), byte(iter)}
				if err := db.Put(key, bytes.NewReader(val)); err != nil {
					errc <- fmt.Errorf("[Writer %d] Put failed: %w", writerID, err)
					return
				}

				// Verify we can read what we just wrote
				reader, err := db.Get(key)
				if err != nil {
					errc <- fmt.Errorf("[Writer %d] Get failed for written key: %w", writerID, err)
					return
				}
				got, err := io.ReadAll(reader)
				if err != nil {
					errc <- fmt.Errorf("[Writer %d] ReadAll failed: %w", writerID, err)
					return
				}
				if !bytes.Equal(got, val) {
					errc <- fmt.Errorf("[Writer %d] expected %v, got %v", writerID, val, got)
					return
				}
			}
		})
	}

	// Close errc once all goroutines finish so the collector loop can exit.
	go func() {
		wg.Wait()
		close(errc)
	}()

	// Collect and report all errors from the test goroutine.
	for err := range errc {
		t.Error(err)
	}
}

func TestDatabase_GetLongerKeyAfterShorterKey(t *testing.T) {
	db := newTestDB(t, TrieType8Bit)

	shortKey := []byte{0xAA, 0xBB}
	if err := db.Put(shortKey, bytes.NewReader([]byte("short"))); err != nil {
		t.Fatalf("Put short key failed: %v", err)
	}

	// Query with a longer key that has the short key as a prefix
	longKey := []byte{0xAA, 0xBB, 0xCC}
	_, err := db.Get(longKey)
	if err != ErrKeyNotFound {
		t.Fatalf("expected %v, got %v", ErrKeyNotFound, err)
	}

	// Verify short key is still retrievable
	r, err := db.Get(shortKey)
	if err != nil {
		t.Fatalf("Get short key failed: %v", err)
	}
	if got := readAll(t, r); !bytes.Equal(got, []byte("short")) {
		t.Fatalf("short key: expected %q, got %q", "short", got)
	}
}

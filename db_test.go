package db

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T, trieType TrieType) DB {
	t.Helper()
	dir := t.TempDir()
	db, err := NewDatabase("test", dir, trieType)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Run(string(trieType), func(t *testing.T) {
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

	db2, err := NewDatabase("test", dir, TrieType8Bit)
	if err != nil {
		t.Fatalf("second NewDatabase failed: %v", err)
	}

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

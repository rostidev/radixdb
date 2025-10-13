package db

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const regularKeySize = 4

func newTestDB(t *testing.T, keySize int, trieType TrieType) DB {
	dir := t.TempDir()
	db, err := NewDatabase("test", dir, keySize, trieType)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGet_KeyNotFound(t *testing.T) {
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	_, err := db.Get([]byte("abcd"))
	if err != ErrKeyNotFound {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}

	key := []byte("test")
	data := []byte("hello world")
	err = db.Put(key, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Get([]byte("abcd"))
	if err != ErrKeyNotFound {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestPutAndGet(t *testing.T) {
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	key := []byte("test")
	data := []byte("hello world")
	err := db.Put(key, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	reader, err := db.Get(key)
	if err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("expected %s, got %s", data, got)
	}
}

func TestPut_ExistingKey(t *testing.T) {
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	key := []byte("test")
	data1 := []byte("first")
	data2 := []byte("second")
	err := db.Put(key, bytes.NewReader(data1))
	if err != nil {
		t.Fatal(err)
	}

	err = db.Put(key, bytes.NewReader(data2))
	if err != ErrKeyAlreadyExists {
		t.Errorf("expected ErrKeyAlreadyExists, got %v", err)
	}
}

func TestPut_InvalidKeySize(t *testing.T) {
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	key := []byte("abc") // too short, should be 4
	data := []byte("data")
	err := db.Put(key, bytes.NewReader(data))
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestGet_InvalidKeySize(t *testing.T) {
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	key := []byte("abcde") // too long
	_, err := db.Get(key)
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestTrieTypes(t *testing.T) {
	trieTypes := []TrieType{TrieType4Bit, TrieType8Bit}
	names := []string{"4Bit", "8Bit"}

	for i, trieType := range trieTypes {
		t.Run(names[i], func(t *testing.T) {
			db := newTestDB(t, regularKeySize, trieType)

			key := []byte("test")
			data := []byte("data")
			err := db.Put(key, bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}

			reader, err := db.Get(key)
			if err != nil {
				t.Fatal(err)
			}

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got, data) {
				t.Errorf("expected %s, got %s", data, got)
			}
		})
	}
}

func TestDatabase_4BitTrieNibbleOperations(t *testing.T) {
	// Test 4-bit trie specifically to exercise nibble() function
	db := newTestDB(t, regularKeySize, TrieType4Bit)

	// Use keys that will exercise different nibble patterns
	// This ensures the 4-bit nibble extraction logic is tested
	testCases := []struct {
		key  []byte
		data string
	}{
		{[]byte{0xAB, 0xCD, 0xEF, 0x12}, "data1"}, // High nibbles: A, C, E, 1; Low nibbles: B, D, F, 2
		{[]byte{0x12, 0x34, 0x56, 0x78}, "data2"}, // Different bit patterns
		{[]byte{0xFF, 0x00, 0xAA, 0x55}, "data3"}, // Edge case patterns
	}

	for _, tc := range testCases {
		err := db.Put(tc.key, bytes.NewReader([]byte(tc.data)))
		if err != nil {
			t.Fatalf("Put failed for key %x: %v", tc.key, err)
		}

		reader, err := db.Get(tc.key)
		if err != nil {
			t.Fatalf("Get failed for key %x: %v", tc.key, err)
		}

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Read failed for key %x: %v", tc.key, err)
		}

		if string(result) != tc.data {
			t.Errorf("Data mismatch for key %x: expected %s, got %s", tc.key, tc.data, string(result))
		}
	}
}

func TestDatabase_4BitVs8BitConsistency(t *testing.T) {
	// Test that both trie types produce consistent results for same operations
	key := []byte("test") // Must be exactly regularKeySize (4) bytes
	data := []byte("consistent_data")

	for _, trieType := range []TrieType{TrieType4Bit, TrieType8Bit} {
		t.Run(fmt.Sprintf("TrieType_%d", trieType), func(t *testing.T) {
			db := newTestDB(t, regularKeySize, trieType)

			err := db.Put(key, bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Put failed: %v", err)
			}

			reader, err := db.Get(key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}

			result, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("Read failed: %v", err)
			}

			if !bytes.Equal(result, data) {
				t.Errorf("Data inconsistency: expected %x, got %x", data, result)
			}
		})
	}
}

func TestNewDatabase_CorruptedIndex(t *testing.T) {
	// Test corrupted index file detection
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "test.idx")

	// Create corrupted index file (wrong size - not multiple of trieSize)
	file, err := os.Create(idxPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write invalid data (7 bytes - neither 16 nor 256 bytes for trie nodes)
	invalidData := make([]byte, 7)
	_, err = file.Write(invalidData)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()

	// Attempt to open database should fail with corrupted index error
	_, err = NewDatabase("test", dir, regularKeySize, TrieType8Bit)
	if err == nil {
		t.Error("expected error for corrupted index file, got nil")
	}
	if err != ErrCorruptedIndex {
		t.Errorf("expected ErrCorruptedIndex, got %v", err)
	}
}

func TestNewDatabase_ExistingValidIndex(t *testing.T) {
	// Test reusing existing valid index file
	dir := t.TempDir()

	// First instance - creates database and index
	db1, err := NewDatabase("test", dir, regularKeySize, TrieType8Bit)
	if err != nil {
		t.Fatalf("First NewDatabase failed: %v", err)
	}

	// Add some data
	key := []byte("test") // Must be exactly regularKeySize (4) bytes
	data := []byte("persistent_data")
	err = db1.Put(key, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Second instance - should reuse existing index and data
	db2, err := NewDatabase("test", dir, regularKeySize, TrieType8Bit)
	if err != nil {
		t.Fatalf("Second NewDatabase failed: %v", err)
	}

	// Verify data persists across database instances
	reader, err := db2.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("Data persistence failed: expected %x, got %x", data, result)
	}
}

func TestDatabase_TrieNodeSplitting(t *testing.T) {
	// Test trie node splitting by creating many entries that force new nodes
	db := newTestDB(t, regularKeySize, TrieType8Bit)

	// Generate keys that will cause trie node splitting
	// Use sequential keys to ensure trie depth increases
	keyCount := 200
	keys := make([][]byte, keyCount)
	values := make([][]byte, keyCount)

	for i := 0; i < keyCount; i++ {
		// Create keys that will exercise different trie paths
		key := make([]byte, regularKeySize)
		// Use different byte patterns to create varied trie paths
		key[0] = byte(i % 256)
		key[1] = byte((i / 256) % 256)
		key[2] = byte(i % 16) // Limited range to force collisions
		key[3] = byte(i % 8)  // Very limited range to force deep collisions

		keys[i] = key
		values[i] = []byte(fmt.Sprintf("value-%d", i))
	}

	// Insert all entries - this should trigger trie node splitting
	for i, key := range keys {
		err := db.Put(key, bytes.NewReader(values[i]))
		if err != nil {
			t.Fatalf("Put failed for key %d (%x): %v", i, key, err)
		}
	}

	// Verify all entries are retrievable
	for i, key := range keys {
		reader, err := db.Get(key)
		if err != nil {
			t.Fatalf("Get failed for key %d (%x): %v", i, key, err)
		}

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Read failed for key %d: %v", i, err)
		}

		expected := values[i]
		if !bytes.Equal(result, expected) {
			t.Errorf("Data mismatch for key %d: expected %s, got %s", i, expected, result)
		}
	}
}

func TestDatabase_HashCollisionHandling(t *testing.T) {
	// Test collision handling with small key space to force collisions
	smallKeySize := 1 // Only 256 possible keys (0-255)
	db := newTestDB(t, smallKeySize, TrieType8Bit)

	// Try to insert more entries than possible keys
	totalInserts := 300 // More than 256 possible keys
	keys := make([][]byte, totalInserts)
	values := make([][]byte, totalInserts)

	for i := 0; i < totalInserts; i++ {
		key := []byte{byte(i % 256)} // Cycle through 0-255
		keys[i] = key
		values[i] = []byte(fmt.Sprintf("data-%d", i))
	}

	// First 256 inserts should succeed (one per unique key)
	for i := 0; i < 256; i++ {
		err := db.Put(keys[i], bytes.NewReader(values[i]))
		if err != nil {
			t.Fatalf("First Put failed for key %d: %v", i, err)
		}
	}

	// Subsequent inserts with same keys should fail
	for i := 256; i < totalInserts; i++ {
		err := db.Put(keys[i], bytes.NewReader(values[i]))
		if err != ErrKeyAlreadyExists {
			t.Errorf("Expected ErrKeyAlreadyExists for duplicate key %d, got: %v", i, err)
		}
	}

	// Verify first 256 entries are still accessible
	for i := 0; i < 256; i++ {
		reader, err := db.Get(keys[i])
		if err != nil {
			t.Fatalf("Get failed for key %d: %v", i, err)
		}

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Read failed for key %d: %v", i, err)
		}

		if !bytes.Equal(result, values[i]) {
			t.Errorf("Data mismatch for key %d: expected %s, got %s", i, values[i], result)
		}
	}
}

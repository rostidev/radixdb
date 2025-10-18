package db

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Standard key size ranges for content-addressed database testing
const standardKeyMinSize = 8  // 8 bytes minimum: avoids weak short keys, supports 64-bit hashes
const standardKeyMaxSize = 32 // 32 bytes maximum: covers SHA-256, BLAKE2, future hash standards

func newTestDB(t *testing.T, keyMinSize, keyMaxSize int, trieType TrieType) DB {
	dir := t.TempDir()
	db, err := NewDatabase("test", dir, keyMinSize, keyMaxSize, trieType)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGet_KeyNotFound(t *testing.T) {
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)

	_, err := db.Get([]byte("abcdefgh")) // 8 bytes
	if err != ErrKeyNotFound {
		t.Errorf(`expected "%v", got "%v"`, ErrKeyNotFound, err)
	}

	key := []byte("testkey1") // 8 bytes
	data := []byte("hello world")
	err = db.Put(key, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Get([]byte("abcdefgh")) // 8 bytes
	if err != ErrKeyNotFound {
		t.Errorf(`expected "%v", got "%v"`, ErrKeyNotFound, err)
	}
}

func TestPutAndGet(t *testing.T) {
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)

	key := []byte("testkey1") // 8 bytes
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
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)

	key := []byte("testkey1") // 8 bytes
	data1 := []byte("first")
	data2 := []byte("second")
	err := db.Put(key, bytes.NewReader(data1))
	if err != nil {
		t.Fatal(err)
	}

	err = db.Put(key, bytes.NewReader(data2))
	if err != ErrKeyAlreadyExists {
		t.Errorf(`expected "%v", got "%v"`, ErrKeyAlreadyExists, err)
	}
}

func TestPut_InvalidKeySize(t *testing.T) {
	db := newTestDB(t, standardKeyMinSize, standardKeyMinSize, TrieType8Bit)

	key := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // 7 bytes, below minimum
	data := []byte("data")
	err := db.Put(key, bytes.NewReader(data))
	if err != ErrInvalidKeySize {
		t.Errorf(`expected "%v", got "%v"`, ErrInvalidKeySize, err)
	}
}

func TestGet_InvalidKeySize(t *testing.T) {
	db := newTestDB(t, standardKeyMinSize, standardKeyMinSize, TrieType8Bit)

	key := make([]byte, standardKeyMinSize+1) // Just over the maximum
	_, err := db.Get(key)
	if err != ErrInvalidKeySize {
		t.Errorf(`expected "%v", got "%v"`, ErrInvalidKeySize, err)
	}
}

func TestTrieTypes(t *testing.T) {
	trieTypes := []TrieType{TrieType4Bit, TrieType8Bit}
	names := []string{"4Bit", "8Bit"}

	for i, trieType := range trieTypes {
		t.Run(names[i], func(t *testing.T) {
			db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, trieType)

			key := []byte("testkey1") // 8 bytes
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
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType4Bit)

	// Use keys that will exercise different nibble patterns
	// This ensures the 4-bit nibble extraction logic is tested
	testCases := []struct {
		key  []byte
		data string
	}{
		{[]byte{0xAB, 0xCD, 0xEF, 0x12, 0x13, 0x24, 0x35, 0x46}, "data1"}, // High nibbles: A, C, E, 1, 1, 2, 3, 4; Low nibbles: B, D, F, 2, 3, 4, 5, 6
		{[]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}, "data2"}, // Sequential nibble patterns
		{[]byte{0xFF, 0x00, 0xAA, 0x55, 0xCC, 0x33, 0x99, 0x66}, "data3"}, // Mixed high/low nibble patterns
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
	key := []byte("testkey1") // 8 bytes, within standard key range
	data := []byte("consistent_data")

	for _, trieType := range []TrieType{TrieType4Bit, TrieType8Bit} {
		t.Run(fmt.Sprintf("TrieType_%d", trieType), func(t *testing.T) {
			db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, trieType)

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
	_, err = NewDatabase("test", dir, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
	if err == nil {
		t.Error("expected error for corrupted index file, got nil")
	}
	if err != ErrCorruptedIndex {
		t.Errorf(`expected "%v", got "%v"`, ErrCorruptedIndex, err)
	}
}

func TestNewDatabase_ExistingValidIndex(t *testing.T) {
	// Test reusing existing valid index file
	dir := t.TempDir()

	// First instance - creates database and index
	db1, err := NewDatabase("test", dir, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
	if err != nil {
		t.Fatalf("First NewDatabase failed: %v", err)
	}

	// Add some data
	key := []byte("testkey1") // 8 bytes, within standard key range
	data := []byte("persistent_data")
	err = db1.Put(key, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Second instance - should reuse existing index and data
	db2, err := NewDatabase("test", dir, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
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
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)

	// Generate keys that will cause trie node splitting
	// Use sequential keys to ensure trie depth increases
	keyCount := 200
	keys := make([][]byte, keyCount)
	values := make([][]byte, keyCount)

	for i := 0; i < keyCount; i++ {
		// Create keys that will exercise different trie paths
		key := make([]byte, standardKeyMaxSize)
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
	db := newTestDB(t, smallKeySize, smallKeySize, TrieType8Bit)

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
			t.Errorf(`Expected "%v" for duplicate key %d, got: "%v"`, ErrKeyAlreadyExists, i, err)
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

func Test4BitTrie_EnhancedNibbleCoverage(t *testing.T) {
	// Test 4-bit trie with keys that force nibble extraction and trie expansion
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType4Bit)

	// Use keys designed to trigger all nibble code paths and trie growth
	testKeys := []struct {
		key   []byte
		value string
		desc  string
	}{
		{[]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}, "test1", "low nibbles only"},
		{[]byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC, 0xFE}, "test2", "high nibbles shifted"},
		{[]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}, "test3", "incrementing pairs"},
		{[]byte{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88}, "test4", "decrementing"},
	}

	// First inserts - test basic nibble processing
	for _, tc := range testKeys {
		err := db.Put(tc.key, bytes.NewReader([]byte(tc.value)))
		if err != nil {
			t.Fatalf("Put failed for %s: %v", tc.desc, err)
		}
	}

	// Verify all can be retrieved
	for _, tc := range testKeys {
		reader, err := db.Get(tc.key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", tc.desc, err)
		}
		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Read failed for %s: %v", tc.desc, err)
		}
		if string(result) != tc.value {
			t.Errorf("Wrong value for %s: got %s, expected %s", tc.desc, string(result), tc.value)
		}
	}

	// Add some colliding keys to trigger nibble-based collision resolution
	collidingKeys := []struct {
		key   []byte
		value string
		desc  string
	}{
		{[]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}, "collide1", "shares nibble prefix"},
		{[]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x78}, "collide2", "triggers nibble split"},
	}

	for _, tc := range collidingKeys {
		err := db.Put(tc.key, bytes.NewReader([]byte(tc.value)))
		if err != nil {
			t.Fatalf("Put failed for %s: %v", tc.desc, err)
		}
	}

	// Verify colliding keys are accessible and different
	reader1, err1 := db.Get(collidingKeys[0].key)
	reader2, err2 := db.Get(collidingKeys[1].key)
	if err1 != nil || err2 != nil {
		t.Fatal("Get failed for colliding keys")
	}

	result1, _ := io.ReadAll(reader1)
	result2, _ := io.ReadAll(reader2)

	if string(result1) == string(result2) {
		t.Error("Colliding keys returned same value")
	}
	if string(result1) != collidingKeys[0].value {
		t.Errorf("Wrong value for collide1: got %s, expected %s", string(result1), collidingKeys[0].value)
	}
	if string(result2) != collidingKeys[1].value {
		t.Errorf("Wrong value for collide2: got %s, expected %s", string(result2), collidingKeys[1].value)
	}
}

func TestDatabase_NodeExpansion(t *testing.T) {
	// Test that forces trie node expansion (appendIndex calls)
	db := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)

	// Create many unique keys that will definitely require new trie nodes
	numKeys := 300
	baseKey := make([]byte, standardKeyMaxSize)

	for i := 0; i < numKeys; i++ {
		// Create deterministic unique keys to avoid collisions
		baseKey[0] = byte(i % 256)           // First byte varies systematically
		baseKey[1] = byte((i / 256) % 256)   // Second byte varies for large i
		baseKey[2] = byte((i / 65536) % 256) // Third byte for very large i
		for j := 3; j < len(baseKey); j++ {
			baseKey[j] = byte((i + 997) % 256) // Fill rest with unique pattern
		}

		value := fmt.Sprintf("expansion-test-%d", i)
		err := db.Put(baseKey[:], bytes.NewReader([]byte(value)))
		if err != nil {
			t.Fatalf("Put failed for key %d: %v", i, err)
		}
	}

	// Verify a sample of keys can be retrieved
	for i := 0; i < numKeys; i += 10 { // Test every 10th key
		// Use same deterministic algorithm as insertion
		baseKey[0] = byte(i % 256)
		baseKey[1] = byte((i / 256) % 256)
		baseKey[2] = byte((i / 65536) % 256)
		for j := 3; j < len(baseKey); j++ {
			baseKey[j] = byte((i + 997) % 256)
		}

		expectedValue := fmt.Sprintf("expansion-test-%d", i)
		reader, err := db.Get(baseKey[:])
		if err != nil {
			t.Fatalf("Get failed for expanded key %d: %v", i, err)
		}

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Read failed for expanded key %d: %v", i, err)
		}

		if string(result) != expectedValue {
			t.Errorf("Wrong value for expanded key %d: got %s, expected %s", i, string(result), expectedValue)
		}
	}
}

func TestNewDatabase_InvalidBounds(t *testing.T) {
	// Test constructor parameter validation for edge cases
	dir := t.TempDir()

	invalidBounds := []struct {
		minSize, maxSize int
		desc             string
	}{
		{0, 32, "minimum size zero"},
		{-1, 32, "minimum size negative"},
		{8, 0, "maximum size zero"},
		{8, -1, "maximum size negative"},
		{32, 16, "minimum > maximum"},
	}

	for _, tc := range invalidBounds {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := NewDatabase("test", dir, tc.minSize, tc.maxSize, TrieType8Bit)
			if err == nil {
				t.Errorf("Expected error for %s, but got nil", tc.desc)
			}
			// Check that error contains expected validation message
			expectedErr := "wrong key limits"
			if err.Error() != expectedErr {
				t.Errorf("Expected error message '%s', got '%s'", expectedErr, err.Error())
			}
		})
	}

	// Test valid case works
	t.Run("valid bounds", func(t *testing.T) {
		db, err := NewDatabase("test", dir, 8, 32, TrieType8Bit)
		if err != nil {
			t.Errorf("Valid bounds should not fail, got: %v", err)
		}
		if db == nil {
			t.Error("Valid bounds should return non-nil database")
		}
	})
}

func TestDatabase_KeySizeBoundaries(t *testing.T) {
	// Test exact boundary conditions for key sizes

	// Test exact minimum key size (should succeed)
	minKey := make([]byte, standardKeyMinSize) // 8 bytes
	minValue := "minimum-size"
	dbMin := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
	err := dbMin.Put(minKey, bytes.NewReader([]byte(minValue)))
	if err != nil {
		t.Errorf("Put failed for minimum key size: %v", err)
	}

	reader, err := dbMin.Get(minKey)
	if err != nil {
		t.Errorf("Get failed for minimum key size: %v", err)
	}
	result, _ := io.ReadAll(reader)
	if string(result) != minValue {
		t.Error("Wrong value for minimum key size")
	}

	// Test exact maximum key size (should succeed)
	maxKey := make([]byte, standardKeyMaxSize) // 32 bytes
	maxValue := "maximum-size"
	dbMax := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
	err = dbMax.Put(maxKey, bytes.NewReader([]byte(maxValue)))
	if err != nil {
		t.Errorf("Put failed for maximum key size: %v", err)
	}

	reader, err = dbMax.Get(maxKey)
	if err != nil {
		t.Errorf("Get failed for maximum key size: %v", err)
	}
	result, _ = io.ReadAll(reader)
	if string(result) != maxValue {
		t.Error("Wrong value for maximum key size")
	}

	// Test oversized key (should fail)
	tooBigKey := make([]byte, standardKeyMaxSize+1) // 33 bytes
	dbFail := newTestDB(t, standardKeyMinSize, standardKeyMaxSize, TrieType8Bit)
	err = dbFail.Put(tooBigKey, bytes.NewReader([]byte("fail")))
	if err != ErrInvalidKeySize {
		t.Errorf(`Expected "%v" for oversized key, got: "%v"`, ErrInvalidKeySize, err)
	}
}

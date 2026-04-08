package db

import "testing"

func collectKeyIter(seq func(func(int, byte) bool)) ([]int, []byte) {
	var indexes []int
	var values []byte
	seq(func(i int, b byte) bool {
		indexes = append(indexes, i)
		values = append(values, b)
		return true
	})
	return indexes, values
}

func TestTrieNode4GetVariants(t *testing.T) {
	node := &trieNode4{}
	variants := node.getVariants()

	if len(variants) != 16 {
		t.Fatalf("expected 16 variants, got %d", len(variants))
	}

	variants[3] = 42
	if node.Variants[3] != 42 {
		t.Fatalf("expected mutation through slice to affect node, got %d", node.Variants[3])
	}
}

func TestTrieNode4KeyIter(t *testing.T) {
	node := trieNode4{}
	idx, vals := collectKeyIter(node.keyIter([]byte{0xAB, 0xCD}))

	expectedIdx := []int{0, 1, 2, 3}
	expectedVals := []byte{0x0A, 0x0B, 0x0C, 0x0D}

	for i := range expectedIdx {
		if idx[i] != expectedIdx[i] {
			t.Fatalf("index %d: expected %d, got %d", i, expectedIdx[i], idx[i])
		}
		if vals[i] != expectedVals[i] {
			t.Fatalf("value %d: expected %x, got %x", i, expectedVals[i], vals[i])
		}
	}
}

func TestTrieNode8GetVariants(t *testing.T) {
	node := &trieNode8{}
	variants := node.getVariants()

	if len(variants) != 256 {
		t.Fatalf("expected 256 variants, got %d", len(variants))
	}

	variants[200] = 99
	if node.Variants[200] != 99 {
		t.Fatalf("expected mutation through slice to affect node, got %d", node.Variants[200])
	}
}

func TestTrieNode8KeyIter(t *testing.T) {
	node := trieNode8{}
	idx, vals := collectKeyIter(node.keyIter([]byte{0x10, 0x20, 0x30}))

	expectedIdx := []int{0, 1, 2}
	expectedVals := []byte{0x10, 0x20, 0x30}

	for i := range expectedIdx {
		if idx[i] != expectedIdx[i] {
			t.Fatalf("index %d: expected %d, got %d", i, expectedIdx[i], idx[i])
		}
		if vals[i] != expectedVals[i] {
			t.Fatalf("value %d: expected %x, got %x", i, expectedVals[i], vals[i])
		}
	}
}

func TestTrieNodeIterStopsWhenYieldReturnsFalse(t *testing.T) {
	node4 := trieNode4{}
	count4 := 0
	node4.keyIter([]byte{0xAB})(func(i int, b byte) bool {
		count4++
		return false
	})
	if count4 != 1 {
		t.Fatalf("expected 4-bit iterator to stop after first yield, got %d", count4)
	}

	node8 := trieNode8{}
	count8 := 0
	node8.keyIter([]byte{0x10, 0x20})(func(i int, b byte) bool {
		count8++
		return false
	})
	if count8 != 1 {
		t.Fatalf("expected 8-bit iterator to stop after first yield, got %d", count8)
	}
}

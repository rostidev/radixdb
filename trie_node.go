package radixdb

import "iter"

type trieNode interface {
	getVariants() []int64
	keyIter(key []byte) iter.Seq2[int, byte]
}

// 4-bit prefix tree node
type trieNode4 struct {
	Variants [16]int64
}

func (t *trieNode4) getVariants() []int64 {
	return t.Variants[:]
}

func (t trieNode4) keyIter(key []byte) iter.Seq2[int, byte] {
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

// 8-bit prefix tree node
type trieNode8 struct {
	Variants [256]int64
}

func (t *trieNode8) getVariants() []int64 {
	return t.Variants[:]
}

func (t trieNode8) keyIter(key []byte) iter.Seq2[int, byte] {
	return func(yield func(int, byte) bool) {
		for i, b := range key {
			if !yield(i, b) {
				break
			}
		}
	}
}

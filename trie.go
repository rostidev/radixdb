package db

import (
	"encoding"
	"fmt"
)

type TrieType int

const (
	TrieType4Bit TrieType = iota
	TrieType8Bit
)

const (
	trie4Bit = "4bit"
	trie8Bit = "8bit"
)

// Compile time assertion that TrieType implements all those interfaces.
var _ interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
} = (*TrieType)(nil)

// MarshalText implements encoding.TextMarshaler with value receiver.
func (t TrieType) MarshalText() ([]byte, error) {
	switch t {
	case TrieType4Bit:
		return []byte(trie4Bit), nil
	case TrieType8Bit:
		return []byte(trie8Bit), nil
	default:
		return nil, fmt.Errorf("unknown trie type: %d", int(t))
	}
}

// UnmarshalText implements encoding.TextUnmarshaler with pointer receiver.
// Changes its value or returns an error.
func (t *TrieType) UnmarshalText(text []byte) error {
	switch string(text) {
	case trie4Bit:
		*t = TrieType4Bit
	case trie8Bit:
		*t = TrieType8Bit
	default:
		return fmt.Errorf("unknown trie type: %s", string(text))
	}
	return nil
}

package db

import (
	"fmt"
)

type TrieType string

const (
	TrieType4Bit TrieType = "4bit"
	TrieType8Bit TrieType = "8bit"
)

var trieFactories = map[TrieType]func() trieNode{
	TrieType4Bit: func() trieNode { return &trieNode4{} },
	TrieType8Bit: func() trieNode { return &trieNode8{} },
}

// Factory method
func (t TrieType) NewTrieNode() trieNode {
	if factory, ok := trieFactories[t]; ok {
		return factory()
	}
	panic("unknown trie type: " + string(t))
}

func (t TrieType) MarshalText() ([]byte, error) {
	if _, ok := trieFactories[t]; !ok {
		return nil, fmt.Errorf("unknown trie type: %s", t)
	}
	return []byte(t), nil
}

func (t *TrieType) UnmarshalText(data []byte) error {
	switch s := string(data); s {
	case "4bit", "8bit":
		*t = TrieType(s)
		return nil
	default:
		return fmt.Errorf("unknown trie type: %s", s)
	}
}

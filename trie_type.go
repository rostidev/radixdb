package db

type TrieType interface {
	NewTrieNode() trieNode
	NodeSize() int64
}

var TrieType4Bit TrieType = trieType4Bit{}
var TrieType8Bit TrieType = trieType8Bit{}

type trieType4Bit struct{}

func (t trieType4Bit) NewTrieNode() trieNode {
	return &trieNode4{}
}

func (t trieType4Bit) NodeSize() int64 {
	return int64(16 * 8)
}

type trieType8Bit struct{}

func (t trieType8Bit) NewTrieNode() trieNode {
	return &trieNode8{}
}

func (t trieType8Bit) NodeSize() int64 {
	return int64(256 * 8)
}

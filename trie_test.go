package db

import "testing"

func TestTrieTypeMarshalText(t *testing.T) {
	tests := []struct {
		name     string
		trieType TrieType
		expected string
		hasError bool
	}{
		{"4bit", TrieType4Bit, "4bit", false},
		{"8bit", TrieType8Bit, "8bit", false},
		{"invalid", TrieType("999bit"), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.trieType.MarshalText()

			if tt.hasError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if string(data) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(data))
			}
		})
	}
}

func TestTrieTypeUnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected TrieType
		hasError bool
	}{
		{"4bit", "4bit", TrieType4Bit, false},
		{"8bit", "8bit", TrieType8Bit, false},
		{"invalid", "invalid", TrieType4Bit, true},
		{"empty", "", TrieType4Bit, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var trieType TrieType
			err := trieType.UnmarshalText([]byte(tt.input))

			if tt.hasError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if trieType != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, trieType)
			}
		})
	}
}

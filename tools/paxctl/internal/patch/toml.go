package patch

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// ReadTOML reads and parses a TOML file into a map.
// Returns an empty map if the file does not exist.
func ReadTOML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, err
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// WriteTOML atomically encodes doc as TOML and writes it to path via
// temp-file + rename.
func WriteTOML(path string, doc map[string]any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return fmt.Errorf("encoding TOML: %w", err)
	}
	return WriteFileAtomic(path, buf.Bytes(), 0o644)
}

// UnmarshalTOML parses raw TOML bytes into a map.
func UnmarshalTOML(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// MarshalTOML encodes a map as TOML bytes.
func MarshalTOML(doc any) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

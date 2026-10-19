package patch

import (
	"bytes"
	"encoding/json"
	"os"
)

// ReadJSON reads and parses a JSON file into a map.
// Returns an empty map if the file does not exist.
func ReadJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// WriteJSON atomically encodes doc as indented JSON and writes it to path.
func WriteJSON(path string, doc any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes(), 0o644)
}

// UnmarshalJSON parses raw JSON bytes into a map.
func UnmarshalJSON(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// MarshalJSON encodes a map as indented JSON bytes.
func MarshalJSON(doc any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

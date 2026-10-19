package patch

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes content to path atomically by writing to a temp file
// first, then renaming.
func WriteFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(content); err != nil {
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	tmpFile = nil

	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// SetNestedValue sets doc[section][key] = value, creating the section map
// if needed.
func SetNestedValue(doc map[string]any, section, key string, value any) {
	sec, ok := doc[section].(map[string]any)
	if !ok {
		sec = make(map[string]any)
		doc[section] = sec
	}
	sec[key] = value
}

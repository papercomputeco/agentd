// Package secrets reads secret files from a tmpfs directory written by
// stereosd. Each file in the directory represents a secret where the
// filename is the environment variable name and the file content is the value.
package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reader reads secrets from a directory on disk.
type Reader struct {
	dir string
}

// NewReader creates a new secret reader for the given directory.
func NewReader(dir string) *Reader {
	return &Reader{dir: dir}
}

// Dir returns the secret directory path.
func (r *Reader) Dir() string {
	return r.dir
}

// ReadAll reads all secrets from the directory and returns them as a
// map of name to value. Hidden files (starting with ".") are skipped.
// Only regular files are read.
func (r *Reader) ReadAll() (map[string]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("reading secret directory %s: %w", r.dir, err)
	}

	secrets := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		value, err := r.Read(name)
		if err != nil {
			return nil, err
		}
		secrets[name] = value
	}

	return secrets, nil
}

// Read reads a single secret by name from the directory.
func (r *Reader) Read(name string) (string, error) {
	path := filepath.Join(r.dir, name)

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("reading secret %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("secret %q is not a regular file", name)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret %q: %w", name, err)
	}

	return strings.TrimRight(string(data), "\n"), nil
}

// List returns the names of all secrets in the directory.
func (r *Reader) List() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing secrets in %s: %w", r.dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}

	return names, nil
}

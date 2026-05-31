package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Root string
}

func New(root string) Store {
	if root == "" {
		root = "data"
	}
	return Store{Root: root}
}

func (s Store) path(key string) string {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	return filepath.Join(s.Root, clean)
}

func (s Store) Get(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(s.path(key))
}

func (s Store) Put(_ context.Context, key string, data []byte, _ string) error {
	path := s.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s Store) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(s.path(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

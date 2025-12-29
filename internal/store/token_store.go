package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

type TokenStore struct {
	path string
}

func NewTokenStore() (*TokenStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".gmail-tui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &TokenStore{path: filepath.Join(dir, "token.json")}, nil
}

func (s *TokenStore) Load() (*oauth2.Token, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *TokenStore) Save(t *oauth2.Token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0600)
}

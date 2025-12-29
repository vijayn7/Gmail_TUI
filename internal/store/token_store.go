package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

// TokenStore manages persistent storage of OAuth2 tokens on disk.
// Tokens are saved in the user's home directory at ~/.gmail-tui/token.json
type TokenStore struct {
	path string
}

// NewTokenStore creates a new TokenStore instance and ensures the storage
// directory exists. The directory is created with 0700 permissions (user-only access)
// for security. Returns an error if the home directory cannot be determined or
// the .gmail-tui directory cannot be created.
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

// Load reads and deserializes an OAuth2 token from disk.
// Returns an error if the file doesn't exist or cannot be parsed.
// A missing file indicates the user hasn't logged in yet.
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

// Save serializes and writes an OAuth2 token to disk with 0600 permissions
// (user read/write only) for security. This allows the token to persist across
// application restarts so the user doesn't need to re-authenticate each time.
func (s *TokenStore) Save(t *oauth2.Token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0600)
}

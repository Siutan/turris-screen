package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// secretStore keeps tokens and headers out of project JSON.
type secretStore struct {
	mu   sync.Mutex
	path string
}

func openSecrets() *secretStore {
	root, err := os.UserConfigDir()
	if err != nil {
		return &secretStore{}
	}
	dir := filepath.Join(root, "turris-screen")
	_ = os.MkdirAll(dir, 0o755)
	return &secretStore{path: filepath.Join(dir, "secrets.json")}
}

func (s *secretStore) get(id string) SourceSecret {
	if s == nil || s.path == "" || id == "" {
		return SourceSecret{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.read()
	item, ok := all[id]
	if !ok {
		return SourceSecret{}
	}
	return item
}

func (s *secretStore) set(id string, secret SourceSecret) error {
	if s == nil || s.path == "" {
		return os.ErrInvalid
	}
	if id == "" {
		return os.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.read()
	if secret.Token == "" && secret.User == "" && secret.Pass == "" && len(secret.Headers) == 0 {
		delete(all, id)
	} else {
		all[id] = secret
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *secretStore) read() map[string]SourceSecret {
	out := map[string]SourceSecret{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	if out == nil {
		out = map[string]SourceSecret{}
	}
	return out
}

func (a *App) secretFor(id string) SourceSecret {
	if a == nil || a.secrets == nil {
		return SourceSecret{}
	}
	return a.secrets.get(id)
}

// SetSourceSecret stores a token or headers for one source. The project file stays free of them.
func (a *App) SetSourceSecret(id string, secret SourceSecret) error {
	if a.secrets == nil {
		a.secrets = openSecrets()
	}
	return a.secrets.set(id, secret)
}

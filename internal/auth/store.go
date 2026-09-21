package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type session struct {
	ID             string `json:"id"`
	BrowserID      string `json:"browser_id"`
	Binding        string `json:"binding"`
	PasswordDigest string `json:"password_digest"`
	CreatedAt      int64  `json:"created_at"`
	ExpiresAt      int64  `json:"expires_at"`
}

type sessionFile struct {
	Sessions map[string]session `json:"sessions"`
}

type store struct {
	mu       sync.Mutex
	path     string
	sessions map[string]session
}

func loadStore(path string) (*store, error) {
	s := &store{path: path, sessions: map[string]session{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var state sessionFile
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Sessions != nil {
		s.sessions = state.Sessions
	}
	return s, nil
}

func (s *store) add(value session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[value.ID] = value
	if err := s.save(); err != nil {
		delete(s.sessions, value.ID)
		return err
	}
	return nil
}

func (s *store) valid(token, binding, passwordDigest string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[digest(token)]
	return ok && value.Binding == binding && value.PasswordDigest == passwordDigest &&
		(value.ExpiresAt == 0 || now.Unix() < value.ExpiresAt)
}

func (s *store) forBrowser(browserToken string, now time.Time) []session {
	s.mu.Lock()
	defer s.mu.Unlock()
	var values []session
	for _, value := range s.sessions {
		if value.BrowserID == digest(browserToken) && (value.ExpiresAt == 0 || now.Unix() < value.ExpiresAt) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt > values[j].CreatedAt })
	return values
}

func (s *store) revoke(browserToken, binding string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := map[string]session{}
	for id, value := range s.sessions {
		if value.BrowserID == digest(browserToken) && (binding == "" || value.Binding == binding) {
			removed[id] = value
			delete(s.sessions, id)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := s.save(); err != nil {
		for id, value := range removed {
			s.sessions[id] = value
		}
		return 0, err
	}
	return len(removed), nil
}

func (s *store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(sessionFile{Sessions: s.sessions})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".auth-sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}

func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

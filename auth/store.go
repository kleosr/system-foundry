package auth

import (
	"sync"
	"time"
)

type userRow struct {
	ID       string
	Email    string
	Password string
	TenantID string
	Role     string
}

type sessionRow struct {
	ID      string
	UserID  string
	Expires time.Time
}

type clientRow struct {
	ID        string
	Audience  string
	Redirects map[string]struct{}
}

type codeRow struct {
	Code      string
	UserID    string
	ClientID  string
	Redirect  string
	Challenge string
	Nonce     string
	Expires   time.Time
	Used      bool
}

type refreshRow struct {
	Hash     string
	Family   string
	UserID   string
	ClientID string
	Expires  time.Time
	Used     bool
}

type MemoryStore struct {
	mu       sync.Mutex
	users    map[string]userRow
	byEmail  map[string]string
	sessions map[string]sessionRow
	clients  map[string]clientRow
	codes    map[string]codeRow
	refresh  map[string]refreshRow
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:    map[string]userRow{},
		byEmail:  map[string]string{},
		sessions: map[string]sessionRow{},
		clients:  map[string]clientRow{},
		codes:    map[string]codeRow{},
		refresh:  map[string]refreshRow{},
	}
}

func (s *MemoryStore) putUser(row userRow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byEmail[row.Email]; ok {
		return false
	}
	s.users[row.ID] = row
	s.byEmail[row.Email] = row.ID
	return true
}

func (s *MemoryStore) userByEmail(email string) (userRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byEmail[email]
	if !ok {
		return userRow{}, false
	}
	row, ok := s.users[id]
	return row, ok
}

func (s *MemoryStore) userByID(id string) (userRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.users[id]
	return row, ok
}

func (s *MemoryStore) putSession(row sessionRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[row.ID] = row
	return nil
}

func (s *MemoryStore) session(id string) (sessionRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.sessions[id]
	return row, ok
}

func (s *MemoryStore) deleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStore) putClient(row clientRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[row.ID] = row
	return nil
}

func (s *MemoryStore) client(id string) (clientRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.clients[id]
	return row, ok
}

func (s *MemoryStore) putCode(row codeRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[row.Code] = row
	return nil
}

func (s *MemoryStore) takeCode(code string, now time.Time) (codeRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.codes[code]
	if !ok || row.Used || !row.Expires.After(now) {
		return codeRow{}, false
	}
	row.Used = true
	s.codes[code] = row
	return row, true
}

func (s *MemoryStore) putRefresh(row refreshRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh[row.Hash] = row
	return nil
}

func (s *MemoryStore) refreshByHash(hash string) (refreshRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.refresh[hash]
	return row, ok
}

func (s *MemoryStore) markRefreshUsed(hash string) (refreshRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.refresh[hash]
	if !ok || row.Used {
		return row, false
	}
	row.Used = true
	s.refresh[hash] = row
	return row, true
}

func (s *MemoryStore) revokeFamily(family string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, row := range s.refresh {
		if row.Family == family {
			delete(s.refresh, hash)
		}
	}
	return nil
}

func (s *MemoryStore) revokeUserRefresh(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, row := range s.refresh {
		if row.UserID == userID {
			delete(s.refresh, hash)
		}
	}
	return nil
}

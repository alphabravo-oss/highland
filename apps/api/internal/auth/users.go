package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// LocalUser is a local account (password never returned in public APIs).
type LocalUser struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Role     Role   `json:"role"`
}

// PublicUser is safe for list APIs.
type PublicUser struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

// UserStore holds local users for login + admin CRUD.
type UserStore struct {
	mu       sync.RWMutex
	users    map[string]LocalUser
	filePath string // optional persistence (PVC/file)
}

// NewUserStoreFromEnv loads users from HIGHLAND_USERS JSON, optional HIGHLAND_USERS_FILE, or bootstrap admin.
func NewUserStoreFromEnv(bootstrapUser, bootstrapPass string) *UserStore {
	s := &UserStore{
		users:    map[string]LocalUser{},
		filePath: os.Getenv("HIGHLAND_USERS_FILE"),
	}
	if s.filePath != "" {
		if raw, err := os.ReadFile(s.filePath); err == nil && len(raw) > 0 {
			var list []LocalUser
			if json.Unmarshal(raw, &list) == nil {
				for _, u := range list {
					if u.Username == "" {
						continue
					}
					if u.Role == "" {
						u.Role = RoleViewer
					}
					u.Role = ParseRole(string(u.Role))
					s.users[u.Username] = u
				}
			}
		}
	}
	if raw := os.Getenv("HIGHLAND_USERS"); raw != "" {
		var list []LocalUser
		if err := json.Unmarshal([]byte(raw), &list); err == nil {
			for _, u := range list {
				if u.Username == "" || u.Password == "" {
					continue
				}
				if u.Role == "" {
					u.Role = RoleAdmin
				}
				u.Role = ParseRole(string(u.Role))
				s.users[u.Username] = u
			}
		}
	}
	if len(s.users) == 0 && bootstrapUser != "" {
		s.users[bootstrapUser] = LocalUser{
			Username: bootstrapUser,
			Password: bootstrapPass,
			Role:     RoleAdmin,
		}
		if os.Getenv("HIGHLAND_DEV_ROLES") == "1" || os.Getenv("HIGHLAND_DEV_ROLES") == "true" {
			s.users["operator"] = LocalUser{Username: "operator", Password: "operator", Role: RoleOperator}
			s.users["viewer"] = LocalUser{Username: "viewer", Password: "viewer", Role: RoleViewer}
		}
		_ = s.persist()
	}
	return s
}

func (s *UserStore) persist() error {
	if s.filePath == "" {
		return nil
	}
	s.mu.RLock()
	list := make([]LocalUser, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	s.mu.RUnlock()
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, b, 0o600)
}

// Authenticate validates credentials.
func (s *UserStore) Authenticate(username, password string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok || u.Password != password {
		return nil, false
	}
	return &User{Username: u.Username, Role: u.Role}, true
}

// ListPublic returns usernames and roles (no passwords).
func (s *UserStore) ListPublic() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicUser, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, PublicUser{Username: u.Username, Role: u.Role})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if strings.Compare(out[i].Username, out[j].Username) > 0 {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Create adds a local user.
func (s *UserStore) Create(username, password string, role Role) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("username and password required")
	}
	role = ParseRole(string(role))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[username]; exists {
		return fmt.Errorf("user already exists")
	}
	s.users[username] = LocalUser{Username: username, Password: password, Role: role}
	return s.persistLocked()
}

// UpdateRole changes role; optional password update if non-empty.
func (s *UserStore) Update(username, password string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if role != "" {
		u.Role = ParseRole(string(role))
	}
	if password != "" {
		u.Password = password
	}
	s.users[username] = u
	return s.persistLocked()
}

// Delete removes a user (cannot delete last admin).
func (s *UserStore) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if u.Role == RoleAdmin {
		admins := 0
		for _, x := range s.users {
			if x.Role == RoleAdmin {
				admins++
			}
		}
		if admins <= 1 {
			return fmt.Errorf("cannot delete the last admin")
		}
	}
	delete(s.users, username)
	return s.persistLocked()
}

func (s *UserStore) persistLocked() error {
	if s.filePath == "" {
		return nil
	}
	list := make([]LocalUser, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, b, 0o600)
}

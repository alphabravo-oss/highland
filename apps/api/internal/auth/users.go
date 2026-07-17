package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type MFAMode string

const (
	MFADisabled       MFAMode = "disabled"
	MFAOptional       MFAMode = "optional"
	MFARequiredAdmins MFAMode = "required-admins"
	MFARequiredAll    MFAMode = "required-all"
)

type SecurityPolicy struct {
	MinimumPasswordLength int     `json:"minimumPasswordLength"`
	MaximumPasswordLength int     `json:"maximumPasswordLength"`
	PasswordHistory       int     `json:"passwordHistory"`
	BlockCommonPasswords  bool    `json:"blockCommonPasswords"`
	MFAMode               MFAMode `json:"mfaMode"`
}

func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		MinimumPasswordLength: 15,
		MaximumPasswordLength: 128,
		PasswordHistory:       5,
		BlockCommonPasswords:  true,
		MFAMode:               MFAOptional,
	}
}

func (p SecurityPolicy) Normalized() SecurityPolicy {
	switch p.MFAMode {
	case MFADisabled, MFAOptional, MFARequiredAdmins, MFARequiredAll:
	default:
		p.MFAMode = MFAOptional
	}
	minimumFloor := 15
	if p.MFAMode == MFARequiredAll {
		minimumFloor = 8
	}
	if p.MinimumPasswordLength < minimumFloor {
		p.MinimumPasswordLength = minimumFloor
	}
	if p.MaximumPasswordLength < 64 {
		p.MaximumPasswordLength = 64
	}
	if p.MaximumPasswordLength > 256 {
		p.MaximumPasswordLength = 256
	}
	if p.MinimumPasswordLength > p.MaximumPasswordLength {
		p.MinimumPasswordLength = p.MaximumPasswordLength
	}
	if p.PasswordHistory < 0 {
		p.PasswordHistory = 0
	}
	if p.PasswordHistory > 24 {
		p.PasswordHistory = 24
	}
	return p
}

func (p SecurityPolicy) RequiresMFA(role Role) bool {
	switch p.Normalized().MFAMode {
	case MFARequiredAll:
		return true
	case MFARequiredAdmins:
		return role == RoleAdmin
	default:
		return false
	}
}

func (p SecurityPolicy) MFAAvailable() bool { return p.Normalized().MFAMode != MFADisabled }

type LocalUser struct {
	Username            string    `json:"username"`
	Password            string    `json:"password,omitempty"`
	PasswordHistory     []string  `json:"passwordHistory,omitempty"`
	Email               string    `json:"email,omitempty"`
	Role                Role      `json:"role"`
	Disabled            bool      `json:"disabled,omitempty"`
	MFAEnabled          bool      `json:"mfaEnabled,omitempty"`
	TOTPSecret          string    `json:"totpSecret,omitempty"`
	PendingTOTPSecret   string    `json:"pendingTotpSecret,omitempty"`
	RecoveryCodeHashes  []string  `json:"recoveryCodeHashes,omitempty"`
	SessionVersion      int       `json:"sessionVersion"`
	CreatedAt           time.Time `json:"createdAt,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt,omitempty"`
	PasswordChangedAt   time.Time `json:"passwordChangedAt,omitempty"`
	LastAuthenticatedAt time.Time `json:"lastAuthenticatedAt,omitempty"`
}

type PublicUser struct {
	Username            string    `json:"username"`
	Email               string    `json:"email,omitempty"`
	Role                Role      `json:"role"`
	Disabled            bool      `json:"disabled"`
	MFAEnabled          bool      `json:"mfaEnabled"`
	MFARequired         bool      `json:"mfaRequired"`
	CreatedAt           time.Time `json:"createdAt,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt,omitempty"`
	PasswordChangedAt   time.Time `json:"passwordChangedAt,omitempty"`
	LastAuthenticatedAt time.Time `json:"lastAuthenticatedAt,omitempty"`
}

type IdentityDocument struct {
	Version int            `json:"version"`
	Policy  SecurityPolicy `json:"policy"`
	Users   []LocalUser    `json:"users"`
}

type IdentityPersistence interface {
	Load(context.Context) (IdentityDocument, error)
	Update(context.Context, func(*IdentityDocument) error) (IdentityDocument, error)
}

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrAccountDisabled     = errors.New("account disabled")
	ErrIdentityUnavailable = errors.New("identity store unavailable")
)

type UserStore struct {
	mu          sync.RWMutex
	users       map[string]LocalUser
	policy      SecurityPolicy
	filePath    string
	persistence IdentityPersistence
}

func NewUserStoreFromEnv(bootstrapUser, bootstrapPass string) *UserStore {
	s := &UserStore{users: map[string]LocalUser{}, policy: DefaultSecurityPolicy(), filePath: os.Getenv("HIGHLAND_USERS_FILE")}
	if s.filePath != "" {
		if raw, err := os.ReadFile(s.filePath); err == nil && len(raw) > 0 {
			var document IdentityDocument
			if json.Unmarshal(raw, &document) == nil && len(document.Users) > 0 {
				s.applyDocument(document)
			} else {
				var legacy []LocalUser
				if json.Unmarshal(raw, &legacy) == nil {
					s.applyDocument(IdentityDocument{Version: 1, Policy: s.policy, Users: legacy})
				}
			}
		}
	}
	if raw := os.Getenv("HIGHLAND_USERS"); raw != "" {
		var list []LocalUser
		if json.Unmarshal([]byte(raw), &list) == nil {
			for _, user := range list {
				if user.Username == "" || user.Password == "" {
					continue
				}
				s.seedUser(user)
			}
		}
	}
	if len(s.users) == 0 && bootstrapUser != "" {
		s.seedUser(LocalUser{Username: bootstrapUser, Password: bootstrapPass, Role: RoleAdmin})
		if os.Getenv("HIGHLAND_DEV_ROLES") == "1" || os.Getenv("HIGHLAND_DEV_ROLES") == "true" {
			s.seedUser(LocalUser{Username: "operator", Password: "operator", Role: RoleOperator})
			s.seedUser(LocalUser{Username: "viewer", Password: "viewer", Role: RoleViewer})
		}
		_ = s.persistFile()
	}
	return s
}

func (s *UserStore) seedUser(user LocalUser) {
	now := time.Now().UTC()
	user.Username = strings.TrimSpace(user.Username)
	if user.Role == "" {
		user.Role = RoleViewer
	}
	user.Role = ParseRole(string(user.Role))
	hash, err := ensureHash(user.Password)
	if err != nil {
		slog.Warn("skipping user with invalid password hash", "user", user.Username, "err", err)
		return
	}
	user.Password = hash
	if user.SessionVersion < 1 {
		user.SessionVersion = 1
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
	if user.PasswordChangedAt.IsZero() {
		user.PasswordChangedAt = now
	}
	s.users[user.Username] = user
}

func (s *UserStore) ConfigurePersistence(ctx context.Context, persistence IdentityPersistence) error {
	if persistence == nil {
		return errors.New("identity persistence is required")
	}
	s.mu.Lock()
	s.persistence = persistence
	seed := s.documentLocked()
	s.mu.Unlock()
	document, err := persistence.Update(ctx, func(current *IdentityDocument) error {
		if len(current.Users) == 0 {
			*current = seed
			current.Version = 1
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("initialize identity persistence: %w", err)
	}
	s.applyDocument(document)
	return nil
}

func (s *UserStore) StartSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Refresh(ctx); err != nil {
					slog.Warn("identity synchronization failed", "err", err)
				}
			}
		}
	}()
}

func (s *UserStore) Refresh(ctx context.Context) error {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence == nil {
		return nil
	}
	document, err := persistence.Load(ctx)
	if err != nil {
		return fmt.Errorf("load identity document: %w", err)
	}
	s.applyDocument(document)
	return nil
}

func (s *UserStore) Authenticate(ctx context.Context, username, password string) (*User, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, ErrIdentityUnavailable
	}
	s.mu.RLock()
	local, ok := s.users[username]
	policy := s.policy
	s.mu.RUnlock()
	if !ok {
		_, _ = verifyPassword("$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQxMjM0NTY$DSjL0Q+MO/Y3J7RqLejTJvJ3AX/UXQYy6k2A2bVwOaY", password)
		return nil, ErrInvalidCredentials
	}
	valid, legacy := verifyPassword(local.Password, password)
	if !valid {
		return nil, ErrInvalidCredentials
	}
	if local.Disabled {
		return nil, ErrAccountDisabled
	}
	if legacy {
		if hash, err := hashPassword(password); err == nil {
			_ = s.mutate(ctx, func(document *IdentityDocument) error {
				if user := findUser(document, username); user != nil && user.Password == local.Password {
					user.Password = hash
					user.UpdatedAt = time.Now().UTC()
				}
				return nil
			})
		}
	}
	return local.sessionUser(policy), nil
}

func (s *UserStore) VerifySecondFactor(ctx context.Context, username, code string) (*User, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, ErrIdentityUnavailable
	}
	s.mu.RLock()
	local, ok := s.users[username]
	policy := s.policy
	s.mu.RUnlock()
	if !ok || local.Disabled || !local.MFAEnabled {
		return nil, ErrInvalidCredentials
	}
	if validTOTP(local.TOTPSecret, code, time.Now()) {
		return local.sessionUser(policy), nil
	}
	hash := recoveryCodeHash(code)
	used := false
	err := s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil || user.Disabled || !user.MFAEnabled {
			return ErrInvalidCredentials
		}
		for index, candidate := range user.RecoveryCodeHashes {
			if hmacEqualString(candidate, hash) {
				user.RecoveryCodeHashes = append(user.RecoveryCodeHashes[:index], user.RecoveryCodeHashes[index+1:]...)
				user.UpdatedAt = time.Now().UTC()
				used = true
				return nil
			}
		}
		return ErrInvalidCredentials
	})
	if err != nil || !used {
		return nil, ErrInvalidCredentials
	}
	return local.sessionUser(policy), nil
}

func hmacEqualString(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range len(left) {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func (u LocalUser) sessionUser(policy SecurityPolicy) *User {
	return &User{
		Username: u.Username, Email: u.Email, Role: u.Role, AuthSource: "local",
		MFAEnabled: u.MFAEnabled && policy.MFAAvailable(), MFASetupRequired: policy.RequiresMFA(u.Role) && !u.MFAEnabled,
		SessionVersion: u.SessionVersion,
	}
}

func (s *UserStore) ValidateSession(user User) bool {
	if user.AuthSource == "oidc" {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, ok := s.users[user.Username]
	return ok && !current.Disabled && current.SessionVersion == user.SessionVersion && current.Role == user.Role
}

func (s *UserStore) ListPublic(ctx context.Context) ([]PublicUser, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicUser, 0, len(s.users))
	for _, user := range s.users {
		out = append(out, user.public(s.policy))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (u LocalUser) public(policy SecurityPolicy) PublicUser {
	return PublicUser{
		Username: u.Username, Email: u.Email, Role: u.Role, Disabled: u.Disabled,
		MFAEnabled: u.MFAEnabled, MFARequired: policy.RequiresMFA(u.Role),
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, PasswordChangedAt: u.PasswordChangedAt,
		LastAuthenticatedAt: u.LastAuthenticatedAt,
	}
}

func (s *UserStore) Profile(ctx context.Context, username string) (PublicUser, SecurityPolicy, error) {
	if err := s.Refresh(ctx); err != nil {
		return PublicUser{}, SecurityPolicy{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[username]
	if !ok {
		return PublicUser{}, SecurityPolicy{}, errors.New("user not found")
	}
	return user.public(s.policy), s.policy, nil
}

func (s *UserStore) Policy(ctx context.Context) (SecurityPolicy, error) {
	if err := s.Refresh(ctx); err != nil {
		return SecurityPolicy{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy, nil
}

func (s *UserStore) UpdatePolicy(ctx context.Context, policy SecurityPolicy) error {
	policy = policy.Normalized()
	return s.mutate(ctx, func(document *IdentityDocument) error {
		previous := document.Policy.Normalized()
		document.Policy = policy
		if previous.MFAMode != policy.MFAMode {
			for index := range document.Users {
				if policy.RequiresMFA(document.Users[index].Role) && !document.Users[index].MFAEnabled {
					document.Users[index].SessionVersion++
					document.Users[index].UpdatedAt = time.Now().UTC()
				}
			}
		}
		return nil
	})
}

type CreateUserRequest struct {
	Username string
	Email    string
	Password string
	Role     Role
}

func (s *UserStore) Create(ctx context.Context, request CreateUserRequest) error {
	request.Username = strings.TrimSpace(request.Username)
	request.Email = normalizeEmail(request.Email)
	if request.Username == "" || request.Password == "" {
		return errors.New("username and password are required")
	}
	if len(request.Username) > 128 || strings.ContainsAny(request.Username, "\r\n\t/\\") {
		return errors.New("username must be 128 characters or fewer and cannot contain control or path characters")
	}
	if request.Email != "" && !validEmail(request.Email) {
		return errors.New("email address is invalid")
	}
	request.Role = ParseRole(string(request.Role))
	return s.mutate(ctx, func(document *IdentityDocument) error {
		if findUser(document, request.Username) != nil {
			return errors.New("user already exists")
		}
		if request.Email != "" && emailInUse(document, request.Email, "") {
			return errors.New("email address is already assigned")
		}
		if err := validatePassword(document.Policy, request.Password, request.Username, request.Email); err != nil {
			return err
		}
		hash, err := hashPassword(request.Password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		now := time.Now().UTC()
		document.Users = append(document.Users, LocalUser{
			Username: request.Username, Email: request.Email, Password: hash, Role: request.Role,
			SessionVersion: 1, CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now,
		})
		return nil
	})
}

type AdminUserUpdate struct {
	Email    *string
	Password string
	Role     *Role
	Disabled *bool
	ResetMFA bool
}

func (s *UserStore) UpdateAdmin(ctx context.Context, username string, update AdminUserUpdate) error {
	return s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil {
			return errors.New("user not found")
		}
		wasAdmin := user.Role == RoleAdmin && !user.Disabled
		if update.Email != nil {
			email := normalizeEmail(*update.Email)
			if email != "" && !validEmail(email) {
				return errors.New("email address is invalid")
			}
			if email != "" && emailInUse(document, email, username) {
				return errors.New("email address is already assigned")
			}
			user.Email = email
		}
		if update.Role != nil {
			user.Role = ParseRole(string(*update.Role))
		}
		if update.Disabled != nil {
			user.Disabled = *update.Disabled
		}
		if wasAdmin && (user.Role != RoleAdmin || user.Disabled) && activeAdminCount(document) <= 1 {
			return errors.New("cannot remove or disable the last active admin")
		}
		if update.Password != "" {
			if err := replacePassword(document.Policy, user, update.Password); err != nil {
				return err
			}
		}
		if update.ResetMFA {
			clearMFA(user)
		}
		user.SessionVersion++
		user.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *UserStore) Delete(ctx context.Context, username string) error {
	return s.mutate(ctx, func(document *IdentityDocument) error {
		for index := range document.Users {
			user := document.Users[index]
			if user.Username != username {
				continue
			}
			if user.Role == RoleAdmin && !user.Disabled && activeAdminCount(document) <= 1 {
				return errors.New("cannot delete the last active admin")
			}
			document.Users = append(document.Users[:index], document.Users[index+1:]...)
			return nil
		}
		return errors.New("user not found")
	})
}

func (s *UserStore) ChangePassword(ctx context.Context, username, currentPassword, newPassword string) error {
	return s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil || user.Disabled {
			return ErrInvalidCredentials
		}
		if valid, _ := verifyPassword(user.Password, currentPassword); !valid {
			return ErrInvalidCredentials
		}
		return replacePassword(document.Policy, user, newPassword)
	})
}

func replacePassword(policy SecurityPolicy, user *LocalUser, newPassword string) error {
	if err := validatePassword(policy, newPassword, user.Username, user.Email); err != nil {
		return err
	}
	for _, previous := range append([]string{user.Password}, user.PasswordHistory...) {
		if valid, _ := verifyPassword(previous, newPassword); valid {
			return errors.New("new password was used recently; choose a different passphrase")
		}
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	history := append([]string{user.Password}, user.PasswordHistory...)
	limit := policy.Normalized().PasswordHistory
	if len(history) > limit {
		history = history[:limit]
	}
	user.PasswordHistory = history
	user.Password = hash
	user.PasswordChangedAt = time.Now().UTC()
	user.UpdatedAt = user.PasswordChangedAt
	user.SessionVersion++
	return nil
}

func (s *UserStore) ChangeEmail(ctx context.Context, username, currentPassword, email string) error {
	email = normalizeEmail(email)
	if email != "" && !validEmail(email) {
		return errors.New("email address is invalid")
	}
	return s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil || user.Disabled {
			return ErrInvalidCredentials
		}
		if valid, _ := verifyPassword(user.Password, currentPassword); !valid {
			return ErrInvalidCredentials
		}
		if email != "" && emailInUse(document, email, username) {
			return errors.New("email address is already assigned")
		}
		user.Email = email
		user.UpdatedAt = time.Now().UTC()
		user.SessionVersion++
		return nil
	})
}

type MFAEnrollment struct {
	Secret        string   `json:"secret"`
	OTPAuthURI    string   `json:"otpauthUri"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

func (s *UserStore) BeginMFAEnrollment(ctx context.Context, username, currentPassword string) (MFAEnrollment, error) {
	var enrollment MFAEnrollment
	err := s.mutate(ctx, func(document *IdentityDocument) error {
		if !document.Policy.MFAAvailable() {
			return errors.New("multi-factor authentication is disabled by the administrator")
		}
		user := findUser(document, username)
		if user == nil || user.Disabled {
			return ErrInvalidCredentials
		}
		if valid, _ := verifyPassword(user.Password, currentPassword); !valid {
			return ErrInvalidCredentials
		}
		secret, err := newTOTPSecret()
		if err != nil {
			return err
		}
		plain, hashes, err := newRecoveryCodes(10)
		if err != nil {
			return err
		}
		user.PendingTOTPSecret = secret
		user.RecoveryCodeHashes = hashes
		user.UpdatedAt = time.Now().UTC()
		enrollment = MFAEnrollment{Secret: secret, OTPAuthURI: totpURI("Highland", username, secret), RecoveryCodes: plain}
		return nil
	})
	return enrollment, err
}

func (s *UserStore) ConfirmMFAEnrollment(ctx context.Context, username, code string) error {
	return s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil || user.PendingTOTPSecret == "" {
			return errors.New("no pending MFA enrollment")
		}
		if !validTOTP(user.PendingTOTPSecret, code, time.Now()) {
			return errors.New("verification code is invalid")
		}
		user.TOTPSecret = user.PendingTOTPSecret
		user.PendingTOTPSecret = ""
		user.MFAEnabled = true
		user.SessionVersion++
		user.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *UserStore) DisableMFA(ctx context.Context, username, currentPassword, code string) error {
	return s.mutate(ctx, func(document *IdentityDocument) error {
		user := findUser(document, username)
		if user == nil || !user.MFAEnabled {
			return errors.New("MFA is not enabled")
		}
		if document.Policy.RequiresMFA(user.Role) {
			return errors.New("MFA is required for this account by administrator policy")
		}
		if valid, _ := verifyPassword(user.Password, currentPassword); !valid {
			return ErrInvalidCredentials
		}
		if !validTOTP(user.TOTPSecret, code, time.Now()) {
			return errors.New("verification code is invalid")
		}
		clearMFA(user)
		user.SessionVersion++
		user.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func clearMFA(user *LocalUser) {
	user.MFAEnabled = false
	user.TOTPSecret = ""
	user.PendingTOTPSecret = ""
	user.RecoveryCodeHashes = nil
}

func (s *UserStore) RecordAuthentication(ctx context.Context, username string) {
	_ = s.mutate(ctx, func(document *IdentityDocument) error {
		if user := findUser(document, username); user != nil {
			user.LastAuthenticatedAt = time.Now().UTC()
		}
		return nil
	})
}

func (s *UserStore) mutate(ctx context.Context, change func(*IdentityDocument) error) error {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence != nil {
		document, err := persistence.Update(ctx, func(document *IdentityDocument) error {
			document.Policy = document.Policy.Normalized()
			if err := change(document); err != nil {
				return err
			}
			document.Version = 1
			return nil
		})
		if err != nil {
			return err
		}
		s.applyDocument(document)
		return nil
	}
	s.mu.Lock()
	document := s.documentLocked()
	if err := change(&document); err != nil {
		s.mu.Unlock()
		return err
	}
	s.applyDocumentLocked(document)
	err := s.persistFileLocked()
	s.mu.Unlock()
	return err
}

func (s *UserStore) applyDocument(document IdentityDocument) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDocumentLocked(document)
}

func (s *UserStore) applyDocumentLocked(document IdentityDocument) {
	users := make(map[string]LocalUser, len(document.Users))
	for _, user := range document.Users {
		user.Username = strings.TrimSpace(user.Username)
		if user.Username == "" || user.Password == "" {
			continue
		}
		user.Role = ParseRole(string(user.Role))
		if user.SessionVersion < 1 {
			user.SessionVersion = 1
		}
		users[user.Username] = user
	}
	s.users = users
	s.policy = document.Policy.Normalized()
}

func (s *UserStore) documentLocked() IdentityDocument {
	users := make([]LocalUser, 0, len(s.users))
	for _, user := range s.users {
		copy := user
		copy.PasswordHistory = append([]string(nil), user.PasswordHistory...)
		copy.RecoveryCodeHashes = append([]string(nil), user.RecoveryCodeHashes...)
		users = append(users, copy)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return IdentityDocument{Version: 1, Policy: s.policy.Normalized(), Users: users}
}

func (s *UserStore) persistFile() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persistFileLocked()
}

func (s *UserStore) persistFileLocked() error {
	if s.filePath == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(s.documentLocked(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, encoded, 0o600)
}

func findUser(document *IdentityDocument, username string) *LocalUser {
	for index := range document.Users {
		if document.Users[index].Username == username {
			return &document.Users[index]
		}
	}
	return nil
}

func activeAdminCount(document *IdentityDocument) int {
	count := 0
	for _, user := range document.Users {
		if user.Role == RoleAdmin && !user.Disabled {
			count++
		}
	}
	return count
}

func emailInUse(document *IdentityDocument, email, exceptUsername string) bool {
	for _, user := range document.Users {
		if user.Username != exceptUsername && strings.EqualFold(user.Email, email) {
			return true
		}
	}
	return false
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validEmail(value string) bool {
	if len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@")
}

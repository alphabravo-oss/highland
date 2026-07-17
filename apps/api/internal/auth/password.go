package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argonMemory      = 19 * 1024
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLength  = 16
	argonKeyLength   = 32
)

func ensureHash(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	if strings.HasPrefix(password, "$argon2id$") || strings.HasPrefix(password, "$2") {
		return password, nil
	}
	return hashPassword(password)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(encoded, password string) (valid, legacy bool) {
	if strings.HasPrefix(encoded, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) == nil, true
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, false
	}
	var memory uint32
	var iterations uint32
	var parallelism uint8
	for _, parameter := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(parameter, "=", 2)
		if len(pair) != 2 {
			return false, false
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false, false
		}
		switch pair[0] {
		case "m":
			memory = uint32(value)
		case "t":
			iterations = uint32(value)
		case "p":
			parallelism = uint8(value)
		}
	}
	if memory == 0 || iterations == 0 || parallelism == 0 || memory > 256*1024 || iterations > 10 || parallelism > 16 {
		return false, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, false
}

var commonPasswords = map[string]struct{}{
	"123456789012345": {}, "adminadminadmin": {}, "changemechangeme": {},
	"correcthorsebatterystaple": {}, "letmeinletmein": {}, "passwordpassword": {},
	"password123456": {}, "qwertyqwerty123": {}, "welcome1234567": {},
	"longhorn-highland": {}, "highland-highland": {},
}

func validatePassword(policy SecurityPolicy, password, username, email string) error {
	policy = policy.Normalized()
	length := utf8.RuneCountInString(password)
	if length < policy.MinimumPasswordLength {
		return fmt.Errorf("password must contain at least %d characters", policy.MinimumPasswordLength)
	}
	if length > policy.MaximumPasswordLength || len(password) > 1024 {
		return fmt.Errorf("password must contain no more than %d characters", policy.MaximumPasswordLength)
	}
	if strings.ContainsRune(password, '\x00') || !utf8.ValidString(password) {
		return errors.New("password must be valid text and cannot contain null characters")
	}
	if policy.BlockCommonPasswords {
		normalized := strings.ToLower(strings.TrimSpace(password))
		if _, blocked := commonPasswords[normalized]; blocked {
			return errors.New("password is commonly used or specific to Highland; choose a longer unique passphrase")
		}
		for _, personal := range []string{strings.ToLower(username), strings.ToLower(strings.Split(email, "@")[0])} {
			if len(personal) >= 4 && strings.Contains(normalized, personal) {
				return errors.New("password cannot contain the username or email name")
			}
		}
	}
	return nil
}

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 interoperability requires HMAC-SHA1; not used as a collision hash.
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const totpPeriod = 30

func newTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func totpURI(issuer, username, secret string) string {
	label := url.PathEscape(issuer + ":" + username)
	query := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {strconv.Itoa(totpPeriod)}}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func validTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	for offset := int64(-1); offset <= 1; offset++ {
		if hmac.Equal([]byte(totpCode(secret, now.Unix()/totpPeriod+offset)), []byte(code)) {
			return true
		}
	}
	return false
}

func totpCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := (binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", value)
}

func newRecoveryCodes(count int) ([]string, []string, error) {
	plain := make([]string, 0, count)
	hashes := make([]string, 0, count)
	for range count {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		encoded := strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
		code := encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16]
		plain = append(plain, code)
		hashes = append(hashes, recoveryCodeHash(code))
	}
	return plain, hashes, nil
}

func recoveryCodeHash(code string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

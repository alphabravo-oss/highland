package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type mfaChallengePayload struct {
	Username string `json:"u"`
	Expires  int64  `json:"e"`
}

type MFAChallengeSigner struct {
	key []byte
	ttl time.Duration
}

func NewMFAChallengeSigner(key []byte) *MFAChallengeSigner {
	copy := append([]byte(nil), key...)
	return &MFAChallengeSigner{key: copy, ttl: 5 * time.Minute}
}

func (s *MFAChallengeSigner) Issue(username string) (string, error) {
	payload, err := json.Marshal(mfaChallengePayload{Username: username, Expires: time.Now().Add(s.ttl).Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + s.sign(encoded), nil
}

func (s *MFAChallengeSigner) Verify(token string) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || !hmac.Equal([]byte(s.sign(parts[0])), []byte(parts[1])) {
		return "", errors.New("invalid MFA challenge")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("invalid MFA challenge")
	}
	var payload mfaChallengePayload
	if json.Unmarshal(raw, &payload) != nil || payload.Username == "" || time.Now().Unix() > payload.Expires {
		return "", errors.New("expired MFA challenge")
	}
	return payload.Username, nil
}

func (s *MFAChallengeSigner) sign(message string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("highland:mfa-challenge:v1:" + message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

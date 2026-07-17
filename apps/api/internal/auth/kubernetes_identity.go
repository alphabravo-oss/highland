package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const identityDataKey = "identity.json"

type KubernetesIdentityPersistence struct {
	client     kubernetes.Interface
	namespace  string
	secretName string
	aead       cipher.AEAD
}

func NewKubernetesIdentityPersistence(client kubernetes.Interface, namespace, secretName string, encryptionKey []byte) (*KubernetesIdentityPersistence, error) {
	if client == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(secretName) == "" {
		return nil, errors.New("Kubernetes client, namespace, and identity Secret name are required")
	}
	if len(encryptionKey) < 32 {
		return nil, errors.New("identity encryption key must contain at least 32 bytes")
	}
	derived := sha256.Sum256(encryptionKey)
	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return nil, fmt.Errorf("create identity cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create identity AEAD: %w", err)
	}
	return &KubernetesIdentityPersistence{client: client, namespace: namespace, secretName: secretName, aead: aead}, nil
}

func (p *KubernetesIdentityPersistence) Load(ctx context.Context) (IdentityDocument, error) {
	secret, err := p.client.CoreV1().Secrets(p.namespace).Get(ctx, p.secretName, metav1.GetOptions{})
	if err != nil {
		return IdentityDocument{}, fmt.Errorf("get identity Secret: %w", err)
	}
	return p.decode(secret.Data[identityDataKey])
}

func (p *KubernetesIdentityPersistence) Update(ctx context.Context, mutate func(*IdentityDocument) error) (IdentityDocument, error) {
	var updated IdentityDocument
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := p.client.CoreV1().Secrets(p.namespace).Get(ctx, p.secretName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("identity Secret %s/%s is missing; install it with the Highland chart", p.namespace, p.secretName)
			}
			return err
		}
		document, err := p.decode(secret.Data[identityDataKey])
		if err != nil {
			return err
		}
		if err := mutate(&document); err != nil {
			return err
		}
		encoded, err := p.encode(document)
		if err != nil {
			return err
		}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data[identityDataKey] = encoded
		if _, err := p.client.CoreV1().Secrets(p.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return err
		}
		updated = document
		return nil
	})
	if err != nil {
		return IdentityDocument{}, fmt.Errorf("update identity Secret: %w", err)
	}
	return updated, nil
}

func (p *KubernetesIdentityPersistence) decode(raw []byte) (IdentityDocument, error) {
	if len(raw) == 0 {
		return IdentityDocument{Version: 1, Policy: DefaultSecurityPolicy()}, nil
	}
	var document IdentityDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return IdentityDocument{}, fmt.Errorf("decode identity document: %w", err)
	}
	for index := range document.Users {
		for _, field := range []*string{&document.Users[index].TOTPSecret, &document.Users[index].PendingTOTPSecret} {
			value, err := p.decrypt(*field)
			if err != nil {
				return IdentityDocument{}, fmt.Errorf("decrypt MFA secret for %s: %w", document.Users[index].Username, err)
			}
			*field = value
		}
	}
	return document, nil
}

func (p *KubernetesIdentityPersistence) encode(document IdentityDocument) ([]byte, error) {
	copy := document
	copy.Users = append([]LocalUser(nil), document.Users...)
	for index := range copy.Users {
		for _, field := range []*string{&copy.Users[index].TOTPSecret, &copy.Users[index].PendingTOTPSecret} {
			value, err := p.encrypt(*field)
			if err != nil {
				return nil, fmt.Errorf("encrypt MFA secret for %s: %w", copy.Users[index].Username, err)
			}
			*field = value
		}
	}
	encoded, err := json.Marshal(copy)
	if err != nil {
		return nil, fmt.Errorf("encode identity document: %w", err)
	}
	if len(encoded) > 900*1024 {
		return nil, errors.New("identity document exceeds the safe Kubernetes Secret size")
	}
	return encoded, nil
}

func (p *KubernetesIdentityPersistence) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := p.aead.Seal(nil, nonce, []byte(plaintext), []byte(p.secretName))
	payload := append(nonce, ciphertext...)
	return "enc:v1:" + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (p *KubernetesIdentityPersistence) decrypt(encoded string) (string, error) {
	if encoded == "" || !strings.HasPrefix(encoded, "enc:v1:") {
		// Accept legacy plaintext once; the next update migrates it to AEAD.
		return encoded, nil
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:v1:"))
	if err != nil || len(payload) <= p.aead.NonceSize() {
		return "", errors.New("invalid encrypted identity value")
	}
	plaintext, err := p.aead.Open(nil, payload[:p.aead.NonceSize()], payload[p.aead.NonceSize():], []byte(p.secretName))
	if err != nil {
		return "", errors.New("identity value authentication failed")
	}
	return string(plaintext), nil
}

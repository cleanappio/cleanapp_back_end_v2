package publicdiscovery

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	TokenPrefix = "gdt_"
	tokenAAD    = "cleanapp-public-discovery-v1"
)

type Kind string

const (
	KindReport Kind = "report"
	KindBrand  Kind = "brand"
)

type Payload struct {
	Version        int    `json:"v"`
	Kind           Kind   `json:"k"`
	Classification string `json:"c,omitempty"`
	PublicID       string `json:"p,omitempty"`
	BrandName      string `json:"b,omitempty"`
	ExpiresAtUnix  int64  `json:"e"`
}

type Codec struct {
	aead cipher.AEAD
}

func NewCodec(secret string) (*Codec, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("public discovery secret is required")
	}

	key := sha256.Sum256([]byte(secret + "|" + tokenAAD))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Codec{aead: aead}, nil
}

func (c *Codec) Seal(payload Payload) (string, error) {
	payload.Version = 1
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nil, nonce, raw, []byte(tokenAAD))
	tokenBytes := append(nonce, ciphertext...)
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(tokenBytes), nil
}

func (c *Codec) Open(token string) (Payload, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return Payload{}, fmt.Errorf("invalid token prefix")
	}

	rawToken := strings.TrimPrefix(token, TokenPrefix)
	tokenBytes, err := base64.RawURLEncoding.DecodeString(rawToken)
	if err != nil {
		return Payload{}, fmt.Errorf("decode token: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	if len(tokenBytes) <= nonceSize {
		return Payload{}, fmt.Errorf("token too short")
	}

	nonce := tokenBytes[:nonceSize]
	ciphertext := tokenBytes[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, []byte(tokenAAD))
	if err != nil {
		return Payload{}, fmt.Errorf("decrypt token: %w", err)
	}

	var payload Payload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return Payload{}, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.Version != 1 {
		return Payload{}, fmt.Errorf("unsupported token version")
	}
	if payload.ExpiresAtUnix <= 0 {
		return Payload{}, fmt.Errorf("token missing expiry")
	}
	if time.Now().Unix() > payload.ExpiresAtUnix {
		return Payload{}, fmt.Errorf("token expired")
	}
	return payload, nil
}

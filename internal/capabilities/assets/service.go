// SPDX-License-Identifier: AGPL-3.0-or-later

// Package assets signs and verifies browser-safe references to credentialed upstream images.
package assets

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("assets: invalid token")

type Payload struct {
	V                  int    `json:"v"`
	Connection         string `json:"c"`
	ConnectionRevision string `json:"cf"`
	Path               string `json:"p"`
	Query              string `json:"q,omitempty"`
	Transform          string `json:"t,omitempty"`
	Plugin             string `json:"pl"`
	Expires            int64  `json:"exp"`
}

type Service struct {
	key []byte
	now func() time.Time
}

func New(key []byte) (*Service, error) {
	if len(key) < 32 {
		return nil, errors.New("assets: signing key must be at least 32 bytes")
	}
	return &Service{key: append([]byte(nil), key...), now: time.Now}, nil
}

func NewEphemeral() *Service {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("assets: crypto/rand: " + err.Error())
	}
	s, _ := New(key)
	return s
}

func (s *Service) Mint(payload Payload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	return "v1." + encoded + "." + s.signature(encoded), nil
}

func (s *Service) Verify(token string) (Payload, error) {
	var out Payload
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return out, ErrInvalidToken
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return out, ErrInvalidToken
	}
	got, err := base64.RawURLEncoding.DecodeString(s.signature(parts[1]))
	if err != nil || !hmac.Equal(got, want) {
		return out, ErrInvalidToken
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &out) != nil || out.V != 1 || out.Connection == "" || out.ConnectionRevision == "" || out.Path == "" || out.Plugin == "" || out.Expires <= s.now().Unix() {
		return Payload{}, ErrInvalidToken
	}
	return out, nil
}

// CacheKey intentionally excludes expiry so refreshed cards reuse identical image bytes.
func CacheKey(p Payload) string {
	p.Expires = 0
	body, _ := json.Marshal(p)
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Service) signature(encoded string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

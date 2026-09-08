// SPDX-License-Identifier: AGPL-3.0-or-later

package assets

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMintVerifyAndTamper(t *testing.T) {
	s, err := New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	p := Payload{V: 1, Connection: "photos", ConnectionRevision: "opaque-random", Path: "/api/assets/a/thumbnail", Plugin: "immich@1.0.0", Expires: time.Now().Add(time.Hour).Unix()}
	token, err := s.Mint(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Verify(token)
	if err != nil || got != p {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	parts := strings.Split(token, ".")
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	if _, ok := raw["cf"]; !ok {
		t.Fatal("connection revision missing")
	}
	for key := range raw {
		if strings.Contains(strings.ToLower(key), "hash") {
			t.Fatalf("configuration hash-like field exposed: %s", key)
		}
	}
	parts[1] = "A" + parts[1][1:]
	if _, err = s.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("edited token accepted")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	s, _ := New(make([]byte, 32))
	token, _ := s.Mint(Payload{V: 1, Connection: "x", ConnectionRevision: "r", Path: "/x", Plugin: "p@1", Expires: time.Now().Add(-time.Second).Unix()})
	if _, err := s.Verify(token); err == nil {
		t.Fatal("expired token accepted")
	}
}

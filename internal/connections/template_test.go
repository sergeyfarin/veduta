// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"bytes"
	"testing"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/secrets"
)

func TestResolveRefComposesEmbeddedSecretIntoOneOpaqueValue(t *testing.T) {
	ref := config.SecretRef{Template: `MediaBrowser Token="${secret:KEY}"`, Names: []string{"KEY"}}
	got, err := resolveRef("jellyfin", "auth.value", ref, map[string]secrets.Value{"KEY": secrets.New("abc")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reveal() != `MediaBrowser Token="abc"` {
		t.Fatalf("composed=%q", got.Reveal())
	}
	if got.String() != "***" {
		t.Fatal("composed secret does not redact")
	}
}

func TestMaterialHMACChangesWithResolvedCredentialButIsStableOtherwise(t *testing.T) {
	configs := map[string]config.Connection{"photos": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: "https://photos.test", Auth: config.ConnectionAuth{Type: "bearer", Value: config.SecretRef{Name: "KEY"}}}}}
	key := bytes.Repeat([]byte{7}, 32)
	a, err := MaterialHMACs(configs, map[string]secrets.Value{"KEY": secrets.New("first")}, key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MaterialHMACs(configs, map[string]secrets.Value{"KEY": secrets.New("first")}, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := MaterialHMACs(configs, map[string]secrets.Value{"KEY": secrets.New("second")}, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a["photos"], b["photos"]) {
		t.Fatal("same resolved connection produced unstable HMAC")
	}
	if bytes.Equal(a["photos"], c["photos"]) {
		t.Fatal("credential change did not change private material HMAC")
	}
}

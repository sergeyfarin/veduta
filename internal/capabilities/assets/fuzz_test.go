// SPDX-License-Identifier: AGPL-3.0-or-later

package assets

import (
	"testing"
	"time"
)

func FuzzTokenVerifier(f *testing.F) {
	service, err := New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		f.Fatal(err)
	}
	valid, err := service.Mint(Payload{V: 1, Connection: "photos", ConnectionRevision: "revision", Path: "/asset", Plugin: "demo@1.0.0", Expires: time.Now().Add(24 * time.Hour).Unix()})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add("")
	f.Add("v1.not-base64.not-base64")
	f.Fuzz(func(t *testing.T, token string) {
		payload, verifyErr := service.Verify(token)
		if verifyErr == nil && (payload.V != 1 || payload.Connection == "" || payload.Path == "") {
			t.Fatalf("accepted token returned an incomplete payload: %+v", payload)
		}
	})
}

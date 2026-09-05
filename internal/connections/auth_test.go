// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"net/http"
	"net/url"
	"testing"

	"veduta.dev/veduta/internal/secrets"
)

func TestInjectAuth_Header(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthHeader, Name: "X-Api-Key", Value: secrets.New("k1234567890")})
	if got := req.Header.Get("X-Api-Key"); got != "k1234567890" {
		t.Fatalf("header = %q", got)
	}
}

func TestInjectAuth_Query(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthQuery, Name: "apikey", Value: secrets.New("k1234567890")})
	if got := q.Get("apikey"); got != "k1234567890" {
		t.Fatalf("query = %q", got)
	}
}

func TestInjectAuth_Bearer(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthBearer, Value: secrets.New("tok1234567890")})
	if got := req.Header.Get("Authorization"); got != "Bearer tok1234567890" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestInjectAuth_Basic(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthBasic, User: "admin", Pass: secrets.New("hunter2long")})

	wantUser, wantPass, ok := parseBasicFromExpected("admin", "hunter2long")
	if !ok {
		t.Fatal("could not build expected basic auth for comparison")
	}
	gotReq, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	gotReq.Header.Set("Authorization", req.Header.Get("Authorization"))
	user, pass, ok := gotReq.BasicAuth()
	if !ok || user != wantUser || pass != wantPass {
		t.Fatalf("BasicAuth() = %q, %q, %v; want %q, %q", user, pass, ok, wantUser, wantPass)
	}
}

func parseBasicFromExpected(user, pass string) (string, string, bool) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	req.SetBasicAuth(user, pass)
	return req.BasicAuth()
}

func TestInjectAuth_None(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthNone})
	if req.Header.Get("Authorization") != "" || len(q) != 0 {
		t.Fatal("AuthNone should inject nothing")
	}
}

// TestInjectAuth_OverridesCallerSuppliedHeader: the connection's own auth always wins over a
// caller-supplied header of the same name - a Request cannot spoof or suppress it by naming.
func TestInjectAuth_OverridesCallerSuppliedHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://x/y", nil)
	req.Header.Set("X-Api-Key", "caller-supplied-value")
	q := url.Values{}
	injectAuth(req, q, Auth{Type: AuthHeader, Name: "X-Api-Key", Value: secrets.New("real-secret-value")})
	if got := req.Header.Get("X-Api-Key"); got != "real-secret-value" {
		t.Fatalf("header = %q, want the connection's own auth to win", got)
	}
}

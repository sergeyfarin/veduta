// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"encoding/base64"
	"net/http"
	"net/url"
)

// injectAuth applies a connection's own credential to an outgoing request, overwriting anything
// a caller supplied under the same header or query key - the connection's auth is always
// authoritative, never something a caller's Request can override by coincidence of naming.
func injectAuth(req *http.Request, query url.Values, auth Auth) {
	switch auth.Type {
	case AuthHeader:
		req.Header.Set(auth.Name, auth.Value.Reveal())
	case AuthQuery:
		query.Set(auth.Name, auth.Value.Reveal())
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+auth.Value.Reveal())
	case AuthBasic:
		req.Header.Set("Authorization", basicAuthHeader(auth.User, auth.Pass.Reveal()))
	case AuthNone:
		// nothing to inject
	}
}

// basicAuthHeader mirrors net/http.Request.SetBasicAuth's own encoding exactly (RFC 7617:
// base64 of "user:pass", no line breaks) without needing a *http.Request to call it on before
// the query string (and therefore the final URL) is finalised.
func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/secrets"
)

// MaterialHMACs computes private, instance-keyed fingerprints for resolved connections. Secret
// values are revealed only inside the credential-boundary package and are never returned.
func MaterialHMACs(configs map[string]config.Connection, resolved map[string]secrets.Value, key []byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(configs))
	for id, cfg := range configs {
		conn, err := buildConnection(id, cfg, resolved)
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, key)
		writeMaterial := func(values ...string) {
			for _, value := range values {
				var size [8]byte
				binary.BigEndian.PutUint64(size[:], uint64(len(value)))
				_, _ = mac.Write(size[:])
				_, _ = mac.Write([]byte(value))
			}
		}
		writeMaterial(id, string(conn.Kind))
		if conn.HTTP != nil {
			h := conn.HTTP
			writeMaterial(h.BaseURL, string(h.Auth.Type), h.Auth.Name, h.Auth.Value.Reveal(), h.Auth.User, h.Auth.Pass.Reveal(), h.TLS.CAFile, h.TLS.ServerName, fmt.Sprint(h.TLS.InsecureSkipVerify), h.Timeout.String(), fmt.Sprint(h.MaxResponseBytes), fmt.Sprint(h.MaxRedirects), fmt.Sprint(h.RateLimit.RPS), fmt.Sprint(h.RateLimit.Burst), fmt.Sprint(h.Concurrency))
			keys := make([]string, 0, len(h.Headers))
			for k := range h.Headers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				writeMaterial(k, h.Headers[k])
			}
			paths := append([]string(nil), h.AllowedPaths...)
			sort.Strings(paths)
			writeMaterial(paths...)
		} else if conn.Docker != nil {
			writeMaterial(conn.Docker.Endpoint, fmt.Sprint(conn.Docker.AllowActions), conn.Docker.Timeout.String())
		}
		out[id] = mac.Sum(nil)
	}
	return out, nil
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	hash        []byte
}

func parseArgon2ID(encoded string) (argonParams, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argonParams{}, errors.New("auth: passwordHash must be an Argon2id PHC string at version 19")
	}
	var out argonParams
	for _, field := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return argonParams{}, errors.New("auth: malformed Argon2id parameters")
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argonParams{}, errors.New("auth: malformed Argon2id parameters")
		}
		switch key {
		case "m":
			out.memory = uint32(n)
		case "t":
			out.iterations = uint32(n)
		case "p":
			if n > 255 {
				return argonParams{}, errors.New("auth: Argon2id parallelism is too large")
			}
			out.parallelism = uint8(n)
		default:
			return argonParams{}, fmt.Errorf("auth: unknown Argon2id parameter %q", key)
		}
	}
	if err := checkCost(out.memory, out.iterations, out.parallelism); err != nil {
		return argonParams{}, err
	}
	var err error
	out.salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(out.salt) < 16 || len(out.salt) > 64 {
		return argonParams{}, errors.New("auth: invalid Argon2id salt")
	}
	out.hash, err = base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(out.hash) < 16 || len(out.hash) > 64 {
		return argonParams{}, errors.New("auth: invalid Argon2id hash")
	}
	return out, nil
}

// checkCost is the one definition of what this build considers a safe Argon2id cost, applied both
// to a hash being verified and to one being generated. Sharing it is the point: a `veduta auth
// hash` that could emit parameters `serve` then refuses would be worse than having no command at
// all, since the failure would surface at start-up on the operator's server rather than here.
func checkCost(memory, iterations uint32, parallelism uint8) error {
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return errors.New("auth: Argon2id parameters are outside safe limits")
	}
	return nil
}

// HashCost is the work an Argon2id hash is generated with. Verification reads the cost from the
// hash itself, so raising these later re-costs new passwords without invalidating existing ones.
type HashCost struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultHashCost is RFC 9106's second recommended configuration: 64 MiB, three passes, four
// lanes. The first (2 GiB) is not a reasonable per-login allocation on the Pi-class hosts this
// project targets, and 64 MiB sits well inside the 256 MiB ceiling checkCost enforces.
var DefaultHashCost = HashCost{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 4}

// HashPassword returns a PHC-encoded Argon2id verifier for password, suitable for
// auth.admin.passwordHash. The result is parsed and verified before being returned, so this
// function cannot hand back a string the login path would reject.
func HashPassword(password string, cost HashCost) (string, error) {
	if password == "" {
		return "", errors.New("auth: password is empty")
	}
	if err := checkCost(cost.MemoryKiB, cost.Iterations, cost.Parallelism); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, cost.Iterations, cost.MemoryKiB, cost.Parallelism, 32)
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		cost.MemoryKiB, cost.Iterations, cost.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
	params, err := parseArgon2ID(encoded)
	if err != nil {
		return "", fmt.Errorf("auth: generated hash does not parse: %w", err)
	}
	if !verifyPassword(params, password) {
		return "", errors.New("auth: generated hash does not verify its own password")
	}
	return encoded, nil
}

func verifyPassword(params argonParams, password string) bool {
	// parseArgon2ID bounds the decoded hash to 64 bytes before this conversion.
	keyLength := uint32(len(params.hash)) // #nosec G115 -- bounded above by 64.
	actual := argon2.IDKey([]byte(password), params.salt, params.iterations, params.memory, params.parallelism, keyLength)
	return subtle.ConstantTimeCompare(actual, params.hash) == 1
}

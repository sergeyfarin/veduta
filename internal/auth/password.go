// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
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
	if out.memory < 8*1024 || out.memory > 256*1024 || out.iterations < 1 || out.iterations > 10 || out.parallelism < 1 || out.parallelism > 16 {
		return argonParams{}, errors.New("auth: Argon2id parameters are outside safe limits")
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

func verifyPassword(params argonParams, password string) bool {
	// parseArgon2ID bounds the decoded hash to 64 bytes before this conversion.
	keyLength := uint32(len(params.hash)) // #nosec G115 -- bounded above by 64.
	actual := argon2.IDKey([]byte(password), params.salt, params.iterations, params.memory, params.parallelism, keyLength)
	return subtle.ConstantTimeCompare(actual, params.hash) == 1
}

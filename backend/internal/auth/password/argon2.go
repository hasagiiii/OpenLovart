// Package password provides a thin wrapper around alexedwards/argon2id for
// hashing and verifying user passwords. Tunable parameters are exposed as
// package-level constants so a coordinated rotation only needs to be reviewed
// in one place; verifying an existing hash always uses the parameters
// embedded in the encoded string itself.
package password

import (
	"errors"
	"fmt"

	"github.com/alexedwards/argon2id"
)

// Tunable argon2id parameters. These match the project-wide guidance in
// design.md: 64 MiB memory, 3 iterations, parallelism = NumCPU floor of 2,
// 16-byte salt, 32-byte tag. Bump the constants below (memory or iterations)
// when hardware budgets allow; existing hashes remain verifiable and will be
// silently re-hashed on next successful login (see auth/service Login).
const (
	memoryKiB   uint32 = 64 * 1024 // 64 MiB
	iterations  uint32 = 3
	parallelism uint8  = 2
	saltLen     uint32 = 16
	keyLen      uint32 = 32
)

// currentParams is the single source of truth for newly-created hashes and
// for the "needs rehash" comparison performed by Verify.
var currentParams = &argon2id.Params{
	Memory:      memoryKiB,
	Iterations:  iterations,
	Parallelism: parallelism,
	SaltLength:  saltLen,
	KeyLength:   keyLen,
}

// Hash returns an argon2id-encoded string of the form
// `$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`. The encoded string is
// self-describing: Verify can read params back out without consulting any
// external state.
func Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password: plaintext must not be empty")
	}
	encoded, err := argon2id.CreateHash(plain, currentParams)
	if err != nil {
		return "", fmt.Errorf("password: hash: %w", err)
	}
	return encoded, nil
}

// Verify checks plaintext against an argon2id-encoded hash.
//
// Return values:
//   - ok          — the password matches.
//   - needsRehash — the password matches **and** the encoded params drift
//     from currentParams (caller should re-Hash and persist the new value).
//   - err         — only set when the encoded string itself is malformed;
//     a simple mismatch is reported as ok=false with err=nil so callers can
//     respond with a generic "invalid credentials" without leaking detail.
func Verify(plain, encoded string) (ok, needsRehash bool, err error) {
	if plain == "" || encoded == "" {
		return false, false, nil
	}
	match, params, err := argon2id.CheckHash(plain, encoded)
	if err != nil {
		return false, false, fmt.Errorf("password: verify: %w", err)
	}
	if !match {
		return false, false, nil
	}
	if params == nil || !paramsEqual(params, currentParams) {
		return true, true, nil
	}
	return true, false, nil
}

// paramsEqual compares two argon2id parameter sets for "is rehash needed"
// purposes. We treat any drift in the four tunables as "needs rehash";
// SaltLength is compared so that increasing the salt size also triggers a
// rehash on next login.
func paramsEqual(a, b *argon2id.Params) bool {
	return a.Memory == b.Memory &&
		a.Iterations == b.Iterations &&
		a.Parallelism == b.Parallelism &&
		a.SaltLength == b.SaltLength &&
		a.KeyLength == b.KeyLength
}

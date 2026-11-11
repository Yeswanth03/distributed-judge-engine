// Package password hashes and checks passwords with argon2id, stored in the PHC string format
// ($argon2id$v=19$m=...,t=...,p=...$salt$hash) so the parameters can be raised later without breaking old hashes.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the argon2id cost settings. Default follows OWASP's recommendation (19 MiB, 2 passes, 1 lane),
// which keeps memory use per login modest enough for small containers.
type Params struct {
	MemoryKiB  uint32
	Iterations uint32
	Threads    uint8
}

var Default = Params{MemoryKiB: 19 * 1024, Iterations: 2, Threads: 1}

const (
	saltLen = 16
	keyLen  = 32
)

var ErrMalformedHash = errors.New("malformed password hash")

// Hash returns the encoded argon2id hash of password with a fresh random salt.
func Hash(password string, p Params) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Threads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.MemoryKiB, p.Iterations, p.Threads,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether password matches the encoded hash, comparing in constant time.
func Verify(password, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// dummyHash is checked against when an email is unknown, so "no such account" takes as long as "wrong
// password" and response times don't reveal which emails are registered.
var dummyHash = func() string {
	h, err := Hash("timing-equaliser", Default)
	if err != nil {
		panic(err)
	}
	return h
}()

// Burn does the work of one Verify and discards the result.
func Burn(password string) {
	Verify(password, dummyHash) //nolint:errcheck
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Threads); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}

package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashSaltLen = 16
	hashKeyLen  = 32
)

// Hasher derives PBKDF2-HMAC-SHA256 password hashes. Iterations is recorded
// inside the stored string so Verify always re-derives with the recorded cost.
// Production uses 600000; tests inject 1000 to keep the suite fast.
type Hasher struct {
	Iterations int
}

// Hash returns pbkdf2$sha256$<iterations>$<b64 salt>$<b64 key>.
func (h Hasher) Hash(password string) (string, error) {
	salt := make([]byte, hashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, h.Iterations, hashKeyLen)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s",
		h.Iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches the stored hash, in constant time
// over equal-length keys; malformed hashes fail closed.
func (h Hasher) Verify(stored, password string) bool {
	salt, want, iterations, ok := parseHash(stored)
	if !ok {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(key, want) == 1
}

func parseHash(stored string) (salt, key []byte, iterations int, ok bool) {
	parts := strings.Split(stored, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return nil, nil, 0, false
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations < 1 {
		return nil, nil, 0, false
	}
	salt, err = base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < hashSaltLen {
		return nil, nil, 0, false
	}
	key, err = base64.StdEncoding.DecodeString(parts[4])
	if err != nil || len(key) == 0 {
		return nil, nil, 0, false
	}
	return salt, key, iterations, true
}

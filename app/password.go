package app

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// PasswordIterations follows the OWASP 2023 recommendation for PBKDF2-SHA256.
// Tests may lower it.
var PasswordIterations = 600_000

// MinPasswordLen is enforced on setup and password change.
const MinPasswordLen = 10

var errBadHash = errors.New("geçersiz parola özeti")

// HashPassword returns "pbkdf2-sha256$iter$salt$key".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, PasswordIterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", PasswordIterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword verifies password against an encoded hash in constant time.
func CheckPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, errBadHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false, errBadHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false, errBadHash
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil {
		return false, errBadHash
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is checked when a username does not exist, so response time
// does not reveal which usernames are valid.
var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword("rampa-timing-equalizer")
	return h
})

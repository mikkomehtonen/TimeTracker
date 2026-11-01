package auth_test

import (
	"regexp"
	"testing"

	"example.com/timetracker/internal/auth"
)

var hashFormat = regexp.MustCompile(`^pbkdf2\$sha256\$\d+\$[A-Za-z0-9+/]+={0,2}\$[A-Za-z0-9+/]+={0,2}$`)

func TestHashFormatAndRoundTrip(t *testing.T) {
	h := auth.Hasher{Iterations: 1000}
	stored, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !hashFormat.MatchString(stored) {
		t.Errorf("hash %q does not match pbkdf2$sha256$<iter>$<salt>$<key>", stored)
	}
	if !h.Verify(stored, "correct horse battery") {
		t.Error("correct password did not verify")
	}
	if h.Verify(stored, "wrong password") {
		t.Error("wrong password verified")
	}
}

func TestVerifyWrongPasswordFalseNoPanic(t *testing.T) {
	h := auth.Hasher{Iterations: 1000}
	stored, err := h.Hash("password1234")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	for _, bad := range []string{"", "password123", "password12345", "PSSSWORD"} {
		if h.Verify(stored, bad) {
			t.Errorf("Verify(%q) = true, want false", bad)
		}
	}
}

func TestVerifyUsesIterationsFromHashString(t *testing.T) {
	stored, err := (auth.Hasher{Iterations: 1000}).Hash("password1234")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	// A hasher configured with a different cost must still verify, because
	// the cost is recorded inside the stored hash.
	other := auth.Hasher{Iterations: 2222}
	if !other.Verify(stored, "password1234") {
		t.Error("verify ignored the recorded iteration count")
	}
}

func TestVerifyMalformedHashFailsClosed(t *testing.T) {
	h := auth.Hasher{Iterations: 1000}
	bad := []string{"", "garbage", "pbkdf2$sha256$1000$xx$yy",
		"bcrypt$sha256$1000$c2FsdHNhbHRzYWx0c2FsdA==$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5"}
	for _, b := range bad {
		if h.Verify(b, "password1234") {
			t.Errorf("malformed hash %q verified", b)
		}
	}
}

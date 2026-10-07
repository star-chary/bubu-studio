package identity

import (
	"strings"
	"testing"
)

func TestValidatePasswordBounds(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"empty", "", false},
		{"below minimum", "12345", false},
		{"minimum", "123456", true},
		{"maximum", strings.Repeat("a", 20), true},
		{"above maximum", strings.Repeat("a", 21), false},
		{"whitespace only", "      ", false},
		{"preserve spaces", " pass ", true},
		{"unicode minimum", strings.Repeat("🔑", 6), true},
		{"unicode maximum", strings.Repeat("🔑", 20), true},
		{"unicode above maximum", strings.Repeat("🔑", 21), false},
		{"invalid utf8", "abcde\xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePassword(tc.password) == nil; got != tc.valid {
				t.Fatalf("valid = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestCredentialsAndOpaqueTokens(t *testing.T) {
	for _, email := range []string{"a+draft@company.example", "Someone@GMAIL.com", "名字@example.cn", "person@custom.photography"} {
		if _, err := Email(email); err != nil {
			t.Fatalf("valid email rejected: %s", email)
		}
	}
	for _, email := range []string{"bad", "a@", "Display Name <a@example.com>", "a@example.com\r\nX: evil", "a@.com"} {
		if _, err := Email(email); err == nil {
			t.Fatal("invalid email accepted")
		}
	}
	if v, _ := Email(" Person+draft@EXAMPLE.com "); v != "person+draft@example.com" {
		t.Fatal("email account normalization")
	}
	password := "creative passphrase"
	hash := PasswordHash(password)
	if !PasswordMatches(password, hash) || PasswordMatches(password+"x", hash) || PasswordMatches(password, "broken") {
		t.Fatal("password hash verification")
	}
	if hash == PasswordHash(password) {
		t.Fatal("missing random salt")
	}
	if ValidatePassword("short") == nil || ValidatePassword(password) != nil {
		t.Fatal("password bounds")
	}
	a, b := Token(), Token()
	if a == b || !ValidToken(a) || ValidToken("broken") || TokenHash(a) == a {
		t.Fatal("session token encoding")
	}
}

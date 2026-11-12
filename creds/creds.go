// Package creds holds the host-free parts of Subsonic authentication: checking
// the credentials a request carries against the stored password, generating a
// password, and trimming the client name a request announces.
package creds

import (
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const clientMaxLen = 64

// Check validates the auth parameters of a request against the stored
// password: the token form t=md5(password+salt), or the legacy p= in plain or
// "enc:"+hex. An empty stored password disables access entirely.
func Check(stored string, params url.Values) bool {
	if stored == "" {
		return false
	}
	if t := params.Get("t"); t != "" {
		sum := md5.Sum([]byte(stored + params.Get("s"))) //nolint:gosec // see import
		return subtle.ConstantTimeCompare(
			[]byte(strings.ToLower(t)),
			[]byte(hex.EncodeToString(sum[:])),
		) == 1
	}
	p := params.Get("p")
	if hexed, ok := strings.CutPrefix(p, "enc:"); ok {
		decoded, err := hex.DecodeString(hexed)
		if err != nil {
			return false
		}
		p = string(decoded)
	}
	return p != "" && subtle.ConstantTimeCompare([]byte(p), []byte(stored)) == 1
}

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Generate returns n random letters and digits.
func Generate(n int) (string, error) {
	limit := big.NewInt(int64(len(alphabet)))
	out := make([]byte, n)
	for i := range out {
		k, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[k.Int64()]
	}
	return string(out), nil
}

// SanitizeClient trims a client name (the Subsonic c parameter) to printable
// characters and at most 64 bytes, never splitting a character.
func SanitizeClient(s string) string {
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	for len(s) > clientMaxLen {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

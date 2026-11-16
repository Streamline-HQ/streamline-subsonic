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

// Scheme is the authentication mechanism a request carries.
type Scheme int

const (
	// SchemeUser is u with p, or u with t and s: the ones Check verifies.
	// Missing credentials land here too, for Check to refuse.
	SchemeUser Scheme = iota
	// SchemeAPIKey is the OpenSubsonic apiKey alone, which this server does
	// not offer.
	SchemeAPIKey
	// SchemeConflict is more than one mechanism at once: apiKey with any of
	// u, p, t, s, or p with t or s.
	SchemeConflict
)

// Detect names the mechanism params carry. An empty value counts as absent.
func Detect(params url.Values) Scheme {
	has := func(name string) bool { return params.Get(name) != "" }
	token := has("t") || has("s")
	switch {
	case has("apiKey") && (has("u") || has("p") || token):
		return SchemeConflict
	case has("apiKey"):
		return SchemeAPIKey
	case has("p") && token:
		return SchemeConflict
	}
	return SchemeUser
}

// MissingCredential names the parameter a SchemeUser request still lacks:
// "p" when it carries neither p nor t, "s" for a t with no salt, "t" for a
// salt with no token, "" when it is complete. u is the caller's to check.
func MissingCredential(params url.Values) string {
	has := func(name string) bool { return params.Get(name) != "" }
	switch {
	case has("p"):
		return ""
	case has("t") && !has("s"):
		return "s"
	case has("s") && !has("t"):
		return "t"
	case !has("t"):
		return "p"
	}
	return ""
}

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

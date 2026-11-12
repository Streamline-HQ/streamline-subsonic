package creds

import (
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"net/url"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("credentials", g.Label("unit"), func() {
	g.It("accepts a valid token: t = md5(password + salt)", func() {
		sum := md5.Sum([]byte("sesame" + "abc123")) //nolint:gosec // see import
		token := hex.EncodeToString(sum[:])
		Expect(
			Check("sesame", url.Values{"t": {token}, "s": {"abc123"}}),
		).To(BeTrue())
		Expect(
			Check(
				"sesame",
				url.Values{"t": {strings.ToUpper(token)}, "s": {"abc123"}},
			),
		).To(BeTrue())
	})

	g.It("accepts legacy p= plain and enc: hex forms", func() {
		Expect(Check("sesame", url.Values{"p": {"sesame"}})).To(BeTrue())
		Expect(
			Check(
				"sesame",
				url.Values{"p": {"enc:" + hex.EncodeToString([]byte("sesame"))}},
			),
		).To(BeTrue())
	})

	g.It("rejects a wrong token, a wrong salt and a malformed enc:", func() {
		Expect(Check("sesame", url.Values{"t": {"beef"}, "s": {"x"}})).To(BeFalse())
		sum := md5.Sum([]byte("sesame" + "abc123")) //nolint:gosec // see import
		Expect(
			Check("sesame", url.Values{
				"t": {hex.EncodeToString(sum[:])},
				"s": {"other"},
			}),
		).To(BeFalse())
		Expect(Check("sesame", url.Values{"p": {"enc:zz"}})).To(BeFalse())
		Expect(Check("sesame", url.Values{"p": {"Sesame"}})).To(BeFalse())
	})

	g.It("disables access for an empty stored password", func() {
		Expect(Check("", url.Values{"p": {""}})).To(BeFalse())
		sum := md5.Sum([]byte("")) //nolint:gosec // see import
		Expect(
			Check("", url.Values{"t": {hex.EncodeToString(sum[:])}}),
		).To(BeFalse())
	})

	g.It("refuses a request carrying no credentials", func() {
		Expect(Check("sesame", url.Values{})).To(BeFalse())
	})

	g.It("generates passwords of letters and digits", func() {
		a, err := Generate(24)
		Expect(err).NotTo(HaveOccurred())
		b, err := Generate(24)
		Expect(err).NotTo(HaveOccurred())
		Expect(a).To(MatchRegexp(`^[A-Za-z0-9]{24}$`))
		Expect(a).NotTo(Equal(b))
	})

	g.It("trims a client name to printable characters and 64 bytes", func() {
		Expect(SanitizeClient("  Symfonium\x00\n ")).To(Equal("Symfonium"))
		long := strings.Repeat("é", 40)
		got := SanitizeClient(long)
		Expect(len(got)).To(BeNumerically("<=", 64))
		Expect(strings.Trim(got, "é")).To(BeEmpty())
	})
})

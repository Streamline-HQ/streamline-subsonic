package envelope

import (
	"encoding/json"
	"errors"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// versionCode is the code CheckVersion answers v with.
func versionCode(v string) int {
	var apiErr *Error
	Expect(errors.As(CheckVersion(v), &apiErr)).To(BeTrue(), v)
	return apiErr.Code
}

type doc struct {
	Head
	Error *Error `xml:"error,omitempty" json:"error,omitempty"`
}

// wrongCredentialsXML is error 40 as an XML document.
func wrongCredentialsXML() []byte {
	_, out, err := Encode("", "", "1", &doc{
		Status: StatusFailed, Error: WrongCredentials(),
	})
	Expect(err).NotTo(HaveOccurred())
	return out
}

var _ = g.Describe("envelope", g.Label("unit"), func() {
	g.It("writes the XML envelope by default", func() {
		contentType, out, err := Encode(
			"",
			"",
			"1.2.3",
			&doc{Status: StatusOK},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(contentType).To(ContainSubstring("text/xml"))
		Expect(string(out)).To(HavePrefix(`<?xml version="1.0" encoding="UTF-8"?>`))
		Expect(string(out)).To(ContainSubstring(`<subsonic-response`))
		Expect(
			string(out),
		).To(ContainSubstring(`xmlns="http://subsonic.org/restapi"`))
		Expect(string(out)).To(ContainSubstring(`status="ok"`))
		Expect(string(out)).To(ContainSubstring(`version="1.16.1"`))
		Expect(string(out)).To(ContainSubstring(`type="streamline"`))
		Expect(string(out)).To(ContainSubstring(`serverVersion="1.2.3"`))
		Expect(string(out)).To(ContainSubstring(`openSubsonic="true"`))
	})

	g.It("falls back to XML for a format it does not know", func() {
		contentType, _, err := Encode(
			"yaml",
			"",
			"1",
			&doc{Status: StatusOK},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(contentType).To(ContainSubstring("text/xml"))
	})

	g.It("wraps the JSON in the callback when f=jsonp", func() {
		contentType, out, err := Encode(
			FormatJSONP, "app.cb", "1.2.3", &doc{Status: StatusOK},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(contentType).To(ContainSubstring("application/javascript"))
		Expect(string(out)).To(HavePrefix(`app.cb({"subsonic-response":{`))
		Expect(string(out)).To(HaveSuffix(`});`))
	})

	g.It("accepts only an identifier or a dotted path as a callback", func() {
		for _, ok := range []string{"cb", "_x1", "$", "a.b.c", "jQuery_123"} {
			Expect(ValidCallback(ok)).To(BeTrue(), ok)
		}
		for _, bad := range []string{
			"", "1a", "a.", ".a", "a..b", "a(b)", "alert(1);x", "a-b", "a b",
			"</script>",
		} {
			Expect(ValidCallback(bad)).To(BeFalse(), bad)
		}
	})

	g.It("refuses a client too old or too new for its protocol", func() {
		Expect(CheckVersion("1.16.1")).To(Succeed())
		Expect(CheckVersion("1.16.9")).To(Succeed())
		Expect(CheckVersion("1.2.0")).To(Succeed())
		Expect(CheckVersion("garbage")).To(Succeed())
		Expect(versionCode("0.9.0")).To(Equal(CodeClientTooOld))
		Expect(versionCode("2.0.0")).To(Equal(CodeServerTooOld))
		Expect(versionCode("1.17.0")).To(Equal(CodeServerTooOld))
	})

	g.It("points the unsupported mechanism error at the sign-in docs", func() {
		_, out, err := Encode(FormatJSON, "", "1", &doc{
			Status: StatusFailed, Error: AuthNotSupported(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"helpUrl":"` + HelpURL + `"`))
		Expect(string(wrongCredentialsXML())).NotTo(ContainSubstring("helpUrl"))
	})

	g.It("writes JSON when f=json", func() {
		contentType, out, err := Encode(
			FormatJSON, "", "1.2.3", &doc{Status: StatusOK},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(contentType).To(ContainSubstring("application/json"))
		var payload map[string]map[string]any
		Expect(json.Unmarshal(out, &payload)).To(Succeed())
		resp := payload["subsonic-response"]
		Expect(resp["status"]).To(Equal("ok"))
		Expect(resp["version"]).To(Equal("1.16.1"))
		Expect(resp["type"]).To(Equal("streamline"))
		Expect(resp["serverVersion"]).To(Equal("1.2.3"))
		Expect(resp["openSubsonic"]).To(BeTrue())
		Expect(resp).NotTo(HaveKey("xmlns"))
	})

	g.It("writes spec error codes", func() {
		_, out, err := Encode(FormatJSON, "", "1", &doc{
			Status: StatusFailed,
			Error:  WrongCredentials(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"code":40`))
		Expect(string(out)).To(ContainSubstring(`"status":"failed"`))
	})

	g.It("names the missing parameter", func() {
		Expect(
			MissingParam("id").Error(),
		).To(Equal("Required parameter is missing: id"))
		Expect(MissingParam("id").Code).To(Equal(CodeMissingParam))
	})
})

package envelope

import (
	"encoding/json"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type doc struct {
	Head
	Error *Error `xml:"error,omitempty" json:"error,omitempty"`
}

var _ = g.Describe("envelope", g.Label("unit"), func() {
	g.It("writes the XML envelope by default", func() {
		contentType, out, err := Encode(
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
			"jsonp",
			"1",
			&doc{Status: StatusOK},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(contentType).To(ContainSubstring("text/xml"))
	})

	g.It("writes JSON when f=json", func() {
		contentType, out, err := Encode(
			FormatJSON, "1.2.3", &doc{Status: StatusOK},
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
		_, out, err := Encode(FormatJSON, "1", &doc{
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

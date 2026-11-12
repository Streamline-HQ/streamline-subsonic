// Package envelope is the <subsonic-response> wrapper: the version header every
// answer carries, the Subsonic error codes, and the XML and JSON encodings.
package envelope

import (
	"encoding/json"
	"encoding/xml"
)

const (
	APIVersion = "1.16.1"
	// ServerType is the OpenSubsonic "type" every response carries.
	ServerType = "streamline"
	Namespace  = "http://subsonic.org/restapi"
)

const (
	CodeGeneric          = 0
	CodeMissingParam     = 10
	CodeWrongCredentials = 40
	CodeNotAuthorized    = 50
	CodeNotFound         = 70
)

const (
	StatusOK     = "ok"
	StatusFailed = "failed"

	ContentTypeXML  = "text/xml; charset=utf-8"
	ContentTypeJSON = "application/json; charset=utf-8"
)

// Error is a Subsonic error: the code and message a failed response carries.
type Error struct {
	Code    int    `xml:"code,attr"    json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

func (e *Error) Error() string { return e.Message }

func MissingParam(name string) *Error {
	return &Error{
		Code:    CodeMissingParam,
		Message: "Required parameter is missing: " + name,
	}
}

func NotFound() *Error {
	return &Error{Code: CodeNotFound, Message: "The requested data was not found"}
}

func NotAuthorized() *Error {
	return &Error{
		Code:    CodeNotAuthorized,
		Message: "User is not authorized for the given operation",
	}
}

func WrongCredentials() *Error {
	return &Error{
		Code:    CodeWrongCredentials,
		Message: "Wrong username or password",
	}
}

// Head is what a response document embeds: the root element and the
// attributes every Subsonic answer opens with. Namespace, version and type are
// filled by Encode.
type Head struct {
	XMLName       xml.Name `xml:"subsonic-response"  json:"-"`
	Xmlns         string   `xml:"xmlns,attr"         json:"-"`
	Status        string   `xml:"status,attr"        json:"status"`
	Version       string   `xml:"version,attr"       json:"version"`
	Type          string   `xml:"type,attr"          json:"type"`
	ServerVersion string   `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic  bool     `xml:"openSubsonic,attr"  json:"openSubsonic"`
}

// Document is implemented by a response struct embedding Head.
type Document interface {
	head() *Head
}

func (h *Head) head() *Head { return h }

// FormatJSON is the value of the f parameter that selects JSON; any other
// value, an absent one included, selects XML.
const FormatJSON = "json"

// Encode stamps the header onto doc and serialises it: JSON inside a
// "subsonic-response" object when format is "json", XML with a declaration
// otherwise. It returns the Content-Type to send.
func Encode(
	format, serverVersion string,
	doc Document,
) (contentType string, out []byte, err error) {
	h := doc.head()
	h.Xmlns = Namespace
	h.Version = APIVersion
	h.Type = ServerType
	h.ServerVersion = serverVersion
	h.OpenSubsonic = true

	if format == FormatJSON {
		out, err = json.Marshal(map[string]any{"subsonic-response": doc})
		return ContentTypeJSON, out, err
	}
	out, err = xml.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	return ContentTypeXML, append([]byte(xml.Header), out...), nil
}

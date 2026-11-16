// Package envelope is the <subsonic-response> wrapper: the version header every
// answer carries, the Subsonic error codes, and the XML and JSON encodings.
package envelope

import (
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

const (
	APIVersion = "1.16.1"
	// ServerType is the OpenSubsonic "type" every response carries.
	ServerType = "streamline"
	Namespace  = "http://subsonic.org/restapi"
)

const (
	CodeGeneric      = 0
	CodeMissingParam = 10
	// CodeClientTooOld and CodeServerTooOld answer a client whose v names
	// a protocol this server cannot speak.
	CodeClientTooOld     = 20
	CodeServerTooOld     = 30
	CodeWrongCredentials = 40
	// CodeAuthNotSupported answers an authentication mechanism this server
	// does not offer (the OpenSubsonic apiKey).
	CodeAuthNotSupported = 42
	// CodeConflictingAuth answers a request carrying more than one
	// authentication mechanism.
	CodeConflictingAuth = 43
	CodeNotAuthorized   = 50
	CodeNotFound        = 70
)

const (
	StatusOK     = "ok"
	StatusFailed = "failed"

	ContentTypeXML  = "text/xml; charset=utf-8"
	ContentTypeJSON = "application/json; charset=utf-8"
	// ContentTypeJavaScript is a JSONP answer's.
	ContentTypeJavaScript = "application/javascript; charset=utf-8"
)

// HelpURL is the page that explains how an app signs in, the OpenSubsonic
// helpUrl of the authentication errors.
const HelpURL = "https://github.com/streamline-hq/streamline-subsonic-ext/blob/main/docs/wiki/Subsonic.md"

// Error is a Subsonic error: the code and message a failed response carries,
// and the OpenSubsonic helpUrl when a page says more.
type Error struct {
	Code    int    `xml:"code,attr"              json:"code"`
	Message string `xml:"message,attr"           json:"message"`
	HelpURL string `xml:"helpUrl,attr,omitempty" json:"helpUrl,omitempty"`
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

func AuthNotSupported() *Error {
	return &Error{
		Code:    CodeAuthNotSupported,
		Message: "Provided authentication mechanism not supported",
		HelpURL: HelpURL,
	}
}

// CheckVersion compares the v a client sends with APIVersion: a major below
// this server's is error 20, a major above it or a newer minor error 30. The
// patch level never matters, and a v that is not a version is let through,
// as no real client sends one.
func CheckVersion(v string) error {
	clientMajor, clientMinor, ok := majorMinor(v)
	if !ok {
		return nil
	}
	serverMajor, serverMinor, _ := majorMinor(APIVersion)
	switch {
	case clientMajor < serverMajor:
		return &Error{
			Code:    CodeClientTooOld,
			Message: "Incompatible Subsonic REST protocol version. Client must upgrade.",
		}
	case clientMajor > serverMajor || clientMinor > serverMinor:
		return &Error{
			Code:    CodeServerTooOld,
			Message: "Incompatible Subsonic REST protocol version. Server must upgrade.",
		}
	}
	return nil
}

func majorMinor(v string) (major, minor int, ok bool) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func ConflictingAuth() *Error {
	return &Error{
		Code:    CodeConflictingAuth,
		Message: "Multiple conflicting authentication mechanisms provided",
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

// FormatJSON and FormatJSONP are the values of the f parameter that select
// JSON and JSONP; any other value, an absent one included, selects XML.
const (
	FormatJSON  = "json"
	FormatJSONP = "jsonp"
)

// callbackName is a JavaScript identifier or dotted path, the only callback
// a JSONP answer wraps itself in, so the callback parameter can never inject
// script.
var callbackName = regexp.MustCompile(
	`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`,
)

// ValidCallback reports whether name can be the callback of a JSONP answer.
func ValidCallback(name string) bool {
	return len(name) <= 128 && callbackName.MatchString(name)
}

// Encode stamps the header onto doc and serialises it: JSON inside a
// "subsonic-response" object when format is "json", the same wrapped in a
// call to callback when format is "jsonp", XML with a declaration otherwise.
// The caller checks callback with ValidCallback first. It returns the
// Content-Type to send.
func Encode(
	format, callback, serverVersion string,
	doc Document,
) (contentType string, out []byte, err error) {
	h := doc.head()
	h.Xmlns = Namespace
	h.Version = APIVersion
	h.Type = ServerType
	h.ServerVersion = serverVersion
	h.OpenSubsonic = true

	switch format {
	case FormatJSON:
		out, err = json.Marshal(map[string]any{"subsonic-response": doc})
		return ContentTypeJSON, out, err
	case FormatJSONP:
		out, err = json.Marshal(map[string]any{"subsonic-response": doc})
		if err != nil {
			return "", nil, err
		}
		wrapped := make([]byte, 0, len(callback)+len(out)+3)
		wrapped = append(wrapped, callback...)
		wrapped = append(wrapped, '(')
		wrapped = append(wrapped, out...)
		wrapped = append(wrapped, ");"...)
		return ContentTypeJavaScript, wrapped, nil
	}
	out, err = xml.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	return ContentTypeXML, append([]byte(xml.Header), out...), nil
}

package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

// reqCtx is what a /rest handler works with: the request, its parameters
// (query string and form body merged, as a Subsonic client may send either)
// and the authenticated account.
type reqCtx struct {
	req    sdk.Request
	params url.Values
	// user is the authenticated account; nil only while authenticating.
	user *account
}

// handler answers one /rest endpoint.
type handler func(c *reqCtx) sdk.Response

// decorator fills per-listener facts (star, rating, play count) into a body
// before respond sends it.
type decorator func(c *reqCtx, b *body) error

var decorators []decorator

// registerDecorator is called from an init; decorators run in registration
// order.
func registerDecorator(d decorator) { decorators = append(decorators, d) }

// newReqCtx parses the parameters. A POST form body is merged ahead of the
// query string, so a body value wins, as clients such as DSub and Symfonium
// that send the form in the body expect.
func newReqCtx(req sdk.Request) (*reqCtx, error) {
	params := url.Values{}
	if mediaType(
		headerValue(req, "Content-Type"),
	) == "application/x-www-form-urlencoded" &&
		(req.Method == http.MethodPost || req.Method == http.MethodPut ||
			req.Method == http.MethodPatch) {
		form, err := url.ParseQuery(string(req.Body))
		if err != nil {
			return nil, err
		}
		for k, vs := range form {
			params[k] = append(params[k], vs...)
		}
	}
	query, err := url.ParseQuery(req.Query)
	if err != nil {
		return nil, err
	}
	for k, vs := range query {
		params[k] = append(params[k], vs...)
	}
	return &reqCtx{req: req, params: params}, nil
}

func mediaType(contentType string) string {
	mt, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mt))
}

// headerValue is the first value of a request header, whatever its case.
func headerValue(req sdk.Request, name string) string {
	for k, vs := range req.Headers {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

func (c *reqCtx) header(name string) string { return headerValue(c.req, name) }

// param is the first value of a parameter, "" when absent.
func (c *reqCtx) param(name string) string { return c.params.Get(name) }

// multi is every value of a repeated parameter (id, songId, ...).
func (c *reqCtx) multi(name string) []string { return c.params[name] }

// intParam reads an integer parameter clamped to [lo, hi]; an absent or
// unreadable one is def.
func (c *reqCtx) intParam(name string, def, lo, hi int) int {
	n, err := strconv.Atoi(c.param(name))
	if err != nil {
		return def
	}
	return min(max(n, lo), hi)
}

// boolParam reads a Subsonic boolean; absent or unreadable is def.
func (c *reqCtx) boolParam(name string, def bool) bool {
	b, err := strconv.ParseBool(c.param(name))
	if err != nil {
		return def
	}
	return b
}

// millisParam reads an epoch-milliseconds parameter. present reports whether
// it was sent at all; a value that is not a number counts as not sent.
func (c *reqCtx) millisParam(name string) (t time.Time, present bool) {
	raw := c.param(name)
	if raw == "" {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}

// anyIDParam reads the named id parameter whatever kind it holds: error 10
// when absent, error 70 when malformed.
func (c *reqCtx) anyIDParam(name string) (kind string, id uint32, err error) {
	raw := c.param(name)
	if raw == "" {
		return "", 0, envelope.MissingParam(name)
	}
	kind, id, parseErr := ids.Parse(raw)
	if parseErr != nil {
		return "", 0, envelope.NotFound()
	}
	return kind, id, nil
}

// idParam reads the named id parameter and requires it to be of wantKind:
// error 70 for any other kind, as for a row that does not exist.
func (c *reqCtx) idParam(name, wantKind string) (uint32, error) {
	kind, id, err := c.anyIDParam(name)
	if err != nil {
		return 0, err
	}
	if kind != wantKind {
		return 0, envelope.NotFound()
	}
	return id, nil
}

// require refuses the call with error 50 unless the account holds p.
func (c *reqCtx) require(p permission) error {
	if !c.user.allowed(p) {
		return envelope.NotAuthorized()
	}
	return nil
}

// ok answers a successful call; a nil body is the empty response.
func (c *reqCtx) ok(b *body) sdk.Response {
	if b == nil {
		b = &body{}
	}
	return c.write(
		response{Status: envelope.StatusOK, body: *b},
	)
}

// respond decorates b with the caller's own facts, then answers.
func (c *reqCtx) respond(b *body) sdk.Response {
	for _, d := range decorators {
		if err := d(c, b); err != nil {
			return c.failErr(err)
		}
	}
	return c.ok(b)
}

// fail answers a failed call. Subsonic reports it in a 200.
func (c *reqCtx) fail(code int, msg string) sdk.Response {
	return c.write(response{
		Status: envelope.StatusFailed,
		Error:  &envelope.Error{Code: code, Message: msg},
	})
}

// failErr answers a handler's error: an *envelope.Error as itself, anything
// else as a logged "Internal error", never its text.
func (c *reqCtx) failErr(err error) sdk.Response {
	if apiErr, ok := errors.AsType[*envelope.Error](err); ok {
		return c.fail(apiErr.Code, apiErr.Message)
	}
	sdk.Log("error", "subsonic request failed", "error", err.Error())
	return c.fail(envelope.CodeGeneric, "Internal error")
}

func (c *reqCtx) write(resp response) sdk.Response {
	contentType, out, err := envelope.Encode(c.param("f"), version, &resp)
	if err != nil {
		sdk.Log("error", "encoding a subsonic response failed", "error", err.Error())
		return textResponse(http.StatusInternalServerError, "internal error")
	}
	return sdk.Response{
		Status:  http.StatusOK,
		Headers: map[string][]string{"Content-Type": {contentType}},
		Body:    out,
	}
}

func textResponse(status int, msg string) sdk.Response {
	return sdk.Response{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:    []byte(msg + "\n"),
	}
}

func notFoundResponse() sdk.Response {
	return textResponse(http.StatusNotFound, "404 page not found")
}

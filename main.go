// Command subsonic is Streamline's Subsonic and OpenSubsonic server, run as an
// extension: /rest for apps, /share for public share links. Apps sign in with
// the account's email and a Subsonic password kept in the extension's own
// database.
package main

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
)

// version is the manifest's version and the serverVersion every response
// carries.
const version = "1.0.0"

// main is never run in a reactor module. It names the two exports only so the
// unused linter sees them as entry points; nothing else calls them.
func main() { _, _ = manifest, handle }

//go:wasmexport manifest
func manifest() int32 {
	return sdk.WriteManifest(sdk.Manifest{
		Name:          "subsonic",
		Version:       version,
		StreamlineAPI: 1,
		Mounts: []sdk.Mount{
			{Prefix: "/rest", Auth: "self"},
			{Prefix: "/share", Auth: "self"},
		},
		Views: []string{
			"artists", "artist_members", "albums", "album_artists", "tracks",
			"music_credits", "music_files", "genres", "users", "user_logins",
		},
		Folders:      []string{"music", "artwork", "cache"},
		Services:     []string{"ffmpeg", "library_scan"},
		AccountCards: accountCards(),
	})
}

//go:wasmexport handle
func handle() int32 { return sdk.Dispatch(route) }

// endpoints maps a /rest endpoint name to its handler; register fills it from
// the init of each domain file.
var endpoints = map[string]handler{}

// register routes /rest/<name> and /rest/<name>.view to h. It is called from a
// file's init, so adding an endpoint never edits this file.
func register(name string, h handler) {
	endpoints[name] = h
	endpoints[name+".view"] = h
}

// shareHandler answers /share/...; the shares file sets it from its init.
var shareHandler func(sdk.Request) sdk.Response

func registerShare(h func(sdk.Request) sdk.Response) { shareHandler = h }

// openSubsonicExtensions is answered without credentials, so a client can
// learn what the server supports before it signs in.
const openSubsonicExtensions = "getOpenSubsonicExtensions"

// extensions are the OpenSubsonic extensions this server implements.
var extensions = []openSubsonicExtension{
	{Name: "formPost", Versions: []int{1}},
	{Name: "songLyrics", Versions: []int{1}},
	{Name: "transcodeOffset", Versions: []int{1}},
	{Name: "indexBasedQueue", Versions: []int{1}},
}

func route(r sdk.Request) sdk.Response {
	if err := ensureSchema(); err != nil {
		sdk.Log("error", "preparing the database failed", "error", err.Error())
		return textResponse(http.StatusInternalServerError, "internal error")
	}
	if r.Path == "/cards" || strings.HasPrefix(r.Path, "/cards/") {
		return cards(r)
	}
	if name, ok := restEndpoint(r.Path); ok {
		return serveREST(r, name)
	}
	if shareHandler == nil {
		return notFoundResponse()
	}
	return shareHandler(r)
}

// shareTokenShape is what a share token looks like: a long run of URL-safe
// characters, no endpoint name.
var shareTokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// restEndpoint tells a /rest path from a /share path. The host hands a guest
// the path relative to the mount and does not say which mount it was, so
// /rest/ping and /share/<token> arrive as /ping and /<token>: a single
// segment is /rest unless it is no endpoint of ours and looks like a token.
func restEndpoint(path string) (name string, ok bool) {
	seg := strings.TrimPrefix(path, "/")
	if strings.Contains(seg, "/") {
		return "", false
	}
	name = strings.TrimSuffix(seg, ".view")
	if _, known := endpoints[seg]; known || name == openSubsonicExtensions {
		return name, true
	}
	return name, !shareTokenShape.MatchString(seg)
}

func serveREST(r sdk.Request, name string) sdk.Response {
	c, err := newReqCtx(r)
	if err != nil {
		return (&reqCtx{req: r}).fail(envelope.CodeGeneric, "Malformed request")
	}
	if name == openSubsonicExtensions {
		return c.ok(&body{OpenSubsonicExtensions: extensions})
	}
	if err := c.authenticate(); err != nil {
		return c.failErr(err)
	}
	h, found := endpoints[name]
	if !found {
		return c.fail(envelope.CodeGeneric, "Not implemented")
	}
	return callHandler(c, name, h)
}

// callHandler runs h and turns a panic into the generic Subsonic error, so a
// bug in one endpoint answers like any other failure and not as a bare 500.
func callHandler(c *reqCtx, name string, h handler) sdk.Response {
	var resp sdk.Response
	func() {
		defer func() {
			if p := recover(); p != nil {
				sdk.Log("error", "a subsonic handler panicked", "endpoint", name)
				resp = c.fail(envelope.CodeGeneric, "Internal error")
			}
		}()
		resp = h(c)
	}()
	return resp
}

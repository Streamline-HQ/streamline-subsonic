# Contributing

Thanks for taking an interest. Bug reports and small focused PRs are the most
useful things you can send.

## Before you write code

- **Bug fix**: just open a PR. Link the issue if there is one. For an app that
  misbehaves, say which app and version, and what the same request does against
  another Subsonic server if you can.
- **New endpoint or OpenSubsonic extension**: open an issue first so we can
  agree on the shape before you spend an evening on it.
- **A change Streamline has to make** (a view column, an SDK function, how
  extensions are served) belongs in
  [Streamline](https://github.com/DataHearth/streamline), not here.

## Setup

Requires Go (the `go` directive in `go.mod`), [Task](https://taskfile.dev), and
for the end-to-end suite `ffmpeg` on `PATH` and a Streamline binary built from a
version that serves extension contract 2.

```bash
git clone https://github.com/Streamline-HQ/streamline-subsonic-ext.git
cd streamline-subsonic
task build              # subsonic.wasm
```

The extension SDK is the `github.com/datahearth/streamline/extensions/sdk`
module, pinned in `go.mod` to a Streamline commit. To build against a local
Streamline checkout instead, add it to your own `go.work` with
`go work use ../streamline/extensions/sdk`, and keep that out of the commit.

## The loop

```bash
task lint                                             # guest as wasip1, then e2e/
task fmt
task test:unit                                        # host-free packages, natively
STREAMLINE_BIN=../streamline/streamline task test:e2e
```

CI runs `task build`, `task lint` and `task test:unit`. `go.work` joins the
guest module and `e2e/` (a module of its own, since it runs natively).
`go tool ginkgo` and `go tool golangci-lint` resolve from the root `go.mod`.

## Conventions

A Streamline extension: one Go module built for `wasip1` into `subsonic.wasm`,
importing only the Streamline SDK. It serves `/rest` (the Subsonic API) and
`/share` (public share pages). The host contract is in the Streamline
repository: the
[Writing an extension](https://github.com/DataHearth/streamline/wiki/Writing-an-extension)
page and `extensions/sdk`. The rest of this file is the extension's own house
style.

- **Comments**: only where the code cannot explain itself.
- **Tests**: Ginkgo + Gomega. Pure packages carry `Label("unit")` specs, the
  end-to-end suite `Label("e2e")`. Each spec uses its own accounts and cleans up
  after itself.

### Layout

| Where | What |
|---|---|
| `main.go` | manifest, `route`, `register`, `registerShare` |
| `context.go` | `reqCtx` and its parameter, answer and error helpers |
| `auth.go` | `account`, permissions, `authenticate`, `storePassword` |
| `cards.go` | the `credential` and `settings` cards |
| `db.go` | `query`, `queryRow`, `exec`, `tx`, `queryIn`, `row`, time helpers |
| `schema.go` | every table of the extension |
| `types.go` | every response type of the old server, and `body`/`response` |
| `basics.go` | `ping`, `getLicense`, `getMusicFolders` |
| `envelope/`, `ids/`, `creds/` | pure packages (no SDK), with their specs |
| `e2e/` | the end-to-end suite and its harness |

A domain adds its own files (`browse.go`, `lists.go`, ...), its own `e2e/<domain>_test.go`, and pure packages at the root (`mosaic/`, `lrc/`, ...). A helper local to your files is prefixed with your domain (`browseTrack`, not `track`), because every domain shares one package.


### Adding an endpoint

```go
func init() {
	register("getThing", getThing) // /rest/getThing and /rest/getThing.view
}

func getThing(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindAlbum)   // error 10 when absent, 70 when malformed or the wrong kind
	if err != nil {
		return c.failErr(err)
	}
	...
	return c.respond(&body{...})                // decorators first, then the envelope
}
```

`type handler func(c *reqCtx) sdk.Response`. A handler returns an answer, never a Go error: an `*envelope.Error` (`envelope.NotFound()`, `envelope.NotAuthorized()`, `envelope.MissingParam(name)`, or `&envelope.Error{Code, Message}`) goes through `c.failErr`, which also turns any other error into a logged `Internal error`.

#### `reqCtx`

| | |
|---|---|
| `c.user` | the authenticated `*account`: `ID`, `Email`, `Role`, `DisplayName`, `CanStream`, `CanDownload`, `CanManagePlaylists`, `CanShare`, `CanRate`, `CanChangePassword`, `MaxBitRate` (kbps, 0 none); `c.user.isAdmin()` |
| `c.req` | the `sdk.Request` (folders: `c.req.Folder("music")`, `c.req.GuestPath(hostPath)`) |
| `c.param(name)`, `c.multi(name)` | first / all values; the query string and a form body are merged (body first, as `formPost` clients expect) |
| `c.intParam(name, def, lo, hi)`, `c.boolParam(name, def)`, `c.millisParam(name)` | typed, clamped, never an error |
| `c.anyIDParam(name)`, `c.idParam(name, kind)` | `ids` parsing with the right Subsonic error |
| `c.require(permStream\|permDownload\|permPlaylists\|permShare\|permRate\|permChangePassword)` | `nil`, or error 50 |
| `c.ok(b)` | answer without decorating; `nil` body is the empty response |
| `c.respond(b)` | run the registered decorators (star, rating, play count; the marks domain registers one with `registerDecorator` from its `init`), then `ok` |
| `c.fail(code, msg)`, `c.failErr(err)` | a failed answer (HTTP 200, as Subsonic does) |

A handler that streams returns the `sdk.Response` with `SendFile` or `StreamFFmpeg` directly (see the host docs); a refusal on that path is still `c.failErr(...)`.

`/share`: the share domain calls `registerShare(func(r sdk.Request) sdk.Response)` from its `init`. Streamline serves the extension at `/extensions/subsonic` (`Request.BasePath`), its two mounts below it, and gives a guest the path **relative to the mount**, so `/extensions/subsonic/rest/ping` and `/extensions/subsonic/share/<token>` arrive as `/ping` and `/<token>`; `route` in `main.go` dispatches on `Request.Mount` (`/rest` or `/share`, empty on the `/api` and `/cards` subtrees). Every link the extension builds starts with `Request.BasePath`. Anything under `/share` that is not a page, stream or cover of a well-formed token (20 or more characters of `[A-Za-z0-9_-]`, which the old server's 22-character base64 tokens are) is a plain 404. `Request.Origin` is the scheme and host the client used, worked out by the host behind its trusted proxies: build every absolute URL from it, never from `X-Forwarded-*` headers.

#### Database

All SQL goes through these, parameterised, never built from request text.

```go
rows, err := query("SELECT id, title FROM streamline_albums WHERE artist_id = ?", id)
for _, r := range rows { r.uint(0); r.str(1) }        // typed accessors, NULL is the zero value
r, found, err := queryRow("SELECT ...", ...)
res, err := exec("INSERT INTO playlists(...) VALUES(...)", ...)   // res.LastInsertID, res.RowsAffected
err = tx(func() error { ... })                          // BEGIN IMMEDIATE / COMMIT, ROLLBACK on error
rows, err = queryIn("SELECT ... WHERE id IN ({in})", ids, lead...)  // chunks of 500; {in} marks the placeholders
```

`row` has `str`, `int`, `uint`, `bool`, `null`, `time`. Numbers from the host are float64, so ids come back through `r.uint`.

Time: this extension's tables store `storedTime` text (`nowStored()`, `formatStored(t)`); core views hold Go-printed times. `parseTime(s)` reads both; `timestamp(t)` / `optTimestamp(t)` render a response's RFC 3339 (empty for the zero time).

The host's authorizer refuses `PRAGMA`; `CREATE INDEX` is allowed. `ensureSchema` versions the schema by an empty marker table (`schema_v<N>`), and secondary indexes are ordinary `CREATE INDEX IF NOT EXISTS` statements in `schema.go`. Foreign keys are not enforced and none are declared: delete child rows in the same `tx` as their parent.

#### Core views (read-only, `streamline_<name>`)

`artists`, `artist_members`, `albums`, `album_artists`, `tracks`, `music_credits`, `music_files`, `genres`, `users`, `user_logins`. Columns are in `internal/db/extension_views.go` of Streamline; `music_files.path` is the absolute server path (open it with `c.req.GuestPath`, or hand it to `SendFile`/ffmpeg), `music_files.missing` is 1 for a file gone from disk, `duration` is seconds, `music_files.added_at` is when the file joined the library (a song's `created`). Core times are Go-printed in whatever zone they were written: parse them with `parseTime`, and compare or order them in SQL through `coreMillis(col)`, never as text. `streamline_users.locked` carries an account's lockout. A core id (artist, album, track) is a `uint32`; Subsonic ids are `ids.Artist(id)`, `ids.Album(id)`, `ids.Track(id)`, parsed with `ids.Parse`.

### The extension's tables (`schema.go`)

| Table | Columns | Owner |
|---|---|---|
| `accounts` | `user_id` PK, `password`, `password_created_at`, `last_used_at`, `last_client`, `can_stream`, `can_download`, `can_manage_playlists`, `can_share`, `can_rate`, `can_change_password` (all default 1), `max_bit_rate` 0..320 | auth, cards, users |
| `annotations` | `user_id`, `target_kind` (`ar`/`al`/`tr`), `target_id`, `starred_at`, `rating` 0..5, `play_count`, `played_at`, `updated_at` (every write); PK `(user_id, target_kind, target_id)` | marks |
| `playlists` | `id`, `owner_id`, `name`, `comment`, `public`, `created_at`, `changed_at` | playlists |
| `playlist_tracks` | `playlist_id`, `position`, `track_id`; PK `(playlist_id, position)` | playlists |
| `play_queues` | `user_id` PK, `current_index`, `position_ms`, `changed_by`, `changed_at` | queue |
| `play_queue_entries` | `user_id`, `position`, `track_id`; PK `(user_id, position)` | queue |
| `bookmarks` | `user_id`, `track_id`, `position_ms`, `comment`, `created_at`, `changed_at`; PK `(user_id, track_id)` | queue |
| `shares` | `id`, `token` unique, `owner_id`, `description`, `expires_at`, `last_visited_at`, `visit_count`, `created_at` | shares |
| `share_entries` | `share_id`, `position`, `item` (a Subsonic id such as `tr-12`, `al-3`, `pl-1`); PK `(share_id, position)` | shares |
| `radio_stations` | `id`, `name`, `stream_url`, `homepage_url`, `created_at` | queue |
| `now_playing` | `user_id`, `player`, `track_id`, `started_at`, `expires_at`; PK `(user_id, player)` | marks |

An account with no `accounts` row has every permission on and no password. Everything a request leaves behind is a row: a guest keeps nothing in memory.

A new column or table goes in `schema.go`, with `schemaVersion` bumped so `ensureSchema` applies it.

### Sign-in

`u` is the account's email (matched without case), with `p` (plain or `enc:` hex) or `t`+`s` (`md5(password+salt)`); a missing `p`, or half a token, is error 10. An unknown login, an account with no Subsonic password and a wrong password all answer error 40 with one message. An `apiKey` answers error 42 (this server offers none), and two mechanisms at once (`apiKey` with any of `u`, `p`, `t`, `s`, or `p` with `t`/`s`) error 43, before any login is looked up. `c.user` is set before any handler runs. `getOpenSubsonicExtensions` answers without credentials.

### e2e harness (`e2e/`, package `e2e`, Ginkgo)

`app` (an `*instance`) is booted once per process in `BeforeSuite`: the binary named by `STREAMLINE_BIN`, a scratch directory under `/tmp/streamline/subsonic-e2e/` and a free port it finds itself, `subsonic.wasm` approved through the API so its grants are exactly its manifest, real `ffmpeg` found on `PATH`. Put `Label("e2e")` on every `Describe`.

| Helper | |
|---|---|
| `app.newUser(prefix)`, `app.newUserWithRole(prefix, role)` | a Streamline account (member / any role) with no Subsonic password |
| `app.subsonicUser(prefix)` | `newUser` plus a Subsonic password, the usual start of a spec |
| `app.setSubsonicPassword(u, pw)` | writes `accounts` directly, no web session |
| `app.setSettings(u, map)` | permissions through the settings card (`can_stream`, ..., `max_bit_rate`) |
| `u.client()` | a signed-in `go-subsonic` client (token scheme) |
| `u.get(endpoint, extra)` | signed raw GET, JSON unless `extra` says `f`; returns `rawResponse{Status, Header, Body}`, `.json()` |
| `u.postForm(endpoint, extra)` | the same as a form body |
| `u.call(endpoint, extra)`, `u.callError(endpoint, extra)` | the `subsonic-response` object, asserting `ok` / `failed` (and returning the error object; `errorCode(e)`) |
| `rest(endpoint, q)`, `fetch(method, path, body, header)` | unsigned requests, e.g. a `/share/<token>` |
| `obj(m, key)`, `list(m, key)`, `ids(items)` | read decoded JSON |
| `app.sharedLibrary()` | the read-only seeded library (below), seeded once per instance |
| `app.seedLibrary()`, `app.addArtist(artistSpec)` | a library of your own, for specs that change it |
| `app.core` | the core database (`*sql.DB`), `app.ext()` the extension's (close it) |
| `app.api(method, path, body, status)`, `apiList`, `apiRaw`, `apiAs(token, ...)` | the Streamline API as the seed admin / a session |
| `app.login(email, password)` | a session JWT; **the instance allows 5 logins in 15 minutes and boot and the credential card spec spend two**: use the admin API and the helpers above instead |
| `app.MusicDir`, `app.ArtworkDir`, `app.CacheDir`, `app.BaseURL` | where things are |

`seedLibrary()` makes, with real audio from ffmpeg under `MusicDir` and a poster under `ArtworkDir/albums/<id>/poster.jpg`:

```
The Seeders (Rock)
  First Light (2019, album, Rock/Indie, cover): Dawn, Noon, Dusk       flac
  Second Wind (2021, EP):                       Gust (flac), Calm (no file)
Ana Lyra (Jazz)
  Blue Hour (2015, album, Jazz, no cover):      Indigo, Slate          mp3
```

`lib.track(artist, album, track)` returns a `seededTrack{ID, Title, Position, Path, FileID, Format}`; `lib.Artists[i].Albums[j].ID` is the album id. `addArtist` takes an `artistSpec` of `albumSpec`s of `trackSpec`s (title, disc, position, format `flac|mp3|opus`, `NoFile`, `Seconds`, `Credit`).

Every spec uses its own accounts (`app.unique(prefix)` makes names no other spec uses) and cleans up after itself; the instance is shared by all specs of the process.

#### Worked example

Handler (illustrative; `getMusicFolders` in `basics.go` is the one in the tree):

```go
func init() { register("getGenres", getGenres) }

func getGenres(c *reqCtx) sdk.Response {
	rows, err := query(`SELECT g.name, g.album_count,
			(SELECT count(*) FROM streamline_tracks t
			 JOIN streamline_albums a ON a.id = t.album_id
			 WHERE a.genres LIKE '%"' || g.name || '"%')
		FROM streamline_genres g ORDER BY g.name`)
	if err != nil {
		return c.failErr(err)
	}
	out := &genres{Genre: make([]genre, 0, len(rows))}
	for _, r := range rows {
		out.Genre = append(out.Genre, genre{
			Value: r.str(0), AlbumCount: int(r.int(1)), SongCount: int(r.int(2)),
		})
	}
	return c.ok(&body{Genres: out})
}
```

Its spec (`e2e/browse_test.go`):

```go
var _ = Describe("getGenres", Label("e2e"), func() {
	It("lists each genre with its albums", func() {
		app.sharedLibrary()
		u := app.subsonicUser("genres")
		got := list(obj(u.call("getGenres", nil), "genres"), "genre")
		Expect(got).To(ContainElement(And(
			HaveKeyWithValue("value", "Rock"),
			HaveKeyWithValue("albumCount", BeNumerically(">=", 2)),
		)))
	})

	It("refuses a caller with the wrong password", func() {
		u := app.subsonicUser("genres")
		u.Password = "nope"
		Expect(errorCode(u.callError("getGenres", nil))).To(Equal(40))
	})
})
```

Cover, for every endpoint: permission refusals (error 50, `app.setSettings(u, ...)` first), one user never reading or changing another's rows, ids that are malformed, of the wrong kind or of a row that is gone (error 70).

### Parity notes

- The old server had no JSONP. This one answers `f=jsonp` wrapped in `callback(...)`; a `callback` that is missing or not a JavaScript identifier (or dotted path) is error 10, as XML.
- Binary endpoints (`stream`, `download`, `getCoverArt`, `getAvatar`) register with `registerBinary`: their errors are an XML document whatever `f` says, as the API defines.
- Every call but `getOpenSubsonicExtensions` needs `v` and `c` (error 10); a `v` above 1.16 or of another major version is error 30, one below major 1 error 20.
- Share tokens: see the `/share` paragraph above.
- Not available to a guest: `PRAGMA`.

## AI-assisted contributions

Streamline is built with heavy AI assistance, so this is not a "no AI" project,
and you don't need to hide that you used a coding agent. There is one rule:

**You are the author. The model is a tool.**

In practice:

- **Understand every line you submit.** If a reviewer asks why a function is
  shaped that way and the honest answer is "the model wrote it", the PR isn't
  ready. Read the diff before you open it.
- **Run `task lint` and the tests yourself.** An agent claiming the tests pass
  is not evidence that they pass. Paste real output if a change is subtle.
- **Verify the code against reality, not plausibility.** Agents invent SDK
  functions, view columns and API fields that look exactly right and don't
  exist. Build it.
- **Mention it in the PR description.** One line, "written with Claude Code",
  so a reviewer knows which failure modes to look for. No stigma attached.

What gets closed without much discussion:

- Large unreviewed diffs the submitter can't explain, or PRs that clearly
  weren't run
- Agent-generated refactors, "modernisations", or comment/docstring sweeps nobody
  asked for
- Scanner or agent output filed as a bug report or security report with no
  reproduction: see [SECURITY.md](SECURITY.md) for what a real report looks like

Reviewer time is the scarce resource here. A small PR you fully understand is
worth more than a large one you don't.

## Commits and PRs

Conventional Commits, subject line only unless the body earns its place:

```
feat(media): report a lossless song's bit depth and sampling rate
fix: date songs and the newest list by the file's added_at
```

`type(scope): msg`, scope optional. Keep the PR to one topic. Rebase on `main`
rather than merging it in. Fill in the PR template checklist.

## License

Contributions are licensed under [GPL-3.0-or-later](LICENSE), same as the
project.

# streamline-subsonic

A [Subsonic](https://www.subsonic.org/pages/api.jsp) 1.16.1 and [OpenSubsonic](https://opensubsonic.netlify.app/) server for [Streamline](https://github.com/DataHearth/streamline), shipped as a Streamline extension: a WebAssembly module (`subsonic.wasm`) the server loads and runs in a sandbox.

It lets any Subsonic app (Symfonium, DSub, Ultrasonic, Tempo, play:Sub, Feishin, Sublime Music, …) browse, search, stream and download your Streamline library.

- **API** at `/extensions/subsonic/rest`: point apps at `https://your-host/extensions/subsonic`.
- **Public share pages** at `/extensions/subsonic/share/<token>`.
- **Library access is read-only**, through the views Streamline grants the extension. Everything the extension owns (Subsonic passwords, per-account permissions, stars, ratings, play counts, playlists, play queues, bookmarks, shares, internet radio stations) lives in its own database.

## Features

| Area | Support |
| --- | --- |
| Browsing | Artist index (ignoring a leading *The, El, La, Los, Las, Le, Les*), tag and folder views, genres, artist biography, similar artists and songs, top songs |
| Lists | Album lists (newest, random, alphabetical, by year, by genre, starred, highest rated, most played, recently played), random songs, songs by genre, now playing |
| Search | `search`, `search2`, `search3`, including the empty query apps use to sync a library |
| Stars, ratings, plays | Per-user stars, 1–5 ratings and scrobbling |
| Playlists | Create, edit, reorder, make public, with generated mosaic covers |
| Play queue | Saved per device and resumed elsewhere, by song or by index |
| Bookmarks | Positions in long tracks and mixes |
| Sharing | Public links to songs, albums, artists or playlists, with expiry and a web player page |
| Streaming | Original file with byte-range seeking, or on-the-fly MP3 / Opus / AAC transcoding through Streamline's ffmpeg service, with per-account bit-rate caps |
| Covers | Album, artist, song and playlist covers, scaled to the requested size and cached |
| Lyrics | Sidecar `.lrc` files (timed, enhanced LRC) and embedded lyrics tags, with their language |
| Internet radio | A shared station list, managed by admins |
| Admin | Library rescan and scan status, user listing, own password change |

OpenSubsonic extensions: `formPost`, `songLyrics`, `transcodeOffset`, `indexBasedQueue`; JSON, XML and JSONP responses.

Not supported: podcasts, chat, jukebox mode, video and avatars.

The full user guide is [docs/wiki/Subsonic.md](docs/wiki/Subsonic.md).

## Install

Requires a Streamline release that serves extensions at `/extensions/<name>` (extension contract version 2). Version 1.x of this extension targets the release before it, which mounted it at `/rest` and `/share`.

1. Download `subsonic.wasm` and `subsonic.wasm.sha256` from the [latest release](../../releases/latest).
2. In Streamline, open **Settings → Extensions → Add**, give the URL of `subsonic.wasm` and its SHA-256, then review the views, folders and services it asks for. See the [Extensions](https://github.com/DataHearth/streamline/wiki/Extensions) wiki page.
3. Each user generates a **Subsonic password** from the **Subsonic** card on their account page. It is separate from the web login password and never works for the web UI.
4. In the app, use `https://your-host/extensions/subsonic` as the server, the account's **email** as the user name and the Subsonic password. In apps that offer several server types, pick Subsonic or OpenSubsonic.

Admins set what an account may do from an app (stream, download, manage playlists, share, rate, change its own password, maximum bit rate) on the account's page, under **Subsonic apps**.

> Nothing is carried over from the Subsonic server that used to be built into Streamline: every account starts without a Subsonic password, and stars, playlists and the rest start empty.

### Permissions it requests

| Kind | Granted |
| --- | --- |
| Views | `artists`, `artist_members`, `albums`, `album_artists`, `tracks`, `music_credits`, `music_files`, `genres`, `users`, `user_logins` |
| Folders | `music`, `artwork`, `cache` |
| Services | `ffmpeg` (transcoding, cover scaling), `library_scan` (rescan from an app) |

## Development

Requires Go (version in `go.mod`) and [Task](https://taskfile.dev).

```sh
task build                                 # subsonic.wasm (GOOS=wasip1 GOARCH=wasm)
task lint                                  # golangci-lint on the guest and e2e/
task fmt
task test:unit                             # host-free specs, natively
STREAMLINE_BIN=/path/to/streamline task test:e2e   # end-to-end against a real Streamline binary
```

The extension SDK is the `github.com/datahearth/streamline/extensions/sdk` module from the Streamline repository. It has no tagged release yet, so `go.mod` pins it to a commit; move it with `go get github.com/datahearth/streamline/extensions/sdk@<commit>`. To build against a local Streamline checkout, add it to your own `go.work` with `go work use ../streamline/extensions/sdk`, and keep that out of commits.

Releases are built by CI: pushing a `v*` tag publishes `subsonic.wasm` and its checksum.

Code layout, conventions and how to add an endpoint: [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[GPL-3.0](LICENSE).

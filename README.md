# streamline-subsonic

Subsonic 1.16.1 / OpenSubsonic server for [Streamline](https://github.com/DataHearth/streamline), as an extension: a WebAssembly module the server loads, mounted at `/rest`, with public share pages at `/share`. It reads the library through the views Streamline grants it and keeps everything else (passwords, permissions, stars, ratings, playlists, play queues, bookmarks, shares, radio stations) in its own database.

## Install

In Streamline, Settings → Extensions → Add: give it the URL of `subsonic.wasm` from a release of this repository and its SHA-256 (`subsonic.wasm.sha256`), then review what it asks for. See the [Extensions](https://github.com/DataHearth/streamline/wiki/Extensions) wiki page.

Each account then generates its Subsonic password on its account page. An admin sets what an account may do from an app (stream, download, playlists, shares, rating, changing its own password, a maximum bit rate) on the account's page, under **Subsonic apps**. The user documentation is [docs/wiki/Subsonic.md](docs/wiki/Subsonic.md).

Nothing is carried over from the Subsonic server that was built into Streamline: every account starts without a password, and stars, playlists and the rest start empty.

## Build

```sh
task build        # subsonic.wasm
task lint
task test:unit
STREAMLINE_BIN=/path/to/streamline task test:e2e
```

Contributors: see [CONTRIBUTING.md](CONTRIBUTING.md).

The extension SDK lives in the Streamline repository (`extensions/sdk`) and has no published version yet, so `go.mod` replaces it with a checkout of Streamline beside this one (`../streamline`).

## License

GPL-3.0, see [LICENSE](LICENSE).

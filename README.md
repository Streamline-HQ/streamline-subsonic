# streamline-subsonic

The Subsonic / OpenSubsonic server that used to be built into [Streamline](https://github.com/DataHearth/streamline), kept here as it was when it left core (Streamline commit `403daa9ad7c1`).

**This does not build on its own.** The code imports Streamline's `ent` and `internal/...` packages, which another module cannot import. It is the starting point for a standalone project, not a release.

## What is here

Paths are the ones the files had in Streamline.

- `internal/server/subsonic/` — the `/rest` API and the public `/share/{token}` pages
- `internal/appaccess/` — per-user app password storage
- `internal/ffmpeg/audio.go` — on-the-fly audio transcoding
- `web/app/components/users/UserSubsonicCard.svelte` — the admin's per-user card
- `docs/wiki/Subsonic.md` — the user documentation

Streamline still carries the data this server kept (`User.subsonic_*`, playlists, play queues, bookmarks, shares, radio stations, stars and ratings), untouched and unread, so it can be carried over.

## License

GPL-3.0, see [LICENSE](LICENSE).

# Security Policy

## Supported versions

| Version              | Supported          |
| -------------------- | ------------------ |
| Latest release       | :white_check_mark: |
| Anything older       | :x:                |

streamline-subsonic is maintained by one person. Fixes land in a new patch
release on top of the latest version; older releases are not backported.
Upgrade before reporting.

## Reporting a vulnerability

**Do not open a public issue.**

Use GitHub's private vulnerability reporting:
[Report a vulnerability](https://github.com/Streamline-HQ/streamline-subsonic-ext/security/advisories/new).
If that is unavailable to you, email dev@antoine-langlois.net instead.

Please include:

- The extension version and the Streamline version it runs on
- What an attacker can do: act as another account, get past a permission,
  reach a file outside the music library, run ffmpeg with arguments of their
  choosing, etc.
- Steps to reproduce, ideally with the app and the request it sends
  (passwords and tokens redacted)

Expect an acknowledgement within a week and a fix or a decision within 30 days
for anything exploitable. This is a spare-time project, not a vendor with an
on-call rotation: if something is actively being exploited, say so up front and
I will prioritise it. I will credit you in the advisory and the release notes
unless you'd rather stay anonymous.

Please give me a chance to ship a fix before disclosing publicly. There is no
bug bounty.

## Scope

In scope: anything that breaks the security model of the extension on a
correctly configured Streamline:

- Authentication bypass on `/extensions/subsonic/rest`, or a locked account or
  one without a Subsonic password getting in
- Getting past an account's permissions (stream, download, playlists, shares,
  rating, password change, maximum bit rate)
- One account reading or changing another's playlists, play queue, bookmarks,
  stars, ratings or shares
- A public share page serving more than was shared, or after it expired
- **ffmpeg argument injection.** The extension holds Streamline's `ffmpeg`
  permission, which is full trust: anything a request can slip into an ffmpeg
  command line runs with Streamline's own rights
- A stream, download or cover reaching a file outside the music library
- SQL injection through any parameter
- Supply-chain issues in the release pipeline: the published `subsonic.wasm`
  not matching its checksum or this repository's source

Out of scope:

- Vulnerabilities in Streamline itself (the extension host, its sandbox, its
  views): report those to
  [Streamline](https://github.com/DataHearth/streamline/security/advisories/new).
- Weaknesses of the Subsonic protocol itself: the `p` parameter, `enc:` hex and
  `md5(password + salt)` tokens are what the apps speak. That is why the
  Subsonic password is separate from the login password.
- The API over plain HTTP. Streamline is built to sit behind TLS.
- Vulnerabilities in Subsonic apps. Report those upstream.
- Scanner or agent output with no demonstrated path to exploitation.

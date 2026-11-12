package main

import "github.com/datahearth/streamline/extensions/sdk"

func init() {
	register("ping", ping)
	register("getLicense", getLicense)
	register("getMusicFolders", getMusicFolders)
}

func ping(c *reqCtx) sdk.Response { return c.ok(nil) }

func getLicense(c *reqCtx) sdk.Response {
	return c.ok(&body{License: &license{Valid: true}})
}

// getMusicFolders names the library as the one folder, id 1; a musicFolderId
// naming anything else is the browse endpoints' to refuse.
func getMusicFolders(c *reqCtx) sdk.Response {
	return c.ok(&body{MusicFolders: &musicFolders{
		MusicFolder: []musicFolder{{ID: 1, Name: "Music"}},
	}})
}

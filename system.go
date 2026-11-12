package main

import (
	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
)

func init() {
	register("getScanStatus", getScanStatus)
	register("startScan", startScan)
}

func getScanStatus(c *reqCtx) sdk.Response {
	info, err := sdk.ScanStatus()
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(&body{ScanStatus: &scanStatus{
		Scanning: info.Scanning,
		Count:    int(info.Count),
	}})
}

// startScan runs Streamline's library scan, which re-stats every tracked file
// and marks the missing ones: what a Subsonic client means by a rescan. New
// music arrives through imports, never a folder walk.
func startScan(c *reqCtx) sdk.Response {
	if !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	if err := sdk.StartScan(); err != nil {
		return c.failErr(err)
	}
	return getScanStatus(c)
}

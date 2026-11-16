package main

import (
	"net/url"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

func init() {
	register("getInternetRadioStations", getInternetRadioStations)
	register("createInternetRadioStation", createInternetRadioStation)
	register("updateInternetRadioStation", updateInternetRadioStation)
	register("deleteInternetRadioStation", deleteInternetRadioStation)
}

// radioWebURL accepts an absolute http(s) URL. Streamline never fetches a
// station: this only keeps a client from being handed a javascript: or file:
// link.
func radioWebURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return raw, true
}

func radioBadURL(name string) error {
	return &envelope.Error{
		Code:    envelope.CodeGeneric,
		Message: name + " must be an http or https URL",
	}
}

func getInternetRadioStations(c *reqCtx) sdk.Response {
	rows, err := query(`SELECT id, name, stream_url, homepage_url
		FROM radio_stations ORDER BY name, id`)
	if err != nil {
		return c.failErr(err)
	}
	out := &radioStations{Station: make([]radioStation, 0, len(rows))}
	for _, r := range rows {
		out.Station = append(out.Station, radioStation{
			ID:          ids.Radio(r.uint(0)),
			Name:        r.str(1),
			StreamURL:   r.str(2),
			HomePageURL: r.str(3),
		})
	}
	return c.ok(&body{InternetRadioStations: out})
}

// radioFields are a station as create and update take it: name and streamUrl
// required, homepageUrl optional. An update replaces the whole station, as the
// spec has it take every field, so a homepageUrl it leaves out is cleared.
type radioFields struct {
	name, stream, homepage string
}

func radioRead(c *reqCtx) (radioFields, error) {
	var f radioFields
	f.name = strings.TrimSpace(c.param("name"))
	if f.name == "" {
		return f, envelope.MissingParam("name")
	}
	if c.param("streamUrl") == "" {
		return f, envelope.MissingParam("streamUrl")
	}
	s, ok := radioWebURL(c.param("streamUrl"))
	if !ok {
		return f, radioBadURL("streamUrl")
	}
	f.stream = s
	if raw := strings.TrimSpace(c.param("homepageUrl")); raw != "" {
		hp, ok := radioWebURL(raw)
		if !ok {
			return f, radioBadURL("homepageUrl")
		}
		f.homepage = hp
	}
	return f, nil
}

// The station list is shared by every user, so changing it is an admin's.
func createInternetRadioStation(c *reqCtx) sdk.Response {
	if !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	f, err := radioRead(c)
	if err != nil {
		return c.failErr(err)
	}
	if _, err := exec(`INSERT INTO radio_stations(name, stream_url, homepage_url,
		created_at) VALUES(?, ?, ?, ?)`, f.name, f.stream, f.homepage, nowStored(),
	); err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

func updateInternetRadioStation(c *reqCtx) sdk.Response {
	if !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	id, err := c.idParam("id", ids.KindRadio)
	if err != nil {
		return c.failErr(err)
	}
	f, err := radioRead(c)
	if err != nil {
		return c.failErr(err)
	}
	if _, found, err := queryRow(
		"SELECT 1 FROM radio_stations WHERE id = ?", id,
	); err != nil {
		return c.failErr(err)
	} else if !found {
		return c.failErr(envelope.NotFound())
	}
	if _, err := exec(`UPDATE radio_stations SET name = ?, stream_url = ?,
		homepage_url = ? WHERE id = ?`, f.name, f.stream, f.homepage, id,
	); err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

func deleteInternetRadioStation(c *reqCtx) sdk.Response {
	if !c.user.isAdmin() {
		return c.failErr(envelope.NotAuthorized())
	}
	id, err := c.idParam("id", ids.KindRadio)
	if err != nil {
		return c.failErr(err)
	}
	res, err := exec("DELETE FROM radio_stations WHERE id = ?", id)
	if err != nil {
		return c.failErr(err)
	}
	if res.RowsAffected == 0 {
		return c.failErr(envelope.NotFound())
	}
	return c.ok(nil)
}

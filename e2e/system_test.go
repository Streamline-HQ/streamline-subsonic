package e2e

import (
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("endpoint names", Label("e2e"), func() {
	It("answers a long unknown endpoint as an authenticated error 0", func() {
		u := app.subsonicUser("longname")
		for _, name := range []string{
			"createPodcastChannel", "deletePodcastChannel", "getTranscodeDecision",
		} {
			raw := u.get(name, nil)
			Expect(raw.Status).To(Equal(http.StatusOK), name)
			resp := raw.json()
			Expect(resp["status"]).To(Equal("failed"), name)
			Expect(errorCode(obj(resp, "error"))).To(Equal(0), name)
		}
	})

	It("refuses a locked Streamline account with error 40", func() {
		u := app.subsonicUser("locked")
		u.call("ping", nil)
		_, err := app.core.Exec(
			"UPDATE users SET locked_until = ? WHERE id = ?",
			"2099-01-01 00:00:00+00:00", u.ID,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_, err := app.core.Exec(
				"UPDATE users SET locked_until = NULL WHERE id = ?", u.ID,
			)
			Expect(err).NotTo(HaveOccurred())
		})
		wantCode(u.get("ping", nil), 40)
	})
})

var _ = Describe("the extension", Label("e2e"), func() {
	It("runs with exactly what its manifest asks for", func() {
		ext := app.extension()
		Expect(ext["state"]).To(Equal("running"))
		Expect(ext["granted"]).To(Equal(ext["requested"]))
		requested, ok := ext["requested"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(requested["mounts"]).To(ConsistOf(
			And(HaveKeyWithValue("path", "/rest"), HaveKeyWithValue("auth", "self")),
			And(
				HaveKeyWithValue("path", "/share"),
				HaveKeyWithValue("auth", "self"),
			),
		))
		Expect(requested["folders"]).To(ConsistOf("music", "artwork", "cache"))
		Expect(requested["services"]).To(ConsistOf("ffmpeg", "library_scan"))
		views := requested["views"].([]any)
		names := make([]any, 0, len(views))
		for _, v := range views {
			names = append(names, v.(map[string]any)["name"])
		}
		Expect(names).To(ConsistOf(
			"artists", "artist_members", "albums", "album_artists", "tracks",
			"music_credits", "music_files", "genres", "users", "user_logins",
		))
	})
})

var _ = Describe("system endpoints", Label("e2e"), func() {
	var u *testUser

	BeforeEach(func() {
		u = app.subsonicUser("system")
	})

	It("answers ping in XML by default", func() {
		raw := u.get("ping", url.Values{"f": {""}})
		Expect(raw.Status).To(Equal(http.StatusOK))
		Expect(raw.Header.Get("Content-Type")).To(Equal("text/xml; charset=utf-8"))
		var doc struct {
			XMLName       xml.Name `xml:"subsonic-response"`
			Xmlns         string   `xml:"xmlns,attr"`
			Status        string   `xml:"status,attr"`
			Version       string   `xml:"version,attr"`
			Type          string   `xml:"type,attr"`
			OpenSubsonic  bool     `xml:"openSubsonic,attr"`
			ServerVersion string   `xml:"serverVersion,attr"`
		}
		Expect(xml.Unmarshal(raw.Body, &doc)).To(Succeed(), string(raw.Body))
		Expect(doc.Xmlns).To(Equal("http://subsonic.org/restapi"))
		Expect(doc.Status).To(Equal("ok"))
		Expect(doc.Version).To(Equal("1.16.1"))
		Expect(doc.Type).To(Equal("streamline"))
		Expect(doc.OpenSubsonic).To(BeTrue())
		Expect(doc.ServerVersion).NotTo(BeEmpty())
	})

	It("answers ping in JSON with f=json", func() {
		raw := u.get("ping", nil)
		Expect(
			raw.Header.Get("Content-Type"),
		).To(Equal("application/json; charset=utf-8"))
		resp := raw.json()
		Expect(resp["status"]).To(Equal("ok"))
		Expect(resp["version"]).To(Equal("1.16.1"))
		Expect(resp["openSubsonic"]).To(BeTrue())
	})

	It("routes an endpoint with and without .view", func() {
		Expect(u.call("ping.view", nil)["status"]).To(Equal("ok"))
		Expect(u.call("getLicense.view", nil)["status"]).To(Equal("ok"))
	})

	It("reports a valid license", func() {
		Expect(obj(u.call("getLicense", nil), "license")["valid"]).To(BeTrue())
	})

	It("lists the library as the one music folder", func() {
		folders := list(
			obj(u.call("getMusicFolders", nil), "musicFolders"),
			"musicFolder",
		)
		Expect(folders).To(HaveLen(1))
		Expect(folders[0]["id"]).To(BeEquivalentTo(1))
		Expect(folders[0]["name"]).To(Equal("Music"))
	})

	It("advertises the OpenSubsonic extensions without credentials", func() {
		raw := rest("getOpenSubsonicExtensions", url.Values{"f": {"json"}})
		resp := raw.json()
		Expect(resp["status"]).To(Equal("ok"))
		advertised := list(resp, "openSubsonicExtensions")
		names := make([]string, 0, len(advertised))
		for _, e := range advertised {
			names = append(names, e["name"].(string))
		}
		Expect(names).To(ConsistOf(
			"formPost", "songLyrics", "transcodeOffset", "indexBasedQueue",
		))
	})

	It("takes the parameters from a form body", func() {
		raw := u.postForm("ping", nil)
		Expect(raw.json()["status"]).To(Equal("ok"))
	})

	It("answers an endpoint it does not have with error 0", func() {
		errObj := u.callError("noSuchEndpoint", nil)
		Expect(errorCode(errObj)).To(Equal(0))
		Expect(errObj["message"]).To(Equal("Not implemented"))
	})

	It("answers an unknown endpoint of any length with error 0 under /rest", func() {
		for _, name := range []string{"x", "AbCdEfGhIjKlMnOpQrStUvWxYz0123"} {
			errObj := u.callError(name, nil)
			Expect(errorCode(errObj)).To(Equal(0), name)
			Expect(errObj["message"]).To(Equal("Not implemented"), name)
		}
	})

	It("answers a malformed query with error 0", func() {
		raw := fetch(http.MethodGet, "/rest/ping?u=%zz&f=json", nil, nil)
		Expect(raw.Status).To(Equal(http.StatusOK))
		Expect(string(raw.Body)).To(ContainSubstring(
			`<error code="0" message="Malformed request">`,
		))
	})

	It("keeps /share separate from /rest", func() {
		raw := fetch(http.MethodGet, "/share/AbCdEfGhIjKlMnOpQrStUv", nil, nil)
		Expect(raw.Status).To(Equal(http.StatusNotFound))
	})

	It("answers a junk short path under /share with a plain 404", func() {
		for _, path := range []string{"/share/x", "/share/ping", "/share/a/b/c/d"} {
			raw := fetch(http.MethodGet, path, nil, nil)
			Expect(raw.Status).To(Equal(http.StatusNotFound), path)
			Expect(
				string(raw.Body),
			).NotTo(ContainSubstring("subsonic-response"), path)
		}
	})
})

var _ = Describe("authentication", Label("e2e"), func() {
	var u *testUser

	BeforeEach(func() {
		u = app.subsonicUser("auth")
	})

	wrongCredentials := func(raw rawResponse) {
		GinkgoHelper()
		Expect(raw.Status).To(Equal(http.StatusOK))
		resp := raw.json()
		Expect(resp["status"]).To(Equal("failed"))
		errObj := resp["error"].(map[string]any)
		Expect(errorCode(errObj)).To(Equal(40))
		Expect(errObj["message"]).To(Equal("Wrong username or password"))
	}

	It("accepts the token scheme through a client library", func() {
		Expect(u.client().Ping()).To(BeTrue())
	})

	It("accepts a plain password", func() {
		raw := rest(
			"ping",
			url.Values{"u": {u.Email}, "p": {u.Password}, "f": {"json"}},
		)
		Expect(raw.json()["status"]).To(Equal("ok"))
	})

	It("accepts a hex-encoded password", func() {
		raw := rest("ping", url.Values{
			"u": {u.Email},
			"p": {"enc:" + hex.EncodeToString([]byte(u.Password))},
			"f": {"json"},
		})
		Expect(raw.json()["status"]).To(Equal("ok"))
	})

	It("matches the user name without regard to case", func() {
		raw := u.get("ping", url.Values{"u": {strings.ToUpper(u.Email)}})
		Expect(raw.json()["status"]).To(Equal("ok"))
	})

	It("refuses a wrong password, token and salt alike", func() {
		wrongCredentials(rest("ping", url.Values{
			"u": {u.Email}, "p": {"nope"}, "f": {"json"},
		}))
		wrongCredentials(u.get("ping", url.Values{"t": {"0123456789abcdef"}}))
		wrongCredentials(u.get("ping", url.Values{"s": {"another-salt"}}))
		wrongCredentials(rest("ping", url.Values{
			"u": {u.Email}, "p": {"enc:not-hex"}, "f": {"json"},
		}))
	})

	It("refuses an unknown user the same way", func() {
		wrongCredentials(u.get("ping", url.Values{"u": {"nobody@e2e.local"}}))
	})

	It("refuses an account with no Subsonic password", func() {
		bare := app.newUser("bare")
		bare.Password = "anything"
		wrongCredentials(bare.get("ping", nil))
		wrongCredentials(rest("ping", url.Values{
			"u": {bare.Email}, "p": {""}, "f": {"json"},
		}))
	})

	It("refuses an account Streamline no longer has", func() {
		gone := app.subsonicUser("gone")
		Expect(gone.call("ping", nil)["status"]).To(Equal("ok"))
		app.api(
			http.MethodDelete,
			"/api/v1/users/"+itoa(gone.ID),
			nil,
			http.StatusNoContent,
		)
		wrongCredentials(gone.get("ping", nil))
	})

	It("asks for the user name when it is missing", func() {
		raw := rest("ping", url.Values{"p": {"x"}, "f": {"json"}})
		errObj := raw.json()["error"].(map[string]any)
		Expect(errorCode(errObj)).To(Equal(10))
		Expect(errObj["message"]).To(Equal("Required parameter is missing: u"))
	})

	It("authenticates before it looks for the endpoint", func() {
		wrongCredentials(rest("noSuchEndpoint", url.Values{
			"u": {u.Email}, "p": {"nope"}, "f": {"json"},
		}))
	})

	It("records the client that signed in", func() {
		u.call("ping", url.Values{"c": {"Symfonium"}})
		db := app.ext()
		defer db.Close()
		var client string
		Expect(db.QueryRow(
			"SELECT last_client FROM accounts WHERE user_id = ?", u.ID,
		).Scan(&client)).To(Succeed())
		Expect(client).To(Equal("Symfonium"))
	})
})

var _ = Describe("the credential card", Label("e2e"), func() {
	It("sets, uses and revokes a Subsonic password", func() {
		u := app.newUser("card")
		session := app.login(u.Email, "e2e-Login-Passw0rd!")
		path := "/api/v1/account/extensions/" + extensionName + "/credential"

		state := app.apiAs(session, http.MethodGet, path, nil, http.StatusOK)
		Expect(state["enabled"]).To(BeFalse())
		Expect(state["username"]).To(Equal(u.Email))

		created := app.apiAs(session, http.MethodPost, path, nil, http.StatusOK)
		secret, ok := created["secret"].(string)
		Expect(ok).To(BeTrue(), "no secret in %v", created)
		Expect(secret).To(HaveLen(24))
		u.Password = secret

		u.call("ping", url.Values{"c": {"CardApp"}})
		state = app.apiAs(session, http.MethodGet, path, nil, http.StatusOK)
		Expect(state["enabled"]).To(BeTrue())
		Expect(state["created_at"]).NotTo(BeEmpty())
		Expect(state["last_client"]).To(Equal("CardApp"))
		Expect(state["last_used_at"]).NotTo(BeEmpty())
		Expect(state).NotTo(HaveKey("secret"))

		rotated := app.apiAs(session, http.MethodPost, path, nil, http.StatusOK)
		Expect(rotated["secret"]).NotTo(Equal(secret))
		Expect(u.get("ping", nil).json()["status"]).To(Equal("failed"))
		u.Password = rotated["secret"].(string)
		Expect(u.call("ping", nil)["status"]).To(Equal("ok"))

		app.apiAs(session, http.MethodDelete, path, nil, http.StatusNoContent)
		state = app.apiAs(session, http.MethodGet, path, nil, http.StatusOK)
		Expect(state["enabled"]).To(BeFalse())
		Expect(u.get("ping", nil).json()["status"]).To(Equal("failed"))
	})
})

var _ = Describe("the settings card", Label("e2e"), func() {
	cardValues := func(uid uint32) map[string]any {
		GinkgoHelper()
		cards := app.apiList(
			http.MethodGet, "/api/v1/users/"+itoa(uid)+"/extensions", http.StatusOK,
		)
		for _, c := range cards {
			if c["extension"] == extensionName && c["card"] == "settings" {
				values, ok := c["values"].(map[string]any)
				Expect(ok).To(BeTrue(), "no values in %v", c)
				return values
			}
		}
		Fail("the settings card is not listed")
		return nil
	}
	refused := func(u *testUser, values map[string]any) {
		GinkgoHelper()
		app.api(
			http.MethodPut,
			"/api/v1/users/"+itoa(
				u.ID,
			)+"/extensions/"+extensionName+"/cards/settings",
			map[string]any{"values": values},
			http.StatusUnprocessableEntity,
		)
	}

	It("starts with every permission on and no cap", func() {
		u := app.newUser("settings")
		Expect(cardValues(u.ID)).To(Equal(map[string]any{
			"can_stream":           true,
			"can_download":         true,
			"can_manage_playlists": true,
			"can_share":            true,
			"can_rate":             true,
			"can_change_password":  true,
			"max_bit_rate":         float64(0),
		}))
	})

	It("stores what an admin sets and leaves the rest alone", func() {
		u := app.newUser("settings")
		app.setSettings(u, map[string]any{"can_share": false, "max_bit_rate": 128})
		values := cardValues(u.ID)
		Expect(values["can_share"]).To(BeFalse())
		Expect(values["max_bit_rate"]).To(BeEquivalentTo(128))
		Expect(values["can_stream"]).To(BeTrue())

		app.setSettings(u, map[string]any{"can_stream": false})
		values = cardValues(u.ID)
		Expect(values["can_stream"]).To(BeFalse())
		Expect(values["can_share"]).To(BeFalse())
		Expect(values["max_bit_rate"]).To(BeEquivalentTo(128))
	})

	It("keeps one account's settings from another's", func() {
		a, b := app.newUser("settings"), app.newUser("settings")
		app.setSettings(a, map[string]any{"can_download": false})
		Expect(cardValues(b.ID)["can_download"]).To(BeTrue())
	})

	It("leaves the password alone", func() {
		u := app.subsonicUser("settings")
		app.setSettings(u, map[string]any{"can_rate": false})
		Expect(u.call("ping", nil)["status"]).To(Equal("ok"))
	})

	It("refuses a value the card does not allow", func() {
		u := app.newUser("settings")
		refused(u, map[string]any{"max_bit_rate": 321})
		refused(u, map[string]any{"can_stream": "yes"})
		refused(u, map[string]any{"unknown": true})
	})

	It("is not reachable as the signed-in user", func() {
		app.apiRaw(
			app.adminToken, http.MethodGet,
			"/api/v1/extensions/"+extensionName+"/cards/settings",
			nil, http.StatusNotFound,
		)
	})
})

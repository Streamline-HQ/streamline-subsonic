package e2e

import (
	"bytes"
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"

	"github.com/delucks/go-subsonic"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testUser is a Streamline account with, once setSubsonicPassword ran, a
// Subsonic password.
type testUser struct {
	ID    uint32
	Email string
	Role  string
	// Password is the Subsonic password, empty until set.
	Password string
}

// newUser creates a member account through the admin API. It has no Subsonic
// password yet and, having no row in the extension, every permission on.
func (i *instance) newUser(prefix string) *testUser {
	GinkgoHelper()
	return i.newUserWithRole(prefix, "member")
}

func (i *instance) newUserWithRole(prefix, role string) *testUser {
	GinkgoHelper()
	email := i.email(prefix)
	created := i.api(http.MethodPost, "/api/v1/users", map[string]any{
		"email":    email,
		"password": "e2e-Login-Passw0rd!",
		"role":     role,
	}, http.StatusCreated)
	id, ok := created["id"].(float64)
	Expect(ok).To(BeTrue(), "no id in %v", created)
	return &testUser{ID: uint32(id), Email: email, Role: role}
}

// subsonicUser is newUser with a Subsonic password already set, the common
// start of a spec.
func (i *instance) subsonicUser(prefix string) *testUser {
	GinkgoHelper()
	u := i.newUser(prefix)
	i.setSubsonicPassword(u, "pw-"+u.Email)
	return u
}

// setSubsonicPassword writes the password into the extension's database, the
// way the credential card or changePassword would, without a web session.
func (i *instance) setSubsonicPassword(u *testUser, password string) {
	GinkgoHelper()
	db := i.ext()
	defer db.Close()
	_, err := db.Exec(
		`INSERT INTO accounts(user_id, password, password_created_at)
		VALUES(?, ?, '2026-01-01T00:00:00.000Z')
		ON CONFLICT(user_id) DO UPDATE SET password = excluded.password`,
		u.ID, password,
	)
	Expect(err).NotTo(HaveOccurred())
	u.Password = password
}

// setSettings sets the account's permissions through the settings card, as an
// admin does: keys are the card's field ids (can_stream, can_download,
// can_manage_playlists, can_share, can_rate, can_change_password,
// max_bit_rate), a key left out stays as it is.
func (i *instance) setSettings(u *testUser, values map[string]any) {
	GinkgoHelper()
	i.api(
		http.MethodPut,
		"/api/v1/users/"+itoa(u.ID)+"/extensions/"+extensionName+"/cards/settings",
		map[string]any{"values": values},
		http.StatusNoContent,
	)
}

// client is a go-subsonic client signed in as u with the token scheme.
func (u *testUser) client() *subsonic.Client {
	GinkgoHelper()
	Expect(u.Password).NotTo(BeEmpty(), "%s has no Subsonic password", u.Email)
	c := &subsonic.Client{
		Client:     httpClient,
		BaseUrl:    app.BaseURL,
		User:       u.Email,
		ClientName: "e2e",
	}
	Expect(c.Authenticate(u.Password)).To(Succeed())
	return c
}

// signed is the query a token-scheme client sends: u, t, s, v and c, with f
// set to json unless extra says otherwise.
func (u *testUser) signed(extra url.Values) url.Values {
	const salt = "e2esalt"
	sum := md5.Sum([]byte(u.Password + salt)) //nolint:gosec // see import
	q := url.Values{
		"u": {u.Email},
		"t": {hex.EncodeToString(sum[:])},
		"s": {salt},
		"v": {"1.16.1"},
		"c": {"e2e"},
		"f": {"json"},
	}
	maps.Copy(q, extra)
	return q
}

// rawResponse is an answer exactly as it came, for the specs where the bytes
// are the subject.
type rawResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// rest sends GET /rest/<endpoint> with q as given, signing nothing.
func rest(endpoint string, q url.Values) rawResponse {
	GinkgoHelper()
	return fetch(http.MethodGet, "/rest/"+endpoint+"?"+q.Encode(), nil, nil)
}

// get sends GET /rest/<endpoint> signed as u; extra overrides the signed
// parameters and adds the endpoint's own.
func (u *testUser) get(endpoint string, extra url.Values) rawResponse {
	GinkgoHelper()
	return rest(endpoint, u.signed(extra))
}

// postForm sends the signed parameters as a form body, as formPost clients do.
func (u *testUser) postForm(endpoint string, extra url.Values) rawResponse {
	GinkgoHelper()
	return fetch(
		http.MethodPost,
		"/rest/"+endpoint,
		[]byte(u.signed(extra).Encode()),
		http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
	)
}

// fetch sends one request to the instance with no credentials of its own.
func fetch(method, path string, body []byte, header http.Header) rawResponse {
	GinkgoHelper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, app.BaseURL+path, reader)
	Expect(err).NotTo(HaveOccurred())
	maps.Copy(req.Header, header)
	resp, err := httpClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return rawResponse{Status: resp.StatusCode, Header: resp.Header, Body: raw}
}

// json is the "subsonic-response" object of a JSON answer.
func (r rawResponse) json() map[string]any {
	GinkgoHelper()
	var out map[string]map[string]any
	Expect(json.Unmarshal(r.Body, &out)).To(Succeed(), string(r.Body))
	resp, ok := out["subsonic-response"]
	Expect(ok).To(BeTrue(), "no subsonic-response in %s", r.Body)
	return resp
}

// call is get that expects a JSON answer with status ok, and returns the
// response object.
func (u *testUser) call(endpoint string, extra url.Values) map[string]any {
	GinkgoHelper()
	raw := u.get(endpoint, extra)
	Expect(raw.Status).To(Equal(http.StatusOK))
	resp := raw.json()
	Expect(resp["status"]).To(Equal("ok"), "%v", resp["error"])
	return resp
}

// callError is get that expects status failed, and returns its error object.
func (u *testUser) callError(endpoint string, extra url.Values) map[string]any {
	GinkgoHelper()
	raw := u.get(endpoint, extra)
	Expect(raw.Status).To(Equal(http.StatusOK))
	resp := raw.json()
	Expect(resp["status"]).To(Equal("failed"), "%s answered %v", endpoint, resp)
	errObj, ok := resp["error"].(map[string]any)
	Expect(ok).To(BeTrue(), "no error in %v", resp)
	return errObj
}

// errorCode is an error object's code.
func errorCode(errObj map[string]any) int {
	GinkgoHelper()
	f, ok := errObj["code"].(float64)
	Expect(ok).To(BeTrue(), "no code in %v", errObj)
	return int(f)
}

// obj, list and ids read a decoded JSON answer without a struct per
// endpoint.
func obj(m map[string]any, key string) map[string]any {
	GinkgoHelper()
	v, ok := m[key].(map[string]any)
	Expect(ok).To(BeTrue(), "no object %q in %v", key, m)
	return v
}

func list(m map[string]any, key string) []map[string]any {
	GinkgoHelper()
	raw, _ := m[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		item, ok := v.(map[string]any)
		Expect(ok).To(BeTrue(), "%v in %q is not an object", v, key)
		out = append(out, item)
	}
	return out
}

func ids(items []map[string]any) []string {
	GinkgoHelper()
	out := make([]string, 0, len(items))
	for _, it := range items {
		id, ok := it["id"].(string)
		Expect(ok).To(BeTrue(), "no id in %v", it)
		out = append(out, id)
	}
	return out
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

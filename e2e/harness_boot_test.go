package e2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "modernc.org/sqlite"
)

const (
	extensionName = "subsonic"

	adminEmail    = "e2e-admin@streamline.local"
	adminPassword = "e2e-Admin-Passw0rd!"

	// scratchRoot holds one directory per booted instance, removed with it.
	scratchRoot = "/tmp/streamline/subsonic-e2e"

	sessionCookie = "streamline_session"
)

// instance is one running Streamline with the extension loaded.
type instance struct {
	BaseURL string
	// Dir is the instance's scratch directory; everything below it is removed
	// when the suite ends.
	Dir        string
	DataDir    string
	MusicDir   string
	ArtworkDir string
	CacheDir   string

	adminToken string
	admin      *testUser
	core       *sql.DB
	counter    int
	shared     *library
}

// boot starts the binary named by STREAMLINE_BIN with a scratch config on a
// free port, grants subsonic.wasm (SUBSONIC_WASM) exactly what its manifest
// asks for through the approve endpoint, and returns once the extension runs.
// Everything it starts is torn down with the suite.
func boot() *instance {
	GinkgoHelper()
	bin := os.Getenv("STREAMLINE_BIN")
	Expect(
		bin,
	).NotTo(BeEmpty(), "STREAMLINE_BIN must name a built streamline binary")
	Expect(bin).To(BeAnExistingFile())
	wasm := os.Getenv("SUBSONIC_WASM")
	if wasm == "" {
		abs, err := filepath.Abs("../subsonic.wasm")
		Expect(err).NotTo(HaveOccurred())
		wasm = abs
	}
	Expect(wasm).To(BeAnExistingFile(), "build subsonic.wasm first (task build)")
	ffmpeg, err := exec.LookPath("ffmpeg")
	Expect(err).NotTo(HaveOccurred(), "the suite needs a real ffmpeg on PATH")

	Expect(os.MkdirAll(scratchRoot, 0o755)).To(Succeed())
	dir, err := os.MkdirTemp(scratchRoot, "run-")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(os.RemoveAll(dir)).To(Succeed()) })

	i := &instance{
		Dir:        dir,
		DataDir:    filepath.Join(dir, "data"),
		MusicDir:   filepath.Join(dir, "music"),
		ArtworkDir: filepath.Join(dir, "data", "posters"),
		CacheDir:   filepath.Join(dir, "data", "extensions", extensionName, "cache"),
	}
	for _, d := range []string{"movies", "series", "ebooks", "audiobooks", "downloads"} {
		Expect(os.MkdirAll(filepath.Join(dir, d), 0o755)).To(Succeed())
	}
	Expect(os.MkdirAll(i.MusicDir, 0o755)).To(Succeed())

	port := freePort()
	i.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig(cfgPath, i, port, wasm, filepath.Dir(ffmpeg))

	i.start(bin, cfgPath)
	i.waitHealthy()
	i.adminToken = i.login(adminEmail, adminPassword)
	i.approveExtension()
	i.touchExtension()

	core, err := sql.Open(
		"sqlite",
		"file:"+filepath.Join(
			i.DataDir,
			"streamline.db",
		)+"?_pragma=busy_timeout(10000)",
	)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(core.Close()).To(Succeed()) })
	i.core = core

	i.admin = &testUser{ID: 1, Email: adminEmail, Role: "admin"}
	return i
}

func freePort() int {
	GinkgoHelper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	Expect(ok).To(BeTrue())
	return addr.Port
}

// writeConfig writes the instance's config. JSON is YAML, so the standard
// library is enough. The extension entry carries no grants: approving it is
// what grants exactly the manifest.
func writeConfig(path string, i *instance, port int, wasm, ffmpegDir string) {
	GinkgoHelper()
	cfg := map[string]any{
		"data_dir": i.DataDir,
		"server":   map[string]any{"host": "127.0.0.1", "port": port},
		"library": map[string]any{
			"movie_path":     filepath.Join(i.Dir, "movies"),
			"series_path":    filepath.Join(i.Dir, "series"),
			"music_path":     i.MusicDir,
			"ebook_path":     filepath.Join(i.Dir, "ebooks"),
			"audiobook_path": filepath.Join(i.Dir, "audiobooks"),
			"download_path":  filepath.Join(i.Dir, "downloads"),
		},
		"auth": map[string]any{
			"session_secret": "e2e-session-secret",
			// Hidden seam: every spec shares one loopback address.
			"api_failure_limit": "0",
			"seed_admin": map[string]any{
				"email":    adminEmail,
				"password": adminPassword,
			},
		},
		"ffmpeg": map[string]any{"enabled": true, "path": ffmpegDir},
		"log": map[string]any{
			"app":  map[string]any{"level": "warn"},
			"http": map[string]any{"enabled": false},
		},
		"extensions": []map[string]any{{
			"name":    extensionName,
			"source":  wasm,
			"enabled": true,
		}},
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(path, raw, 0o600)).To(Succeed())
}

// start runs the binary with a clean environment: nothing of the
// developer's STREAMLINE_* settings reaches it.
func (i *instance) start(bin, cfgPath string) {
	GinkgoHelper()
	cmd := exec.Command(bin, "--config", cfgPath)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + i.Dir,
		"TMPDIR=" + i.Dir,
	}
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Start()).To(Succeed())
	exited := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		_ = cmd.Wait()
		close(exited)
	}()
	DeferCleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
}

func (i *instance) waitHealthy() {
	GinkgoHelper()
	Eventually(func() (int, error) {
		resp, err := http.Get(i.BaseURL + "/health")
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}, "30s", "100ms").Should(Equal(http.StatusOK))
}

// login signs in through the web endpoint and returns the session JWT, which
// /api/v1 takes as a Bearer token. The endpoint allows 5 attempts per 15
// minutes per address for the whole instance: boot spends one, so a spec that
// needs a session of its own spends at most a couple more.
func (i *instance) login(email, password string) string {
	GinkgoHelper()
	resp := i.do(http.MethodPost, "/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	Fail("the login response did not set the session cookie")
	return ""
}

// do sends one request to Streamline. token is a session JWT, "" for none; a
// non-nil body is sent as JSON.
func (i *instance) do(method, path, token string, body any) *http.Response {
	GinkgoHelper()
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, i.BaseURL+path, payload)
	Expect(err).NotTo(HaveOccurred())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// api is do as the seed admin, answering with the decoded JSON body (nil for
// an empty one) and failing the spec on any status but want.
func (i *instance) api(method, path string, body any, want int) map[string]any {
	GinkgoHelper()
	return i.apiAs(i.adminToken, method, path, body, want)
}

func (i *instance) apiAs(
	token, method, path string,
	body any,
	want int,
) map[string]any {
	GinkgoHelper()
	raw := i.apiRaw(token, method, path, body, want)
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var out map[string]any
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), string(raw))
	return out
}

// apiList is api for an endpoint that answers a JSON array.
func (i *instance) apiList(method, path string, want int) []map[string]any {
	GinkgoHelper()
	var out []map[string]any
	raw := i.apiRaw(i.adminToken, method, path, nil, want)
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), string(raw))
	return out
}

func (i *instance) apiRaw(
	token, method, path string,
	body any,
	want int,
) []byte {
	GinkgoHelper()
	resp := i.do(method, path, token, body)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(want), string(raw))
	return raw
}

func (i *instance) approveExtension() {
	GinkgoHelper()
	i.api(
		http.MethodPost,
		"/api/v1/extensions/"+extensionName+"/approve",
		nil,
		http.StatusOK,
	)
	Eventually(func() string {
		return i.extension()["state"].(string)
	}, "30s", "200ms").Should(Equal("running"), "the extension did not start")
}

// extension is the extension's entry from GET /extensions.
func (i *instance) extension() map[string]any {
	GinkgoHelper()
	list := i.api(http.MethodGet, "/api/v1/extensions", nil, http.StatusOK)
	items, _ := list["items"].([]any)
	for _, it := range items {
		ext, _ := it.(map[string]any)
		if ext["name"] == extensionName {
			return ext
		}
	}
	Fail("the extension is not listed")
	return nil
}

// touchExtension makes the extension create its tables, which the helpers
// that write to its database need.
func (i *instance) touchExtension() {
	GinkgoHelper()
	resp, err := httpClient.Get(i.BaseURL + "/rest/getOpenSubsonicExtensions?f=json")
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
}

// ext opens the extension's own database. The caller closes it.
func (i *instance) ext() *sql.DB {
	GinkgoHelper()
	db, err := sql.Open("sqlite", "file:"+i.extPath()+"?_pragma=busy_timeout(10000)")
	Expect(err).NotTo(HaveOccurred())
	return db
}

func (i *instance) extPath() string {
	return filepath.Join(i.DataDir, "extensions", extensionName, "data.db")
}

// unique makes a name no other spec of this instance uses.
func (i *instance) unique(prefix string) string {
	i.counter++
	return fmt.Sprintf("%s-%d", prefix, i.counter)
}

func (i *instance) email(prefix string) string {
	return strings.ToLower(i.unique(prefix)) + "@e2e.local"
}

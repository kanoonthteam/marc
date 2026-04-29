package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRelease serves a /repos/<repo>/releases/latest endpoint and the asset
// + checksums.txt downloads referenced from it. The asset bodies are arbitrary
// — what matters is the checksum lines up.
type fakeRelease struct {
	tag    string
	assets map[string][]byte // asset name → content
}

func (f *fakeRelease) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			body := struct {
				TagName string `json:"tag_name"`
				Assets  []struct {
					Name string `json:"name"`
					URL  string `json:"browser_download_url"`
				} `json:"assets"`
			}{TagName: f.tag}
			for name := range f.assets {
				body.Assets = append(body.Assets, struct {
					Name string `json:"name"`
					URL  string `json:"browser_download_url"`
				}{Name: name, URL: "http://" + r.Host + "/asset/" + name})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		case strings.HasPrefix(r.URL.Path, "/asset/"):
			name := strings.TrimPrefix(r.URL.Path, "/asset/")
			data, ok := f.assets[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// optsAgainst builds an Options that points at the fake server. The repo
// name is irrelevant because the fake handler matches on path suffix.
func optsAgainst(srv *httptest.Server, currentVersion, target string) Options {
	return Options{
		Repo:           "fakeowner/marc",
		CurrentVersion: currentVersion,
		TargetPath:     target,
		GOOS:           "linux",
		GOARCH:         "amd64",
		HTTPClient: &http.Client{
			Transport: rewriteTransport{base: http.DefaultTransport, host: strings.TrimPrefix(srv.URL, "http://")},
		},
		Stdout: &bytes.Buffer{},
	}
}

// rewriteTransport sends every outgoing request to the fake server while
// preserving the path. That lets the production code keep using
// "https://api.github.com/..." URLs unchanged.
type rewriteTransport struct {
	base http.RoundTripper
	host string
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = rt.host
	return rt.base.RoundTrip(r2)
}

// makeChecksumsTxt returns the canonical sha256sum-style body
// "<hex>  <name>\n" for each asset. The function also injects the rendered
// checksums into the asset map so it's downloadable from the fake server.
func makeChecksumsTxt(assets map[string][]byte) []byte {
	var b strings.Builder
	for name, content := range assets {
		if name == "checksums.txt" {
			continue
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(b.String())
}

// writeStubBinary writes a placeholder binary at path so the update can
// rename over it.
func writeStubBinary(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "marc")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("writeStubBinary: %v", err)
	}
	return path
}

// --- Tests --------------------------------------------------------------

func TestRun_UpToDate_NoDownload(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new-binary"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.2.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old-binary")
	opts := optsAgainst(srv, "v0.2.0", target)

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.UpToDate {
		t.Errorf("want UpToDate=true, got %+v", res)
	}
	if res.Replaced {
		t.Errorf("Replaced should be false on up-to-date run")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old-binary" {
		t.Errorf("target was rewritten on up-to-date run: %q", got)
	}
}

func TestRun_CheckOnly_DoesNotDownload(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new-binary"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old-binary")
	opts := optsAgainst(srv, "v0.2.0", target)
	opts.CheckOnly = true

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.LatestVersion != "v0.3.0" {
		t.Errorf("LatestVersion: want v0.3.0, got %q", res.LatestVersion)
	}
	if res.Replaced {
		t.Errorf("Replaced must be false in CheckOnly mode")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old-binary" {
		t.Errorf("target was rewritten in CheckOnly mode")
	}
}

func TestRun_FullUpdate_ReplacesBinary(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("the-new-marc-binary-bytes"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	dir := t.TempDir()
	target := writeStubBinary(t, dir, "old-marc")
	opts := optsAgainst(srv, "v0.2.0", target)

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Replaced {
		t.Errorf("want Replaced=true, got %+v", res)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "the-new-marc-binary-bytes" {
		t.Errorf("target content after update: got %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("target lost executable bit: mode %v", info.Mode())
	}
}

func TestRun_ChecksumMismatch_RefusesInstall(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("legitimate"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	// After computing the checksum, swap the asset bytes so the file the
	// installer downloads doesn't match the published hash.
	assets["marc-linux-amd64"] = []byte("TAMPERED")
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old-marc")
	opts := optsAgainst(srv, "v0.2.0", target)

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run should have errored on checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should mention checksum mismatch, got: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old-marc" {
		t.Errorf("target was overwritten despite checksum mismatch — security bug")
	}
}

func TestRun_MissingArchAsset_FailsLoudly(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old")
	opts := optsAgainst(srv, "v0.2.0", target)
	opts.GOARCH = "arm64" // request an arch the release doesn't have

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error when target arch isn't published")
	}
	if !strings.Contains(err.Error(), "marc-linux-arm64") {
		t.Errorf("error should name the missing asset, got: %v", err)
	}
}

func TestRun_MissingChecksumsTxt_RefusesInstall(t *testing.T) {
	// No checksums.txt — installer must NOT install an unverified binary.
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new"),
	}
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old")
	opts := optsAgainst(srv, "v0.2.0", target)

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error when checksums.txt is absent")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("error should mention checksums.txt, got: %v", err)
	}
}

func TestRun_PermissionDenied_StagesBinaryAndPrintsHint(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	// Place the target inside a read-only directory so os.Rename can't
	// overwrite it. The directory must be writable to create the stub, then
	// we lock it down.
	dir := t.TempDir()
	target := writeStubBinary(t, dir, "old-marc")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	stdout := &bytes.Buffer{}
	opts := optsAgainst(srv, "v0.2.0", target)
	opts.Stdout = stdout

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("permission-denied path should not return an error (it stages instead): %v", err)
	}
	if res.Replaced {
		t.Errorf("Replaced should be false when target is read-only")
	}
	if res.StagedPath == "" {
		t.Fatalf("StagedPath should be set when install is deferred")
	}
	if _, err := os.Stat(res.StagedPath); err != nil {
		t.Errorf("staged binary missing: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "sudo install") {
		t.Errorf("stdout should suggest `sudo install`, got: %s", out)
	}
	if !strings.Contains(out, target) {
		t.Errorf("stdout should mention the target path, got: %s", out)
	}
}

func TestRun_DevVersion_TreatsAsOutOfDate(t *testing.T) {
	assets := map[string][]byte{
		"marc-linux-amd64": []byte("new"),
	}
	assets["checksums.txt"] = makeChecksumsTxt(assets)
	rel := &fakeRelease{tag: "v0.3.0", assets: assets}
	srv := rel.start(t)

	target := writeStubBinary(t, t.TempDir(), "old")
	opts := optsAgainst(srv, "dev", target)
	opts.CheckOnly = true

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.UpToDate {
		t.Errorf("'dev' build should never be considered up to date")
	}
}

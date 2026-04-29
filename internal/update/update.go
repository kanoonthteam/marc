// Package update implements `marc update`: in-place self-replacement of the
// running marc binary with the latest published release on GitHub.
//
// Flow:
//  1. GET https://api.github.com/repos/<repo>/releases/latest
//  2. Compare the tag against the binary's compile-time version.
//  3. Download the matching asset (marc-<goos>-<goarch>) and `checksums.txt`.
//  4. Verify the SHA-256 of the downloaded asset matches the checksums file.
//  5. Atomically rename the verified binary over os.Executable().
//
// The package has no external dependencies beyond stdlib + crypto/sha256.
// All network and filesystem behavior is injection-pointed via Options so
// tests can drive each step against fakes.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultRepo is the GitHub owner/name pair marc updates from.
const DefaultRepo = "kanoonthteam/marc"

// Options controls a single update run. Zero-value defaults mirror production.
type Options struct {
	// Repo is the GitHub <owner>/<name> to query. Defaults to DefaultRepo.
	Repo string

	// CurrentVersion is the running binary's version string (e.g. "v0.2.0").
	// If empty or "dev" the update is treated as out-of-date.
	CurrentVersion string

	// TargetPath is the path that will be replaced with the new binary.
	// Defaults to os.Executable() (with no symlink resolution, so a symlink
	// target gets replaced via the symlink — same semantics as `install`).
	TargetPath string

	// GOOS / GOARCH override runtime.GOOS / runtime.GOARCH for tests.
	GOOS, GOARCH string

	// HTTPClient is used for both the API call and the asset download.
	// nil → http.DefaultClient with a 60s timeout.
	HTTPClient *http.Client

	// CheckOnly, when true, skips the download/replace steps and only
	// reports the current vs. latest version.
	CheckOnly bool

	// Stdout receives progress output. nil → io.Discard.
	Stdout io.Writer
}

// Result describes what the update actually did (or would do, in CheckOnly).
type Result struct {
	CurrentVersion string
	LatestVersion  string
	AssetName      string // e.g. "marc-darwin-arm64"
	UpToDate       bool   // current == latest
	Replaced       bool   // a real update happened
	StagedPath     string // when Replaced=false but a binary was downloaded (e.g. permission denied)
}

// Run executes one update cycle.
func Run(ctx context.Context, opts Options) (Result, error) {
	res := Result{CurrentVersion: opts.CurrentVersion}

	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	out := opts.Stdout
	if out == nil {
		out = io.Discard
	}
	if opts.TargetPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return res, fmt.Errorf("update: locate self: %w", err)
		}
		opts.TargetPath = exe
	}

	rel, err := fetchLatestRelease(ctx, opts.HTTPClient, opts.Repo)
	if err != nil {
		return res, err
	}
	res.LatestVersion = rel.TagName
	res.AssetName = fmt.Sprintf("marc-%s-%s", opts.GOOS, opts.GOARCH)

	if normalizeVersion(opts.CurrentVersion) == normalizeVersion(rel.TagName) && opts.CurrentVersion != "" {
		res.UpToDate = true
		fmt.Fprintf(out, "✓ already at %s (latest)\n", rel.TagName)
		return res, nil
	}

	fmt.Fprintf(out, "→ %s available (you have %s)\n", rel.TagName, displayVersion(opts.CurrentVersion))

	if opts.CheckOnly {
		return res, nil
	}

	assetURL, sumsURL, err := pickAssetURLs(rel, res.AssetName)
	if err != nil {
		return res, err
	}

	stage, err := os.MkdirTemp("", "marc-update-")
	if err != nil {
		return res, fmt.Errorf("update: stage dir: %w", err)
	}
	stagedBin := filepath.Join(stage, res.AssetName)

	fmt.Fprintf(out, "  downloading %s ...\n", res.AssetName)
	if err := downloadTo(ctx, opts.HTTPClient, assetURL, stagedBin); err != nil {
		_ = os.RemoveAll(stage)
		return res, err
	}

	fmt.Fprintf(out, "  verifying checksum ...\n")
	if err := verifyChecksum(ctx, opts.HTTPClient, sumsURL, stagedBin, res.AssetName); err != nil {
		_ = os.RemoveAll(stage)
		return res, err
	}

	if err := os.Chmod(stagedBin, 0o755); err != nil {
		_ = os.RemoveAll(stage)
		return res, fmt.Errorf("update: chmod staged binary: %w", err)
	}

	// Try to replace the running binary in place. If we can't write to
	// TargetPath's directory (typical /usr/local/bin owned by root case),
	// leave the staged binary alone and tell the operator how to install it.
	if err := atomicReplace(stagedBin, opts.TargetPath); err != nil {
		if errors.Is(err, os.ErrPermission) {
			res.StagedPath = stagedBin
			fmt.Fprintf(out, "✗ cannot write to %s (permission denied).\n", opts.TargetPath)
			fmt.Fprintf(out, "  The new binary is staged at: %s\n", stagedBin)
			fmt.Fprintf(out, "  Install with: sudo install %s %s\n", stagedBin, opts.TargetPath)
			return res, nil
		}
		_ = os.RemoveAll(stage)
		return res, err
	}
	_ = os.RemoveAll(stage) // staged file already moved; just clean the dir

	res.Replaced = true
	fmt.Fprintf(out, "✓ replaced %s with %s\n", opts.TargetPath, rel.TagName)
	fmt.Fprintln(out, "  Restart the daemons to pick up the new binary:")
	switch opts.GOOS {
	case "linux":
		fmt.Fprintln(out, "    sudo systemctl restart marc-proxy marc-ship")
	case "darwin":
		fmt.Fprintln(out, `    launchctl kickstart -k "gui/$(id -u)/io.marc.proxy"`)
		fmt.Fprintln(out, `    launchctl kickstart -k "gui/$(id -u)/io.marc.ship"`)
	}
	return res, nil
}

// release is the subset of the GitHub Releases API we care about.
type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestRelease(ctx context.Context, c *http.Client, repo string) (release, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, fmt.Errorf("update: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("update: fetch latest release: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return release{}, fmt.Errorf("update: GitHub returned %d: %s", resp.StatusCode, body)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return release{}, fmt.Errorf("update: decode release JSON: %w", err)
	}
	if r.TagName == "" {
		return release{}, errors.New("update: GitHub response had no tag_name")
	}
	return r, nil
}

func pickAssetURLs(rel release, assetName string) (asset, sums string, err error) {
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			asset = a.URL
		case "checksums.txt":
			sums = a.URL
		}
	}
	if asset == "" {
		var avail []string
		for _, a := range rel.Assets {
			avail = append(avail, a.Name)
		}
		return "", "", fmt.Errorf("update: no %s asset on release %s; available: %s",
			assetName, rel.TagName, strings.Join(avail, ", "))
	}
	if sums == "" {
		return "", "", fmt.Errorf("update: release %s has no checksums.txt — refusing to install unverified binary", rel.TagName)
	}
	return asset, sums, nil
}

func downloadTo(ctx context.Context, c *http.Client, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("update: build download request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("update: download %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update: download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("update: create %s: %w", dst, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return fmt.Errorf("update: copy to %s: %w", dst, err)
	}
	return f.Close()
}

func verifyChecksum(ctx context.Context, c *http.Client, sumsURL, binPath, assetName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return fmt.Errorf("update: build checksums request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("update: download checksums.txt: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update: checksums.txt: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("update: read checksums.txt: %w", err)
	}

	want := ""
	for _, line := range strings.Split(string(body), "\n") {
		// Format is `<sha256>  <filename>` (two spaces) per `sha256sum`.
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("update: %s not listed in checksums.txt", assetName)
	}

	f, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("update: open staged binary: %w", err)
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("update: hash staged binary: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("update: checksum mismatch for %s: want %s, got %s", assetName, want, got)
	}
	return nil
}

// atomicReplace renames src over dst on the same filesystem. If src and dst
// are on different filesystems (e.g. /tmp on tmpfs vs /usr/local/bin on the
// root volume), os.Rename surfaces EXDEV — fall back to copy + rename so the
// final step on dst's filesystem is still atomic.
func atomicReplace(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "cross-device") && !strings.Contains(err.Error(), "EXDEV") {
		return err
	}
	tmp := dst + ".update.tmp"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// normalizeVersion strips a leading 'v' so "v0.2.0" and "0.2.0" compare equal.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	// Strip GoReleaser dirty suffixes like "0.2.0-dirty" or git-describe-style
	// trailers introduced by local builds. We only treat exact-tag matches as
	// "up to date" for simplicity.
	if i := strings.IndexAny(v, "-+"); i > 0 {
		v = v[:i]
	}
	return v
}

func displayVersion(v string) string {
	if v == "" || v == "dev" {
		return "an unversioned dev build"
	}
	return v
}

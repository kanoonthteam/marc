package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ClientPaths holds filesystem paths used by the marc client daemon.
type ClientPaths struct {
	CaptureFile string `toml:"capture_file"`
	LogFile     string `toml:"log_file"`
}

// ClientProxy holds proxy listen and upstream settings.
type ClientProxy struct {
	ListenAddr      string   `toml:"listen_addr"`
	UpstreamURL     string   `toml:"upstream_url"`
	StrippedHeaders []string `toml:"stripped_headers"`
}

// ClientShipper holds rotation and upload timing settings.
type ClientShipper struct {
	RotateSizeMB        int `toml:"rotate_size_mb"`
	ShipIntervalSeconds int `toml:"ship_interval_seconds"`
}

// ClientMinIO holds MinIO connection settings for the client.
type ClientMinIO struct {
	Endpoint  string `toml:"endpoint"`
	Bucket    string `toml:"bucket"`
	AccessKey string `toml:"access_key"`
	SecretKey string `toml:"secret_key"`
	VerifyTLS bool   `toml:"verify_tls"`
}

// ClientAnthropic holds the Anthropic API key used by `marc proxy --self-test`
// to send a real request through the proxy. The proxy itself does not use
// this; Claude Code passes its own key in the Authorization header, which the
// proxy forwards verbatim.
type ClientAnthropic struct {
	APIKey string `toml:"api_key"`
}

// ClientProfile describes a single LLM provider routing target.
//
// Profiles let one marc install talk to multiple providers (Anthropic,
// Minimax, OpenAI, etc.) via path-prefix routing. The proxy forwards
// /<name>/v1/* requests to the profile's BaseURL, applying the matching
// auth style and any header overrides.
type ClientProfile struct {
	// BaseURL is the upstream API root (e.g. "https://api.anthropic.com").
	BaseURL string `toml:"base_url"`

	// APIKeyEnv is the name of the environment variable holding this
	// profile's API key. Preferred over APIKey for security.
	APIKeyEnv string `toml:"api_key_env"`

	// APIKey is the inline API key. Optional — APIKeyEnv is preferred.
	// Only honoured when the config file mode is 0600.
	APIKey string `toml:"api_key"`

	// AuthStyle is "x-api-key" (Anthropic-native) or "bearer"
	// (OpenAI-compatible providers).
	AuthStyle string `toml:"auth_style"`

	// HeaderOverrides are extra request headers applied to every forwarded
	// request. Useful for providers that require pinned versions
	// (e.g. anthropic-version = "2023-06-01").
	HeaderOverrides map[string]string `toml:"header_overrides"`
}

// ClientConfig is the top-level configuration for the marc client binary.
// It is loaded from ~/.marc/config.toml (mode 0600).
type ClientConfig struct {
	MachineName    string                   `toml:"machine_name"`
	Paths          ClientPaths              `toml:"paths"`
	Proxy          ClientProxy              `toml:"proxy"`
	Shipper        ClientShipper            `toml:"shipper"`
	MinIO          ClientMinIO              `toml:"minio"`
	Anthropic      ClientAnthropic          `toml:"anthropic"`
	DefaultProfile string                   `toml:"default_profile"`
	Profiles       map[string]ClientProfile `toml:"profiles"`
}

// migrateProfiles ensures cfg.Profiles is non-empty. When the config has no
// [profiles] block (legacy single-provider configs), it synthesizes a default
// "anthropic" profile from the legacy [proxy] + [anthropic] blocks. This
// preserves backward compatibility — existing configs keep working unchanged.
//
// Also normalizes DefaultProfile: empty → "anthropic".
func (cfg *ClientConfig) migrateProfiles() {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ClientProfile)
	}
	if len(cfg.Profiles) == 0 {
		base := strings.TrimSpace(cfg.Proxy.UpstreamURL)
		if base == "" {
			base = "https://api.anthropic.com"
		}
		cfg.Profiles["anthropic"] = ClientProfile{
			BaseURL:   base,
			APIKeyEnv: "ANTHROPIC_API_KEY",
			APIKey:    cfg.Anthropic.APIKey,
			AuthStyle: "x-api-key",
		}
	}
	if strings.TrimSpace(cfg.DefaultProfile) == "" {
		cfg.DefaultProfile = "anthropic"
	}
}

// ResolveProfile returns the profile for the given name, falling back to
// DefaultProfile when name is empty. Returns an error if the resolved name
// doesn't exist in cfg.Profiles, listing available profiles in the message.
func (cfg *ClientConfig) ResolveProfile(name string) (string, ClientProfile, error) {
	resolved := strings.TrimSpace(name)
	if resolved == "" {
		resolved = cfg.DefaultProfile
	}
	if resolved == "" {
		resolved = "anthropic"
	}
	p, ok := cfg.Profiles[resolved]
	if !ok {
		available := make([]string, 0, len(cfg.Profiles))
		for k := range cfg.Profiles {
			available = append(available, k)
		}
		return "", ClientProfile{}, fmt.Errorf(
			"config: profile %q not found (available: %s)",
			resolved, strings.Join(available, ", "))
	}
	return resolved, p, nil
}

// LoadClient reads and parses the client config at path.
// It returns an error if the file permissions are not exactly 0600,
// if the TOML cannot be parsed, or if required fields are absent.
func LoadClient(path string) (*ClientConfig, error) {
	if err := checkMode(path); err != nil {
		return nil, err
	}

	var cfg ClientConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	// Expand ~/  in path fields.
	var expandErr error
	cfg.Paths.CaptureFile, expandErr = expandHome(cfg.Paths.CaptureFile)
	if expandErr != nil {
		return nil, fmt.Errorf("config: expanding capture_file: %w", expandErr)
	}
	cfg.Paths.LogFile, expandErr = expandHome(cfg.Paths.LogFile)
	if expandErr != nil {
		return nil, fmt.Errorf("config: expanding log_file: %w", expandErr)
	}

	if err := validateClient(&cfg); err != nil {
		return nil, err
	}

	cfg.migrateProfiles()

	return &cfg, nil
}

// PrintDefaultClient writes the spec's template client TOML to w.
// The output is parseable by LoadClient after the user fills in placeholder
// values and sets file mode 0600.
func PrintDefaultClient(w io.Writer) error {
	_, err := fmt.Fprint(w, defaultClientTOML)
	return err
}

// defaultClientTOML is the verbatim template from spec §"Client config file format".
const defaultClientTOML = `machine_name = "macbook-kanoon"
default_profile = "anthropic"

[paths]
capture_file = "~/.marc/capture.jsonl"
log_file = "~/.marc/marc.log"

[proxy]
listen_addr = "127.0.0.1:8082"
stripped_headers = ["authorization", "x-api-key", "cookie"]
# upstream_url is legacy — only used when no [profiles] block is defined,
# in which case it auto-synthesizes a single "anthropic" profile. With
# profiles defined below, this field is ignored and can be deleted.

# Profiles enable AWS-style provider switching:
#   marc --profile anthropic --continue
#   marc --profile minimax -p "..."
# Resolution order: --profile flag > MARC_PROFILE env > default_profile > "anthropic".
# Add more profiles by appending [profiles.<name>] sections.

[profiles.anthropic]
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"
auth_style  = "x-api-key"

# [profiles.minimax]
# base_url    = "https://api.minimax.chat/v1"
# api_key_env = "MINIMAX_API_KEY"
# auth_style  = "bearer"
# [profiles.minimax.header_overrides]
# "anthropic-version" = "2023-06-01"

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "https://artifacts.kanolab.io"
bucket = "marc"
access_key = "AKIA..."
secret_key = "MIGHTY..."
verify_tls = true
`

// validateClient checks required fields and returns a descriptive error.
func validateClient(cfg *ClientConfig) error {
	required := []struct {
		value string
		name  string
	}{
		{cfg.MachineName, "machine_name"},
		{cfg.MinIO.Endpoint, "minio.endpoint"},
		{cfg.MinIO.Bucket, "minio.bucket"},
		{cfg.MinIO.AccessKey, "minio.access_key"},
		{cfg.MinIO.SecretKey, "minio.secret_key"},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			return fmt.Errorf("config: required field %q is missing or empty", r.name)
		}
	}
	return nil
}

// checkMode returns an error if the file at path is not mode 0600.
func checkMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("config: %s has permissions %04o; must be 0600 (run: chmod 0600 %s)",
			path, info.Mode().Perm(), path)
	}
	return nil
}

// expandHome replaces a leading "~/" with the current user's home directory.
func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[2:]), nil
}

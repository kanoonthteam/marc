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

// ClientConfig is the top-level configuration for the marc client binary.
// It is loaded from ~/.marc/config.toml (mode 0600).
type ClientConfig struct {
	MachineName string        `toml:"machine_name"`
	Paths       ClientPaths   `toml:"paths"`
	Proxy       ClientProxy   `toml:"proxy"`
	Shipper     ClientShipper `toml:"shipper"`
	MinIO       ClientMinIO   `toml:"minio"`
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

[paths]
capture_file = "~/.marc/capture.jsonl"
log_file = "~/.marc/marc.log"

[proxy]
listen_addr = "127.0.0.1:8082"
upstream_url = "https://api.anthropic.com"
stripped_headers = ["authorization", "x-api-key", "cookie"]

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

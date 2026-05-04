package clauderun

import (
	"os"
	"strings"

	"github.com/caffeaun/marc/internal/config"
)

// ParseProfileFlag extracts the value of a --profile flag from args, returning
// the profile name (or "" if not present) and the args list with the flag and
// its value removed.
//
// Both forms are supported:
//
//	--profile minimax     (separate value)
//	--profile=minimax     (joined value)
//
// When --profile appears multiple times, the LAST occurrence wins (mirrors
// AWS CLI behaviour). When --profile is the last token with no value, it is
// treated as absent and left in args (the spawned binary, not marc, gets to
// complain about it).
func ParseProfileFlag(args []string) (profile string, remaining []string) {
	remaining = make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--profile":
			// Need a value after.
			if i+1 < len(args) {
				profile = args[i+1]
				i += 2
				continue
			}
			// No value — leave the flag in args; the user clearly meant
			// something but we won't guess.
			remaining = append(remaining, a)
			i++
		case strings.HasPrefix(a, "--profile="):
			profile = strings.TrimPrefix(a, "--profile=")
			i++
		default:
			remaining = append(remaining, a)
			i++
		}
	}
	return profile, remaining
}

// ResolveProfileName applies AWS-style precedence to determine which profile
// the operator wants:
//
//  1. Explicit --profile value (already parsed by ParseProfileFlag)
//  2. MARC_PROFILE environment variable
//  3. cfg.DefaultProfile
//  4. Hardcoded fallback "anthropic"
//
// The returned name is then looked up in cfg.Profiles by ClientConfig.ResolveProfile.
func ResolveProfileName(flagValue string, cfg *config.ClientConfig) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("MARC_PROFILE")); v != "" {
		return v
	}
	if cfg != nil && strings.TrimSpace(cfg.DefaultProfile) != "" {
		return cfg.DefaultProfile
	}
	return "anthropic"
}

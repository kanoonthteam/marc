package clauderun

import (
	"reflect"
	"testing"

	"github.com/caffeaun/marc/internal/config"
)

func TestParseProfileFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantProfile string
		wantRest    []string
	}{
		{
			name:        "absent",
			args:        []string{"--continue"},
			wantProfile: "",
			wantRest:    []string{"--continue"},
		},
		{
			name:        "separate value at start",
			args:        []string{"--profile", "minimax", "--continue"},
			wantProfile: "minimax",
			wantRest:    []string{"--continue"},
		},
		{
			name:        "separate value at end",
			args:        []string{"-p", "hello", "--profile", "minimax"},
			wantProfile: "minimax",
			wantRest:    []string{"-p", "hello"},
		},
		{
			name:        "joined value",
			args:        []string{"--profile=minimax", "--continue"},
			wantProfile: "minimax",
			wantRest:    []string{"--continue"},
		},
		{
			name:        "between other flags",
			args:        []string{"-p", "--profile", "minimax", "hello"},
			wantProfile: "minimax",
			wantRest:    []string{"-p", "hello"},
		},
		{
			name:        "duplicate — last wins",
			args:        []string{"--profile", "x", "--profile", "y"},
			wantProfile: "y",
			wantRest:    []string{},
		},
		{
			name:        "duplicate joined — last wins",
			args:        []string{"--profile=x", "--profile=y"},
			wantProfile: "y",
			wantRest:    []string{},
		},
		{
			name:        "trailing --profile with no value left in args",
			args:        []string{"--continue", "--profile"},
			wantProfile: "",
			wantRest:    []string{"--continue", "--profile"},
		},
		{
			name:        "empty input",
			args:        []string{},
			wantProfile: "",
			wantRest:    []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProfile, gotRest := ParseProfileFlag(tt.args)
			if gotProfile != tt.wantProfile {
				t.Errorf("profile = %q, want %q", gotProfile, tt.wantProfile)
			}
			if !reflect.DeepEqual(gotRest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", gotRest, tt.wantRest)
			}
		})
	}
}

func TestResolveProfileName(t *testing.T) {
	cfg := &config.ClientConfig{
		DefaultProfile: "anthropic",
	}

	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "envvalue")
		got := ResolveProfileName("flagvalue", cfg)
		if got != "flagvalue" {
			t.Errorf("got %q, want flagvalue", got)
		}
	})
	t.Run("env wins over default", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "envvalue")
		got := ResolveProfileName("", cfg)
		if got != "envvalue" {
			t.Errorf("got %q, want envvalue", got)
		}
	})
	t.Run("default when flag and env empty", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "")
		got := ResolveProfileName("", cfg)
		if got != "anthropic" {
			t.Errorf("got %q, want anthropic", got)
		}
	})
	t.Run("hardcoded anthropic when default empty", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "")
		empty := &config.ClientConfig{}
		got := ResolveProfileName("", empty)
		if got != "anthropic" {
			t.Errorf("got %q, want anthropic", got)
		}
	})
	t.Run("nil cfg falls back to anthropic", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "")
		got := ResolveProfileName("", nil)
		if got != "anthropic" {
			t.Errorf("got %q, want anthropic", got)
		}
	})
	t.Run("whitespace-only flag treated as absent", func(t *testing.T) {
		t.Setenv("MARC_PROFILE", "envvalue")
		got := ResolveProfileName("   ", cfg)
		if got != "envvalue" {
			t.Errorf("got %q, want envvalue", got)
		}
	})
}

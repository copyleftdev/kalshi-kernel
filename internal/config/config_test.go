package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsToPaperAndDropsCredentials(t *testing.T) {
	t.Setenv("KALSHI_KERNEL_MODE", "")
	t.Setenv("KALSHI_API_KEY_ID", "must-not-escape")
	t.Setenv("KALSHI_PRIVATE_KEY_PATH", "/must/not/be/read")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Mode != ModePaper {
		t.Fatalf("Mode = %q, want %q", config.Mode, ModePaper)
	}
	if config.APIKeyID != "" || config.PrivateKeyPath != "" {
		t.Fatal("paper mode retained live credentials")
	}
}

func TestLoadRejectsUnarmedLiveMode(t *testing.T) {
	t.Setenv("KALSHI_KERNEL_MODE", "live")
	t.Setenv("KALSHI_API_KEY_ID", "key")
	t.Setenv("KALSHI_PRIVATE_KEY_PATH", "/key.pem")
	t.Setenv("KALSHI_LIVE_TRADING_ACK", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted live mode without explicit acknowledgement")
	}
}

func TestLoadAcceptsArmedLiveMode(t *testing.T) {
	t.Setenv("KALSHI_KERNEL_MODE", "live")
	t.Setenv("KALSHI_API_KEY_ID", "key")
	t.Setenv("KALSHI_PRIVATE_KEY_PATH", "/key.pem")
	t.Setenv("KALSHI_LIVE_TRADING_ACK", liveAcknowledgement)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Mode != ModeLive {
		t.Fatalf("Mode = %q, want %q", config.Mode, ModeLive)
	}
}

func TestLoadRejectsInvalidModes(t *testing.T) {
	for _, mode := range []string{"demo", "sandbox", "dry-run", "production", "paper-live"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("KALSHI_KERNEL_MODE", mode)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted invalid mode %q", mode)
			}
		})
	}
}

func TestLoadNormalizesModeButNotLiveAcknowledgement(t *testing.T) {
	t.Setenv("KALSHI_KERNEL_MODE", " LIVE ")
	t.Setenv("KALSHI_API_KEY_ID", " key ")
	t.Setenv("KALSHI_PRIVATE_KEY_PATH", " /key.pem ")
	t.Setenv("KALSHI_LIVE_TRADING_ACK", " "+liveAcknowledgement+" ")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-exact live acknowledgement")
	}
}

func TestLoadRequiresEachLiveCredential(t *testing.T) {
	tests := []struct {
		name    string
		keyID   string
		keyPath string
	}{
		{name: "missing key id", keyPath: "/key.pem"},
		{name: "missing private key path", keyID: "key"},
		{name: "missing both"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KALSHI_KERNEL_MODE", "live")
			t.Setenv("KALSHI_API_KEY_ID", test.keyID)
			t.Setenv("KALSHI_PRIVATE_KEY_PATH", test.keyPath)
			t.Setenv("KALSHI_LIVE_TRADING_ACK", liveAcknowledgement)
			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted incomplete live credentials")
			}
		})
	}
}

func FuzzLoadNeverEnablesUnexpectedMode(f *testing.F) {
	for _, mode := range []string{"", "paper", "live", " LIVE ", "sandbox", "production", "\x00live"} {
		f.Add(mode)
	}
	f.Fuzz(func(t *testing.T, mode string) {
		values := map[string]string{
			"KALSHI_KERNEL_MODE":      mode,
			"KALSHI_API_KEY_ID":       "key",
			"KALSHI_PRIVATE_KEY_PATH": "/key.pem",
			"KALSHI_LIVE_TRADING_ACK": liveAcknowledgement,
		}
		config, err := load(func(name string) string { return values[name] })
		normalized := strings.ToLower(strings.TrimSpace(mode))
		if err == nil && normalized != "" && normalized != string(ModePaper) && normalized != string(ModeLive) {
			t.Fatalf("arbitrary mode %q enabled execution as %q", mode, config.Mode)
		}
		if err == nil && config.Mode == ModeLive && normalized != string(ModeLive) {
			t.Fatalf("mode %q unexpectedly enabled live execution", mode)
		}
	})
}

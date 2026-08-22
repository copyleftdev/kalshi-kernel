package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Mode string

const (
	ModePaper Mode = "paper"
	ModeLive  Mode = "live"
)

const liveAcknowledgement = "I_UNDERSTAND_THIS_TRADES_REAL_MONEY"

type Config struct {
	Mode           Mode
	APIKeyID       string
	PrivateKeyPath string
}

func Load() (Config, error) {
	return load(os.Getenv)
}

func load(lookup func(string) string) (Config, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(lookup("KALSHI_KERNEL_MODE"))))
	if mode == "" {
		mode = ModePaper
	}

	config := Config{
		Mode:           mode,
		APIKeyID:       strings.TrimSpace(lookup("KALSHI_API_KEY_ID")),
		PrivateKeyPath: strings.TrimSpace(lookup("KALSHI_PRIVATE_KEY_PATH")),
	}

	switch mode {
	case ModePaper:
		// Production write credentials are intentionally ignored in paper mode.
		config.APIKeyID = ""
		config.PrivateKeyPath = ""
		return config, nil
	case ModeLive:
		if lookup("KALSHI_LIVE_TRADING_ACK") != liveAcknowledgement {
			return Config{}, fmt.Errorf("live mode requires KALSHI_LIVE_TRADING_ACK=%s", liveAcknowledgement)
		}
		if config.APIKeyID == "" || config.PrivateKeyPath == "" {
			return Config{}, errors.New("live mode requires KALSHI_API_KEY_ID and KALSHI_PRIVATE_KEY_PATH")
		}
		return config, nil
	default:
		return Config{}, fmt.Errorf("invalid KALSHI_KERNEL_MODE %q: expected paper or live", mode)
	}
}

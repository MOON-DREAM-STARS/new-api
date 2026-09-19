// Command workspace-guard is the only Chrome DevTools Protocol consumer of one
// Web Workspace runtime. It runs until the CDP channel fails and then exits non
// zero; the runtime entrypoint treats that exit as fatal and stops Chromium, so
// a browser can never stay usable without its guard.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/QuantumNous/new-api/browser-agent/internal/guard"
	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("workspace guard stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	mode, ok := policy.ParseMode(os.Getenv("WW_GUARD_MODE"))
	if !ok {
		return errors.New("invalid WW_GUARD_MODE: expected LOCKED or LOGIN")
	}
	cdpURL := strings.TrimSpace(os.Getenv("WW_CDP_URL"))
	if cdpURL == "" {
		cdpURL = guard.DefaultCDPURL
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return guard.Run(ctx, guard.Config{
		CDPURL:   cdpURL,
		Mode:     mode,
		Logger:   logger,
		StartURL: strings.TrimSpace(os.Getenv("WW_START_URL")),
	})
}

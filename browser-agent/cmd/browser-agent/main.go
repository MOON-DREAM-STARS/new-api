// Command browser-agent runs the private Web Workspace execution plane. It has
// no public listener of its own: the deployment is responsible for attaching it
// to the private network only, and every request needs the service token.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/config"
	"github.com/QuantumNous/new-api/browser-agent/internal/httpapi"
	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime/docker"
)

const (
	readHeaderTimeout = 10 * time.Second
	reconcileTimeout  = 30 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("browser agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	driver := docker.NewDriver(docker.NewClient(cfg.DockerSocketPath), docker.Options{
		Image:       cfg.RuntimeImage,
		Network:     cfg.RuntimeNetwork,
		MemoryBytes: cfg.MemoryBytes,
		NanoCPUs:    cfg.NanoCPUs,
		PidsLimit:   cfg.PidsLimit,
	})
	mgr := manager.New(driver, driver, manager.Options{
		DataRoot:     cfg.DataRoot,
		HostDataRoot: cfg.HostDataRoot,
		IdleTimeout:  cfg.IdleTimeout,
		ScanInterval: cfg.IdleScanInterval,
		Logger:       logger,
	})

	// Adopt running runtime containers before serving traffic, and remove the
	// orphaned containers an agent crash may have left behind.
	reconcileCtx, cancelReconcile := context.WithTimeout(ctx, reconcileTimeout)
	err = mgr.Reconcile(reconcileCtx)
	cancelReconcile()
	if err != nil {
		return fmt.Errorf("reconcile runtime containers: %w", err)
	}

	go mgr.Supervise(ctx)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           httpapi.New(mgr, cfg.Token, logger),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()
	logger.Info("browser agent listening", "listen", cfg.Listen)

	select {
	case <-ctx.Done():
		// Runtime containers keep running across an agent restart; they are
		// reconciled again on the next start.
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}

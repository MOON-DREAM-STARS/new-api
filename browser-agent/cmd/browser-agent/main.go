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
	"github.com/QuantumNous/new-api/browser-agent/internal/egress"
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

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	driver := docker.NewDriver(docker.NewClient(cfg.DockerSocketPath), docker.Options{
		Image:       cfg.RuntimeImage,
		Network:     cfg.RuntimeNetwork,
		MemoryBytes: cfg.MemoryBytes,
		NanoCPUs:    cfg.NanoCPUs,
		PidsLimit:   cfg.PidsLimit,
	})

	// The runtime network must exist and be internal before any container is
	// adopted or started. A non-internal network is a fatal fail-open condition.
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, reconcileTimeout)
	err = driver.EnsureRuntimeNetwork(preflightCtx)
	cancelPreflight()
	if err != nil {
		return fmt.Errorf("ensure runtime network: %w", err)
	}

	mgr := manager.New(driver, driver, manager.Options{
		DataRoot:          cfg.DataRoot,
		HostDataRoot:      cfg.HostDataRoot,
		EgressProxyURL:    cfg.EgressProxyURL,
		IdleTimeout:       cfg.IdleTimeout,
		ScanInterval:      cfg.IdleScanInterval,
		MaxActiveRuntimes: cfg.MaxActiveRuntimes,
		Logger:            logger,
	})

	proxy := egress.New(egress.Options{
		Listen: cfg.EgressProxyListen,
		Logger: logger,
		Lookup: mgr.LookupSource,
	})
	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- proxy.ListenAndServe()
	}()
	logger.Info("starting egress proxy", "listen", cfg.EgressProxyListen)
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		_ = proxy.Shutdown(shutdownCtx)
	}()

	// Adopt running runtime containers before serving traffic, and remove the
	// orphaned containers an agent crash may have left behind. The egress
	// index is populated during reconcile, so requests from adopted runtimes
	// are denied until their source address is known.
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

	stop := func() error {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		if err := proxy.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown egress proxy: %w", err)
		}
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown browser agent http server: %w", err)
		}
		return nil
	}

	select {
	case <-ctx.Done():
		return stop()
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = stop()
			return err
		}
		if err == nil {
			err = errors.New("browser agent http server stopped unexpectedly")
		}
		if stopErr := stop(); stopErr != nil {
			return stopErr
		}
		return fmt.Errorf("browser agent http server: %w", err)
	case err := <-proxyErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = stop()
			return fmt.Errorf("egress proxy: %w", err)
		}
		if err == nil {
			err = errors.New("egress proxy stopped unexpectedly")
		}
		if stopErr := stop(); stopErr != nil {
			return stopErr
		}
		return fmt.Errorf("egress proxy: %w", err)
	}
}

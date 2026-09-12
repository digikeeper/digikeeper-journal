package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	command "github.com/digikeeper/digikeeper-journal/internal/domain/command/append"
	domainCandidate "github.com/digikeeper/digikeeper-journal/internal/domain/command/candidate"
	domainCompaction "github.com/digikeeper/digikeeper-journal/internal/domain/command/compaction"
	"github.com/digikeeper/digikeeper-journal/internal/domain/query"
	apicmd "github.com/digikeeper/digikeeper-journal/internal/httpapi/command"
	apiqry "github.com/digikeeper/digikeeper-journal/internal/httpapi/query"
	apisreg "github.com/digikeeper/digikeeper-journal/internal/httpapi/schemaregistry"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/candidatestore"
	store "github.com/digikeeper/digikeeper-journal/internal/infrastructure/commandstore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/index"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/querystore"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/sourcerepo"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cfg := configure()

	level := slog.LevelInfo
	if cfg.IsDevEnv() {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Storage
	dataPath := cfg.JournalStorage.Path
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dataPath, err)
	}

	idx, err := index.New(filepath.Join(dataPath, "index.db"), index.Config{
		JournalMode: cfg.SQLite.JournalMode,
		BusyTimeout: cfg.SQLite.BusyTimeout,
	})
	if err != nil {
		return fmt.Errorf("init index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	journalStore, err := store.NewStore(dataPath, idx)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	defer func() { _ = journalStore.Close() }()

	candidateStore, err := candidatestore.New(dataPath)
	if err != nil {
		return fmt.Errorf("init candidate storage: %w", err)
	}

	qryStore := querystore.NewStore(filepath.Join(dataPath, "dk_journal"), idx)

	// Sources
	srcRepo, err := sourcerepo.New()
	if err != nil {
		return fmt.Errorf("init sources: %w", err)
	}

	// Services
	cmdSvc := command.NewService(journalStore, srcRepo, logger)
	candidateSvc := domainCandidate.NewService(
		candidateStore, journalStore, logger,
	)
	compactionSvc := domainCompaction.NewService(
		journalStore, candidateStore, idx, logger,
	)
	qrySvc := query.NewService(qryStore, qryStore, logger)

	// Handlers
	cmdHandler := apicmd.NewHandler(cmdSvc, srcRepo.ResolveName)
	candidateHandler := apicmd.NewCandidateHandler(candidateSvc)
	compactionHandler := apicmd.NewCompactionHandler(compactionSvc)
	qryHandler := apiqry.NewHandler(qrySvc, srcRepo.ResolveName)
	sregHandler, err := apisreg.NewHandler()
	if err != nil {
		return fmt.Errorf("init schema registry: %w", err)
	}

	// API
	handler := newHTTPHandler(cfg, logger, handlers{
		Command:    cmdHandler,
		Candidate:  candidateHandler,
		Compaction: compactionHandler,
		Query:      qryHandler,
		Schema:     sregHandler,
	})

	addr := fmt.Sprintf("%s:%s", cfg.API.Host, cfg.API.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  cfg.API.Timeout,
		WriteTimeout: cfg.API.Timeout,
		IdleTimeout:  2 * cfg.API.Timeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting",
			slog.String("addr", addr),
			slog.String("data_path", dataPath),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		stop()
		logger.Info("shutdown signal received", slog.String("cause", context.Cause(ctx).Error()))
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
	}

	logger.Info("server stopped")
	return nil
}

// Command dkbackup dumps the journal's JSONL source of truth to a remote,
// restores it, and rebuilds the derived SQLite index.
//
//	dkbackup push      upload everything changed since the remote's last dump
//	dkbackup restore   materialise the remote's last dump into the data dir
//	dkbackup verify    parse the local JSONL tree, optionally repairing tears
//	dkbackup reindex   rebuild index.db from the JSONL files (server stopped)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "dkbackup:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return errors.New("missing command")
	}

	command, args := os.Args[1], os.Args[2:]
	if command == "help" || command == "-h" || command == "--help" {
		usage()
		return nil
	}
	if command != "push" && command != "restore" && command != "verify" && command != "reindex" {
		usage()
		return fmt.Errorf("unknown command %q", command)
	}

	cfg, err := configure()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch command {
	case "push":
		return runPush(ctx, args, cfg)
	case "restore":
		return runRestore(ctx, args, cfg)
	case "verify":
		return runVerify(args, cfg)
	default: // reindex
		return runReindex(ctx, args, cfg)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `dkbackup — journal backup and index maintenance

usage: dkbackup <command> [flags]

commands:
  push      upload files changed since the remote's last dump, then the manifest
  restore   download the remote's last dump into the data directory
  verify    parse the local JSONL tree; -repair drops truncated trailing lines
  reindex   rebuild index.db from the JSONL files; requires the server stopped

run "dkbackup <command> -h" for the flags of one command.
`)
}

func logger(verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
	"github.com/digikeeper/digikeeper-journal/internal/backup/remote"
)

type journalStorageConfig struct {
	Path string `env:"PATH" env-default:"/var/lib/digikeeper"`
}

type sqliteConfig struct {
	JournalMode string        `env:"JOURNAL_MODE" env-default:"WAL"`
	BusyTimeout time.Duration `env:"BUSY_TIMEOUT" env-default:"5s"`
}

type backupConfig struct {
	Remote   string        `env:"REMOTE"`
	User     string        `env:"USER"`
	Password string        `env:"PASSWORD"`
	Timeout  time.Duration `env:"TIMEOUT" env-default:"2m"`
	Verbose  bool          `env:"VERBOSE" env-default:"false"`
}

type config struct {
	JournalStorage journalStorageConfig `env-prefix:"JOURNAL_STORAGE_"`
	SQLite         sqliteConfig         `env-prefix:"SQLITE_"`
	Backup         backupConfig         `env-prefix:"BACKUP_"`
}

func configure() (config, error) {
	var cfg config
	if os.Getenv("DIGIKEEPER_LOAD_DOTENV") == "true" {
		if err := cleanenv.ReadConfig(".env", &cfg); err != nil {
			return config{}, fmt.Errorf("load .env: %w", err)
		}
	}
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return config{}, fmt.Errorf("read environment: %w", err)
	}
	return cfg, nil
}

func dataFlag(fs *flag.FlagSet, fallback string) *string {
	return fs.String("data", fallback, "data directory (env JOURNAL_STORAGE_PATH)")
}

func verboseFlag(fs *flag.FlagSet, fallback bool) *bool {
	return fs.Bool("v", fallback, "log every file, not just failures (env BACKUP_VERBOSE)")
}

func remoteFlag(fs *flag.FlagSet, fallback string) *string {
	return fs.String("remote", fallback,
		"destination: dir:<path>, or a WebDAV URL (env BACKUP_REMOTE)")
}

// openRemote constructs the configured backend. Credentials stay in the
// environment-backed config rather than command-line flags and process lists.
func openRemote(spec string, cfg backupConfig) (backup.Remote, error) {
	switch {
	case spec == "":
		return nil, fmt.Errorf("no remote configured: pass -remote or set BACKUP_REMOTE")

	case strings.HasPrefix(spec, "dir:"):
		return remote.NewLocal(strings.TrimPrefix(spec, "dir:"))

	case strings.HasPrefix(spec, "http://"), strings.HasPrefix(spec, "https://"):
		if cfg.User == "" || cfg.Password == "" {
			return nil, fmt.Errorf("WebDAV remote needs BACKUP_USER and BACKUP_PASSWORD")
		}
		return remote.NewWebDAV(remote.WebDAVConfig{
			BaseURL:  spec,
			User:     cfg.User,
			Password: cfg.Password,
			Timeout:  cfg.Timeout,
		})

	default:
		return nil, fmt.Errorf("unsupported remote %q: want dir:<path> or an http(s) WebDAV URL", spec)
	}
}

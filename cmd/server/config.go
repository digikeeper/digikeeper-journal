package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type CommonConfig struct {
	EnvName string `env:"ENVIRONMENT_NAME" env-default:"dev"`
}

type APIConfig struct {
	Timeout time.Duration `env:"TIMEOUT" env-default:"5s"`
	Port    string        `env:"PORT" env-default:"9000"`
	Host    string        `env:"HOST" env-required:"true"`
}

type JournalStorageConfig struct {
	Path string `env:"PATH" env-default:"/var/lib/digikeeper"`
}

type SQLiteConfig struct {
	JournalMode string        `env:"JOURNAL_MODE" env-default:"WAL"`
	BusyTimeout time.Duration `env:"BUSY_TIMEOUT" env-default:"5s"`
}

type DebugConfig struct {
	Enabled bool `env:"ENABLED" env-default:"false"`
}

type Config struct {
	Common         CommonConfig         `yaml:"common"`
	JournalStorage JournalStorageConfig `yaml:"journal_storage" env-prefix:"JOURNAL_STORAGE_"`
	SQLite         SQLiteConfig         `yaml:"sqlite" env-prefix:"SQLITE_"`
	API            APIConfig            `yaml:"api" env-prefix:"API_"`
	Debug          DebugConfig          `yaml:"debug" env-prefix:"DEBUG_"`
}

func (c *Config) IsDevEnv() bool {
	return strings.HasPrefix(strings.ToLower(c.Common.EnvName), "dev")
}

func configure() Config {
	var cfg Config

	if os.Getenv("DIGIKEEPER_LOAD_DOTENV") == "true" {
		if err := cleanenv.ReadConfig(".env", &cfg); err != nil {
			log.Printf("No .env loaded: %v", err)
		}
	}

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(fmt.Sprintf("failed to fetch env vars: %v", err))
	}

	return cfg
}

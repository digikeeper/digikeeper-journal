package schemaregistry

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed default_schemas/note/v1/*
var defaultSchemas embed.FS

// SeedDefaultNote writes the project-owned note/v1 bundle to a newly created
// schema directory. Call it only when the storage root did not exist before
// startup; it never replaces a user-owned file.
func SeedDefaultNote(schemaDir string) error {
	bundle, err := fs.Sub(defaultSchemas, "default_schemas/note/v1")
	if err != nil {
		return fmt.Errorf("open embedded default note: %w", err)
	}
	for _, name := range []string{"schema.json", "instructions.md"} {
		contents, err := fs.ReadFile(bundle, name)
		if err != nil {
			return fmt.Errorf("read embedded default note %s: %w", name, err)
		}
		destination := filepath.Join(schemaDir, "note", "v1", name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create default note directory: %w", err)
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("write default note %s: %w", name, err)
		}
		if _, err := file.Write(contents); err != nil {
			_ = file.Close()
			return fmt.Errorf("write default note %s: %w", name, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close default note %s: %w", name, err)
		}
	}
	return nil
}

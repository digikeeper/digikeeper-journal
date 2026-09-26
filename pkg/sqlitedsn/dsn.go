package sqlitedsn

import (
	"fmt"
	"net/url"
	"strings"
)

// Param is a supported modernc SQLite DSN query parameter.
type Param string

const (
	// ParamPragma runs the option value as a per-connection PRAGMA.
	ParamPragma Param = "_pragma"
)

// PragmaKey is a supported SQLite PRAGMA name.
type PragmaKey string

const (
	// PragmaJournalMode configures SQLite journal mode.
	PragmaJournalMode PragmaKey = "journal_mode"
	// PragmaBusyTimeout sets how long SQLite waits for a locked database before
	// returning SQLITE_BUSY. The value is in milliseconds, per connection.
	PragmaBusyTimeout PragmaKey = "busy_timeout"
	// PragmaForeignKeys configures SQLite foreign key enforcement.
	PragmaForeignKeys PragmaKey = "foreign_keys"
)

// Option configures a modernc SQLite DSN.
type Option func(url.Values)

// ParamValue adds one query parameter value to the DSN.
func ParamValue(param Param, value string) Option {
	return func(query url.Values) {
		query.Add(string(param), value)
	}
}

// Pragma adds a per-connection PRAGMA to the DSN.
func Pragma(name PragmaKey, args ...any) Option {
	if len(args) == 0 {
		return ParamValue(ParamPragma, string(name))
	}

	values := make([]string, len(args))
	for i, arg := range args {
		values[i] = fmt.Sprint(arg)
	}
	return ParamValue(ParamPragma, fmt.Sprintf("%s(%s)", name, strings.Join(values, ",")))
}

// File builds a modernc SQLite file DSN.
func File(path string, opts ...Option) string {
	query := url.Values{}
	for _, opt := range opts {
		opt(query)
	}

	dsn := url.URL{
		Scheme: "file",
		Path:   path,
	}
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

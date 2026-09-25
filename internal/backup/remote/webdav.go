package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/digikeeper/digikeeper-journal/internal/backup"
)

// DefaultTimeout bounds a single WebDAV request.
const DefaultTimeout = 2 * time.Minute

// WebDAV stores a dump on any WebDAV server: Yandex Disk
// (https://webdav.yandex.ru, with an app password), Nextcloud, pCloud, Box.
type WebDAV struct {
	base     *url.URL
	user     string
	password string
	client   *http.Client

	mkcolOnce sync.Map // collection path → struct{}, so each dir is created once
}

// WebDAVConfig configures a WebDAV remote.
type WebDAVConfig struct {
	// BaseURL is the directory the dump lives in, e.g.
	// https://webdav.yandex.ru/digikeeper.
	BaseURL  string
	User     string
	Password string
	Timeout  time.Duration
}

// NewWebDAV builds a WebDAV remote.
func NewWebDAV(cfg WebDAVConfig) (*WebDAV, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("remote: parse webdav url: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("remote: webdav url needs http(s) scheme, got %q", base.Scheme)
	}
	if cfg.User == "" || cfg.Password == "" {
		return nil, errors.New("remote: webdav needs a user and password")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}

	base.Path = strings.TrimSuffix(base.Path, "/")
	return &WebDAV{
		base:     base,
		user:     cfg.User,
		password: cfg.Password,
		client:   &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (w *WebDAV) String() string { return w.base.String() }

func (w *WebDAV) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := w.do(ctx, http.MethodGet, key, nil, 0)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("remote: %s: %w", key, backup.ErrNotExist)
	case resp.StatusCode >= 300:
		return nil, w.statusErr(resp, "GET", key)
	}
	return resp.Body, nil
}

func (w *WebDAV) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := w.mkcolAll(ctx, path.Dir(key)); err != nil {
		return err
	}

	resp, err := w.do(ctx, http.MethodPut, key, r, size)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return w.statusErr(resp, "PUT", key)
	}
	return nil
}

// mkcolAll creates every missing collection along dir. WebDAV has no recursive
// MKCOL, so each segment is created in turn; servers answer 405 for one that
// already exists.
func (w *WebDAV) mkcolAll(ctx context.Context, dir string) error {
	if dir == "." || dir == "/" || dir == "" {
		return nil
	}

	var built string
	for segment := range strings.SplitSeq(strings.Trim(dir, "/"), "/") {
		built = path.Join(built, segment)
		if _, done := w.mkcolOnce.Load(built); done {
			continue
		}

		resp, err := w.do(ctx, "MKCOL", built, nil, 0)
		if err != nil {
			return err
		}
		status := resp.StatusCode
		_ = resp.Body.Close()

		switch {
		case status == http.StatusMethodNotAllowed, status == http.StatusConflict:
			// Already a collection (405), or the parent is still being created
			// by a server that reports 409 — either way the next PUT tells us.
		case status >= 300:
			return fmt.Errorf("remote: MKCOL %s: unexpected status %s", built, resp.Status)
		}
		w.mkcolOnce.Store(built, struct{}{})
	}
	return nil
}

func (w *WebDAV) do(ctx context.Context, method, key string, body io.Reader, size int64) (*http.Response, error) {
	target := *w.base
	target.Path = path.Join(w.base.Path, key)

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("remote: %s %s: %w", method, key, err)
	}
	req.SetBasicAuth(w.user, w.password)
	if size > 0 {
		req.ContentLength = size
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: %s %s: %w", method, key, err)
	}
	return resp, nil
}

// statusErr drains and closes the body, then describes the failure.
func (w *WebDAV) statusErr(resp *http.Response, method, key string) error {
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	_ = resp.Body.Close()

	msg := strings.TrimSpace(string(detail))
	if msg == "" {
		return fmt.Errorf("remote: %s %s: %s", method, key, resp.Status)
	}
	return fmt.Errorf("remote: %s %s: %s: %s", method, key, resp.Status, msg)
}

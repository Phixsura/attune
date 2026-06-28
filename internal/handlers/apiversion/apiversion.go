// SPDX-License-Identifier: Apache-2.0

// Package apiversion enforces attune's date-pinned public API contract on the
// API-key product surface. The URL path stays at /v1 while callers may pin a
// specific compatible contract via X-Attune-Api-Version.
package apiversion

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

const (
	// HeaderName is the request/response header used to pin attune's public API
	// contract without changing the stable /v1 path shape.
	HeaderName = "X-Attune-Api-Version"
	// DeprecationHeader is emitted when a pinned version is deprecated.
	DeprecationHeader = "Deprecation"
	// SunsetHeader is emitted when a pinned version has a scheduled removal date.
	SunsetHeader = "Sunset"
	// CurrentVersion is the current date-based attune public API contract.
	CurrentVersion = "2026-06-28"
)

// DeprecationNotice describes a deprecated-but-still-supported API version.
type DeprecationNotice struct {
	Deprecation string
	Sunset      string
}

// Config controls the supported public API versions.
type Config struct {
	CurrentVersion    string
	SupportedVersions []string
	Deprecations      map[string]DeprecationNotice
}

// DefaultConfig returns the currently supported public API contract.
func DefaultConfig() Config {
	return Config{
		CurrentVersion:    CurrentVersion,
		SupportedVersions: []string{CurrentVersion},
	}
}

// Middleware validates the optional X-Attune-Api-Version request header,
// echoes the effective version on the response, and emits deprecation metadata
// for still-supported older contracts.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	cfg = normalizeConfig(cfg)
	supportedSet := make(map[string]struct{}, len(cfg.SupportedVersions))
	for _, version := range cfg.SupportedVersions {
		supportedSet[version] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			version, msg := resolveVersion(r.Header, cfg.CurrentVersion, cfg.SupportedVersions, supportedSet)
			if msg != "" {
				appendVary(w.Header(), HeaderName)
				w.Header().Set(HeaderName, cfg.CurrentVersion)
				dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, msg)
				return
			}

			writer := ptrext.Of(contractWriter{
				ResponseWriter: w,
				version:        version,
				notice:         cfg.Deprecations[version],
			})
			writer.ensureContractHeaders()
			next.ServeHTTP(writer, r)
			if !writer.wrote {
				writer.ensureContractHeaders()
			}
		})
	}
}

type contractWriter struct {
	http.ResponseWriter
	version string
	notice  DeprecationNotice
	wrote   bool
}

func (w *contractWriter) WriteHeader(statusCode int) {
	w.beginWrite()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *contractWriter) Write(p []byte) (int, error) {
	w.beginWrite()
	return w.ResponseWriter.Write(p)
}

func (w *contractWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	w.beginWrite()
	flusher.Flush()
}

func (w *contractWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *contractWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *contractWriter) ReadFrom(r io.Reader) (int64, error) {
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(writerOnly{Writer: w}, r)
	}
	w.beginWrite()
	return readerFrom.ReadFrom(r)
}

func (w *contractWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *contractWriter) beginWrite() {
	if w.wrote {
		return
	}
	w.ensureContractHeaders()
	w.wrote = true
}

type writerOnly struct {
	io.Writer
}

func (w *contractWriter) ensureContractHeaders() {
	h := w.Header()
	appendVary(h, HeaderName)
	h.Set(HeaderName, w.version)
	if w.notice.Deprecation != "" {
		h.Set(DeprecationHeader, w.notice.Deprecation)
	} else {
		h.Del(DeprecationHeader)
	}
	if w.notice.Sunset != "" {
		h.Set(SunsetHeader, w.notice.Sunset)
	} else {
		h.Del(SunsetHeader)
	}
}

func normalizeConfig(cfg Config) Config {
	current := strings.TrimSpace(cfg.CurrentVersion)
	if current == "" {
		current = CurrentVersion
	}

	seen := map[string]struct{}{current: {}}
	supported := []string{current}
	for _, version := range cfg.SupportedVersions {
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		if _, ok := seen[version]; ok {
			continue
		}
		seen[version] = struct{}{}
		supported = append(supported, version)
	}

	cfg.CurrentVersion = current
	cfg.SupportedVersions = supported
	if cfg.Deprecations == nil {
		cfg.Deprecations = map[string]DeprecationNotice{}
	}
	return cfg
}

func resolveVersion(
	headers http.Header,
	current string,
	supported []string,
	supportedSet map[string]struct{},
) (string, string) {
	values := headers.Values(HeaderName)
	if len(values) == 0 {
		return current, ""
	}
	if len(values) > 1 {
		return "", fmt.Sprintf("%s must appear at most once", HeaderName)
	}

	raw := strings.TrimSpace(values[0])
	if raw == "" {
		return "", fmt.Sprintf("%s must not be empty", HeaderName)
	}
	if strings.Contains(raw, ",") {
		return "", fmt.Sprintf("%s must contain exactly one version", HeaderName)
	}
	if _, ok := supportedSet[raw]; !ok {
		return "", fmt.Sprintf("unsupported %s %q; supported values: %s", HeaderName, raw, strings.Join(supported, ", "))
	}
	return raw, ""
}

func appendVary(h http.Header, name string) {
	for _, existing := range h.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), name) {
				return
			}
		}
	}
	h.Add("Vary", name)
}

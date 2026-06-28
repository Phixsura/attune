package main

import (
	"net/http"

	handlercors "github.com/Phixsura/attune/internal/handlers/cors"
	"github.com/Phixsura/attune/internal/infra/config"
)

func buildPublicIngestCORSConfig(cfg *config.Config) (handlercors.Config, bool) {
	if cfg == nil || len(cfg.IngestCORSAllowedOrigins) == 0 {
		return handlercors.Config{}, false
	}
	corsCfg := handlercors.DefaultConfig()
	corsCfg.AllowedOrigins = append([]string(nil), cfg.IngestCORSAllowedOrigins...)
	return corsCfg, true
}

func publicIngestCORS(cfg *config.Config) func(http.Handler) http.Handler {
	corsCfg, ok := buildPublicIngestCORSConfig(cfg)
	if !ok {
		return nil
	}
	return handlercors.Middleware(corsCfg)
}

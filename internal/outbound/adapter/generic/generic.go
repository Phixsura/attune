// SPDX-License-Identifier: Apache-2.0

// Package generic is the raw-webhook delivery adapter. It POSTs the v2
// envelope JSON to a customer URL with HMAC-SHA256 signing.
package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "raw-webhook"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("marshal envelope: %w", err)
	}

	var signature string
	switch dst.SignatureVersion {
	case outbound.SignatureVersionBytes, "":
		signature = outbound.BytesSign(body, dst.Secret)
	default:
		sig, err := outbound.ContentHashSign(env, dst.Secret)
		if err != nil {
			return outbound.Rendered{}, fmt.Errorf("content-hash sign: %w", err)
		}
		signature = sig
	}

	label := fmt.Sprintf("generic-%s", dst.TenantID)
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("X-Attune-Signature", signature)
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.generic] upstream req,label:%s,url:%s,body:%s",
				label, dst.URL, truncate(string(body), 1024))
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

func (c *channel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	body, err := json.Marshal(view)
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("marshal digest view: %w", err)
	}

	signature := outbound.BytesSign(body, dst.Secret)
	label := fmt.Sprintf("digest-%s", dst.TenantID)

	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("X-Attune-Signature", signature)
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.generic] digest req,label:%s,url:%s", label, dst.URL)
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

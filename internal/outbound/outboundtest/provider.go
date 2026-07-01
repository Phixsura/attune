// SPDX-License-Identifier: Apache-2.0

package outboundtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ProviderResponse is one fake upstream response. When a fake provider receives
// more requests than responses, it repeats the final response.
type ProviderResponse struct {
	Status  int
	Body    string
	Headers map[string]string
}

// ProviderRequest is the request captured by a fake upstream provider.
type ProviderRequest struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     []byte
}

// BodyString returns the captured request body as text.
func (r ProviderRequest) BodyString() string {
	return string(r.Body)
}

// ProviderScenario configures one fake upstream provider profile.
type ProviderScenario struct {
	Name      string
	Responses []ProviderResponse
	Assert    func(t *testing.T, req ProviderRequest)
}

// FakeProvider is an httptest-backed upstream that records every request and
// replays a provider-shaped response sequence.
type FakeProvider struct {
	t        *testing.T
	server   *httptest.Server
	scenario ProviderScenario
	mu       sync.Mutex
	requests []ProviderRequest
}

// NewProvider starts a fake upstream provider for one scenario.
func NewProvider(t *testing.T, scenario ProviderScenario) *FakeProvider {
	t.Helper()
	if len(scenario.Responses) == 0 {
		scenario.Responses = []ProviderResponse{{Status: http.StatusOK}}
	}
	provider := ptrext.Of(FakeProvider{t: t, scenario: scenario})
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.server.Close)
	return provider
}

// URL returns the fake provider URL with the supplied path fragment appended.
func (p *FakeProvider) URL(pathFragment string) string {
	if pathFragment == "" || pathFragment == "/" {
		return p.server.URL
	}
	if strings.HasPrefix(pathFragment, "/") {
		return p.server.URL + pathFragment
	}
	return p.server.URL + "/" + pathFragment
}

// Requests returns a snapshot of all captured requests.
func (p *FakeProvider) Requests() []ProviderRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]ProviderRequest, len(p.requests))
	for i, req := range p.requests {
		out[i] = req.clone()
	}
	return out
}

// CallCount returns the number of captured requests.
func (p *FakeProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *FakeProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.t.Errorf("read fake provider request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	req := ProviderRequest{
		Method:   r.Method,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
		Header:   r.Header.Clone(),
		Body:     append([]byte(nil), body...),
	}
	call := p.record(req)
	if p.scenario.Assert != nil {
		p.scenario.Assert(p.t, req.clone())
	}

	resp := p.response(call)
	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if resp.Body != "" {
		if _, err := w.Write([]byte(resp.Body)); err != nil {
			p.t.Errorf("write fake provider response body: %v", err)
		}
	}
}

func (p *FakeProvider) record(req ProviderRequest) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req.clone())
	return len(p.requests)
}

func (p *FakeProvider) response(call int) ProviderResponse {
	idx := call - 1
	if idx >= len(p.scenario.Responses) {
		idx = len(p.scenario.Responses) - 1
	}
	return p.scenario.Responses[idx]
}

func (r ProviderRequest) clone() ProviderRequest {
	return ProviderRequest{
		Method:   r.Method,
		Path:     r.Path,
		RawQuery: r.RawQuery,
		Header:   r.Header.Clone(),
		Body:     append([]byte(nil), r.Body...),
	}
}

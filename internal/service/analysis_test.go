package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"web-scrapper/internal/cache"
	"web-scrapper/internal/middleware"
	"web-scrapper/internal/security"
)

type fakeResolver struct {
	hosts map[string][]net.IP
}

func (r fakeResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	ips, ok := r.hosts[host]
	if !ok {
		return nil, fmt.Errorf("host not found: %s", host)
	}
	return ips, nil
}

type fakeRenderer struct {
	hits    atomic.Int32
	handler func(ctx context.Context, rawURL string) (string, string, error)
}

func (f *fakeRenderer) RenderPage(ctx context.Context, rawURL string) (string, string, error) {
	f.hits.Add(1)
	return f.handler(ctx, rawURL)
}

func newTestService(renderer PageRenderer, validator URLValidator) (*AnalysisService, *cache.Cache) {
	resultCache := cache.New(time.Minute, 100)
	return NewAnalysisService(
		renderer,
		resultCache,
		validator,
		middleware.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	), resultCache
}

func TestAnalyze_CachesByFinalURL(t *testing.T) {
	renderer := &fakeRenderer{
		handler: func(_ context.Context, rawURL string) (string, string, error) {
			finalURL := rawURL
			if strings.Contains(rawURL, "/redirect") {
				finalURL = "http://safe.test/final"
			}
			html := `<!DOCTYPE html><html><head><title>Cached Result</title></head><body></body></html>`
			return html, finalURL, nil
		},
	}

	validator := security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"safe.test": {net.ParseIP("93.184.216.34")},
		},
	})

	svc, resultCache := newTestService(renderer, validator)
	defer resultCache.Close()

	firstURL, firstResult, err := svc.Analyze(context.Background(), "http://safe.test/redirect")
	if err != nil {
		t.Fatalf("expected first analyze call to succeed, got %v", err)
	}
	if firstURL != "http://safe.test/redirect" {
		t.Fatalf("expected normalized URL to be preserved, got %q", firstURL)
	}
	if firstResult == nil || firstResult.Title != "Cached Result" {
		t.Fatalf("expected analyzed result, got %#v", firstResult)
	}
	if renderer.hits.Load() != 1 {
		t.Fatalf("expected renderer to be called once, got %d", renderer.hits.Load())
	}

	secondURL, secondResult, err := svc.Analyze(context.Background(), "http://safe.test/final")
	if err != nil {
		t.Fatalf("expected second analyze call to succeed, got %v", err)
	}
	if secondURL != "http://safe.test/final" {
		t.Fatalf("expected direct final URL, got %q", secondURL)
	}
	if secondResult == nil || secondResult.Title != "Cached Result" {
		t.Fatalf("expected cached result, got %#v", secondResult)
	}
	if renderer.hits.Load() != 1 {
		t.Fatalf("expected second request to use cache, renderer hit count %d", renderer.hits.Load())
	}
}

func TestAnalyze_BlockedFinalURLReturnsForbidden(t *testing.T) {
	renderer := &fakeRenderer{
		handler: func(_ context.Context, rawURL string) (string, string, error) {
			html := `<!DOCTYPE html><html><head><title>Blocked</title></head><body></body></html>`
			return html, "http://127.0.0.1/admin", nil
		},
	}

	validator := security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"safe.test": {net.ParseIP("93.184.216.34")},
		},
	})

	svc, resultCache := newTestService(renderer, validator)
	defer resultCache.Close()

	_, _, err := svc.Analyze(context.Background(), "http://safe.test/redirect")
	if err == nil {
		t.Fatal("expected blocked final URL error")
	}

	statusCode, message := ErrorResponse(err)
	if statusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", statusCode)
	}
	if !strings.Contains(message, "URL is not allowed") {
		t.Fatalf("expected blocked message, got %q", message)
	}
}

func TestAnalyze_InputValidationReturnsBadRequest(t *testing.T) {
	svc, resultCache := newTestService(&fakeRenderer{}, security.DefaultValidator())
	defer resultCache.Close()

	_, _, err := svc.Analyze(context.Background(), "")
	if err == nil {
		t.Fatal("expected validation error")
	}

	statusCode, message := ErrorResponse(err)
	if statusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", statusCode)
	}
	if message != "Please enter a URL" {
		t.Fatalf("expected input validation message, got %q", message)
	}
}

func TestAnalyze_RenderFailureReturnsBadGateway(t *testing.T) {
	renderer := &fakeRenderer{
		handler: func(_ context.Context, rawURL string) (string, string, error) {
			return "", "", fmt.Errorf("boom")
		},
	}

	validator := security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"safe.test": {net.ParseIP("93.184.216.34")},
		},
	})

	svc, resultCache := newTestService(renderer, validator)
	defer resultCache.Close()

	_, _, err := svc.Analyze(context.Background(), "http://safe.test")
	if err == nil {
		t.Fatal("expected render failure")
	}

	statusCode, message := ErrorResponse(err)
	if statusCode != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", statusCode)
	}
	if !strings.Contains(message, "Could not render the page: boom") {
		t.Fatalf("expected render failure message, got %q", message)
	}
}

func TestAnalyze_RendererBlockedRequestReturnsForbidden(t *testing.T) {
	renderer := &fakeRenderer{
		handler: func(_ context.Context, rawURL string) (string, string, error) {
			return "", "", &security.BlockedURLError{
				URL: "http://127.0.0.1/admin",
				Err: fmt.Errorf("access to private/internal addresses is not allowed (127.0.0.1)"),
			}
		},
	}

	validator := security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"safe.test": {net.ParseIP("93.184.216.34")},
		},
	})

	svc, resultCache := newTestService(renderer, validator)
	defer resultCache.Close()

	_, _, err := svc.Analyze(context.Background(), "http://safe.test")
	if err == nil {
		t.Fatal("expected blocked renderer error")
	}

	statusCode, message := ErrorResponse(err)
	if statusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", statusCode)
	}
	if !strings.Contains(message, "URL is not allowed") {
		t.Fatalf("expected blocked renderer message, got %q", message)
	}
}

package controller

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"web-scrapper/internal/models"
	"web-scrapper/internal/service"
)

type fakeAnalysisService struct {
	handler func(ctx context.Context, rawURL string) (string, *models.AnalysisResult, error)
}

func (f *fakeAnalysisService) Analyze(ctx context.Context, rawURL string) (string, *models.AnalysisResult, error) {
	return f.handler(ctx, rawURL)
}

func newTestController(t *testing.T, svc AnalysisService) *AnalyzerController {
	t.Helper()
	return NewAnalyzerController(
		template.Must(template.New("test").Parse(`{{if .Result}}{{.Result.Title}}{{else}}{{.Error}}{{end}}`)),
		svc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestAnalyzePage_RendersSuccessfulResult(t *testing.T) {
	ctrl := newTestController(t, &fakeAnalysisService{
		handler: func(_ context.Context, rawURL string) (string, *models.AnalysisResult, error) {
			return "https://example.com", &models.AnalysisResult{Title: "Rendered"}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader("url=example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	ctrl.AnalyzePage(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if strings.TrimSpace(resp.Body.String()) != "Rendered" {
		t.Fatalf("expected rendered title, got %q", resp.Body.String())
	}
}

func TestAnalyzePage_RendersServiceError(t *testing.T) {
	ctrl := newTestController(t, &fakeAnalysisService{
		handler: func(_ context.Context, rawURL string) (string, *models.AnalysisResult, error) {
			return "https://example.com", nil, &service.AnalysisError{
				StatusCode:    http.StatusBadGateway,
				PublicMessage: "Could not render the page: boom",
				Err:           errors.New("boom"),
			}
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader("url=example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	ctrl.AnalyzePage(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "Could not render the page: boom") {
		t.Fatalf("expected error message, got %q", resp.Body.String())
	}
}

package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"web-scrapper/internal/analyzer"
	"web-scrapper/internal/middleware"
	"web-scrapper/internal/models"
	"web-scrapper/internal/security"
)

// PageRenderer renders a URL and returns the HTML content and final URL.
type PageRenderer interface {
	RenderPage(ctx context.Context, rawURL string) (html string, finalURL string, err error)
}

// ResultCache stores analysis results by URL.
type ResultCache interface {
	Get(url string) (*models.AnalysisResult, bool)
	Set(url string, result *models.AnalysisResult)
}

// URLValidator validates whether a URL is safe to fetch.
type URLValidator interface {
	ValidateURL(ctx context.Context, rawURL string) error
}

// MetricsRecorder records analysis counters.
type MetricsRecorder interface {
	RecordRequest()
	RecordSuccess()
	RecordFailure()
	RecordCacheHit()
}

// AnalysisError contains an HTTP status and user-facing message.
type AnalysisError struct {
	StatusCode    int
	PublicMessage string
	Err           error
}

func (e *AnalysisError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.PublicMessage
	}
	return e.PublicMessage + ": " + e.Err.Error()
}

func (e *AnalysisError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ErrorResponse extracts the HTTP status code and public message.
func ErrorResponse(err error) (int, string) {
	var analysisErr *AnalysisError
	if errors.As(err, &analysisErr) {
		return analysisErr.StatusCode, analysisErr.PublicMessage
	}
	return http.StatusInternalServerError, "Internal server error"
}

// AnalysisService orchestrates validation, rendering, caching, and analysis.
type AnalysisService struct {
	renderer  PageRenderer
	cache     ResultCache
	validator URLValidator
	metrics   MetricsRecorder
	logger    *slog.Logger
}

// NewAnalysisService creates a new AnalysisService.
func NewAnalysisService(r PageRenderer, c ResultCache, v URLValidator, m MetricsRecorder, logger *slog.Logger) *AnalysisService {
	if v == nil {
		v = security.DefaultValidator()
	}

	return &AnalysisService{
		renderer:  r,
		cache:     c,
		validator: v,
		metrics:   m,
		logger:    logger,
	}
}

// Analyze validates input, renders the page, analyzes it, and returns the result.
func (s *AnalysisService) Analyze(ctx context.Context, rawURL string) (string, *models.AnalysisResult, error) {
	s.recordRequest()
	rawURL = strings.TrimSpace(rawURL)

	if rawURL == "" {
		return rawURL, nil, s.fail(http.StatusBadRequest, "Please enter a URL", nil)
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Host == "" {
		return rawURL, nil, s.fail(http.StatusBadRequest, "Invalid URL format", nil)
	}

	if err := s.validator.ValidateURL(ctx, rawURL); err != nil {
		return rawURL, nil, s.fail(http.StatusForbidden, "URL is not allowed: "+err.Error(), err)
	}

	if cached, ok := s.cache.Get(rawURL); ok {
		s.recordCacheHit()
		s.recordSuccess()
		s.getLogger().Info("cache hit",
			"request_id", middleware.GetRequestID(ctx),
			"url", rawURL,
		)
		return rawURL, cached, nil
	}

	s.getLogger().Info("rendering URL with headless browser",
		"request_id", middleware.GetRequestID(ctx),
		"url", rawURL,
	)

	htmlContent, finalURLStr, err := s.renderer.RenderPage(ctx, rawURL)
	if err != nil {
		if isBlockedAnalysisError(err) {
			return rawURL, nil, s.fail(http.StatusForbidden, "URL is not allowed: "+err.Error(), err)
		}
		return rawURL, nil, s.fail(http.StatusBadGateway, "Could not render the page: "+err.Error(), err)
	}

	finalURL, err := url.Parse(finalURLStr)
	if err != nil {
		return rawURL, nil, s.fail(http.StatusBadGateway, "Invalid redirect URL: "+err.Error(), err)
	}

	if err := s.validator.ValidateURL(ctx, finalURLStr); err != nil {
		return rawURL, nil, s.fail(http.StatusForbidden, "URL is not allowed: "+err.Error(), err)
	}

	if cached, ok := s.cache.Get(finalURLStr); ok {
		s.recordCacheHit()
		s.recordSuccess()
		s.getLogger().Info("cache hit after redirect resolution",
			"request_id", middleware.GetRequestID(ctx),
			"url", rawURL,
			"cache_key", finalURLStr,
		)
		return rawURL, cached, nil
	}

	result := analyzer.AnalyzeWithValidator(ctx, strings.NewReader(htmlContent), finalURL, s.validator)
	result.RenderMode = "JavaScript Rendered"

	s.cache.Set(finalURLStr, result)
	s.recordSuccess()

	s.getLogger().Info("analysis complete",
		"request_id", middleware.GetRequestID(ctx),
		"url", rawURL,
		"html_version", result.HTMLVersion,
		"internal_links", result.InternalLinks,
		"external_links", result.ExternalLinks,
		"inaccessible_links", result.InaccessibleLinks,
		"has_login_form", result.HasLoginForm,
	)

	return rawURL, result, nil
}

func (s *AnalysisService) fail(statusCode int, message string, err error) error {
	s.recordFailure()
	return &AnalysisError{
		StatusCode:    statusCode,
		PublicMessage: message,
		Err:           err,
	}
}

func (s *AnalysisService) getLogger() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func (s *AnalysisService) recordRequest() {
	if s.metrics != nil {
		s.metrics.RecordRequest()
	}
}

func (s *AnalysisService) recordSuccess() {
	if s.metrics != nil {
		s.metrics.RecordSuccess()
	}
}

func (s *AnalysisService) recordFailure() {
	if s.metrics != nil {
		s.metrics.RecordFailure()
	}
}

func (s *AnalysisService) recordCacheHit() {
	if s.metrics != nil {
		s.metrics.RecordCacheHit()
	}
}

func isBlockedAnalysisError(err error) bool {
	if security.IsBlockedError(err) {
		return true
	}

	var urlErr *url.Error
	return errors.As(err, &urlErr) && security.IsBlockedError(urlErr.Err)
}

package controller

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"net/http"

	"web-scrapper/internal/models"
	"web-scrapper/internal/service"
)

// Verify interface compliance at compile time.
var _ AnalysisService = (*service.AnalysisService)(nil)

// AnalysisService executes the analysis use case.
type AnalysisService interface {
	Analyze(ctx context.Context, rawURL string) (string, *models.AnalysisResult, error)
}

// AnalyzerController handles webpage analysis requests.
type AnalyzerController struct {
	tmpl    *template.Template
	service AnalysisService
	logger  *slog.Logger
}

// NewAnalyzerController creates an AnalyzerController with configured dependencies.
func NewAnalyzerController(tmpl *template.Template, svc AnalysisService, logger *slog.Logger) *AnalyzerController {
	return &AnalyzerController{
		tmpl:    tmpl,
		service: svc,
		logger:  logger,
	}
}

// HomePage renders the form page.
func (ac *AnalyzerController) HomePage(w http.ResponseWriter, r *http.Request) {
	if err := ac.tmpl.Execute(w, models.PageData{}); err != nil {
		ac.logger.Error("template execution failed", "error", err)
	}
}

// AnalyzePage handles the form submission, fetches the URL, and renders results.
func (ac *AnalyzerController) AnalyzePage(w http.ResponseWriter, r *http.Request) {
	rawURL, result, err := ac.service.Analyze(r.Context(), r.FormValue("url"))
	if err != nil {
		ac.renderServiceError(w, rawURL, err)
		return
	}
	ac.render(w, rawURL, result)
}

// render renders the results page.
func (ac *AnalyzerController) render(w http.ResponseWriter, rawURL string, result *models.AnalysisResult) {
	if err := ac.tmpl.Execute(w, models.PageData{
		URL:    rawURL,
		Result: result,
	}); err != nil {
		ac.logger.Error("template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (ac *AnalyzerController) renderServiceError(w http.ResponseWriter, rawURL string, err error) {
	statusCode, message := service.ErrorResponse(err)

	// Render to a buffer first so we don't write headers before knowing
	// whether the template succeeded, avoiding a double-WriteHeader.
	var buf bytes.Buffer
	if tplErr := ac.tmpl.Execute(&buf, models.PageData{
		URL:        rawURL,
		Error:      message,
		StatusCode: statusCode,
	}); tplErr != nil {
		ac.logger.Error("template execution failed", "error", tplErr)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(statusCode)
	buf.WriteTo(w)
}

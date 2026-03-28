package transport

import (
	"log/slog"
	"net/http"

	"web-scrapper/internal/controller"
	"web-scrapper/internal/middleware"
)

// NewRouter sets up all routes, static file serving, and middleware chain.
// Returns a fully configured http.Handler ready to be used by the server.
func NewRouter(ac *controller.AnalyzerController, metrics *middleware.Metrics, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Application routes
	mux.HandleFunc("/", exactPath("/", ac.HomePage))
	mux.HandleFunc("/analyze", methodOnly(http.MethodPost, ac.AnalyzePage))

	// Operational routes
	mux.HandleFunc("/metrics", metrics.Handler())

	// Static files
	fs := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Middleware chain (outermost runs first)
	// RequestID → Logging → router
	var handler http.Handler = mux
	handler = middleware.Logging(logger)(handler)
	handler = middleware.RequestID(handler)

	return handler
}

// exactPath rejects requests whose path does not match exactly, returning 404.
func exactPath(path string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// methodOnly wraps a handler to only accept the specified HTTP method.
func methodOnly(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

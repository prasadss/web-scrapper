package main

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web-scrapper/internal/cache"
	"web-scrapper/internal/controller"
	"web-scrapper/internal/middleware"
	"web-scrapper/internal/renderer"
	"web-scrapper/internal/security"
	"web-scrapper/internal/service"
	"web-scrapper/internal/transport"
)

func main() {
	// Structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load HTML template
	tmpl, err := template.ParseFiles("web/templates/index.html")
	if err != nil {
		logger.Error("failed to load template", "error", err)
		os.Exit(1)
	}

	// Initialize headless browser renderer
	validator := security.DefaultValidator()
	pageRenderer, err := renderer.New(15*time.Second, validator)
	if err != nil {
		logger.Error("failed to start headless browser", "error", err)
		os.Exit(1)
	}
	defer pageRenderer.Close()

	// Initialize dependencies
	resultCache := cache.New(5*time.Minute, 100)
	defer resultCache.Close()
	metrics := middleware.NewMetrics()
	analysisService := service.NewAnalysisService(pageRenderer, resultCache, validator, metrics, logger)
	analyzerCtrl := controller.NewAnalyzerController(tmpl, analysisService, logger)

	// Build router with all routes and middleware
	router := transport.NewRouter(analyzerCtrl, metrics, logger)

	// Determine port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("server starting", "port", port, "url", "http://localhost:"+port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped gracefully")
}

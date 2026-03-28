package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

type contextKey string

const _requestIDKey contextKey = "request_id"

// RequestID is middleware that assigns a unique request ID to each request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateID()
		ctx := context.WithValue(r.Context(), _requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(_requestIDKey).(string); ok {
		return id
	}
	return "unknown"
}

// generateID creates a short unique ID.
func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}

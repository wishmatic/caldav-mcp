package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

var ErrNoAPIKey = errors.New("API_KEY is required")

func Middleware(log *zap.Logger, apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, reason, ok := bearerToken(r)
			if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
				if ok {
					reason = "invalid token"
				}

				log.Warn("authentication failed",
					zap.String("reason", reason),
					zap.String("client_ip", middleware.GetClientIP(r.Context())),
					zap.String("request_id", middleware.GetReqID(r.Context())),
				)

				unauthorized(w)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) (token, reason string, ok bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", "missing authorization header", false
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", "unsupported authorization scheme", false
	}

	token = strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", "empty token", false
	}

	return token, "", true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	w.WriteHeader(http.StatusUnauthorized)

	_, _ = w.Write([]byte("unauthorized"))
}

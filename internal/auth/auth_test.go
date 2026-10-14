package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

const testKey = "secret-key-123"

func TestBearerAuth(t *testing.T) {
	tests := map[string]struct {
		authHeader string
		wantCode   int
	}{
		"valid":          {authHeader: "Bearer " + testKey, wantCode: http.StatusOK},
		"missing header": {authHeader: "", wantCode: http.StatusUnauthorized},
		"wrong token":    {authHeader: "Bearer wrong-key", wantCode: http.StatusUnauthorized},
		"empty token":    {authHeader: "Bearer ", wantCode: http.StatusUnauthorized},
		"basic scheme":   {authHeader: "Basic " + testKey, wantCode: http.StatusUnauthorized},
		"no space":       {authHeader: "Bearer" + testKey, wantCode: http.StatusUnauthorized},
		"trailing space": {authHeader: "Bearer " + testKey + " ", wantCode: http.StatusOK},
		"token prefix":   {authHeader: "Bearer " + testKey[:5], wantCode: http.StatusUnauthorized},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := do(t, guarded(), tt.authHeader)
			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d (body: %q)", rec.Code, tt.wantCode, rec.Body.String())
			}
		})
	}
}

func TestUnauthorizedIncludesChallenge(t *testing.T) {
	rec := do(t, guarded(), "")

	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header on 401 response")
	}
}

func guarded() http.Handler {
	mw := Middleware(zap.NewNop(), testKey)

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

func do(t *testing.T, h http.Handler, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

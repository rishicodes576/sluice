package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func ok(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(Owner(r.Context())))
}

func TestAuthDisabledAllowsAnonymous(t *testing.T) {
	a := New(nil)
	h := a.Middleware(http.HandlerFunc(ok))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "anonymous" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAuthRejectsMissingKey(t *testing.T) {
	a := New([]Key{{Secret: "sk-1", Owner: "team"}})
	h := a.Middleware(http.HandlerFunc(ok))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestAuthAcceptsValidKeyAndSetsOwner(t *testing.T) {
	a := New([]Key{{Secret: "sk-1", Owner: "team"}})
	h := a.Middleware(http.HandlerFunc(ok))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer sk-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "team" {
		t.Fatalf("code=%d owner=%q", rec.Code, rec.Body.String())
	}
}

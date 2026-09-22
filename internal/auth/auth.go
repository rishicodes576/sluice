// Package auth authenticates requests by API key (a.k.a. virtual keys) and
// exposes the resolved key identity to downstream middleware via the context.
package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// Key is a virtual API key with an owner label used for per-key rate limiting
// and budgeting.
type Key struct {
	Secret string `json:"secret"`
	Owner  string `json:"owner"`
}

type ctxKey struct{}

// Authenticator validates bearer tokens against a configured key set.
type Authenticator struct {
	enabled bool
	keys    map[string]string // secret -> owner
}

// New creates an Authenticator. When no keys are configured, auth is disabled
// and every request is treated as the "anonymous" owner (handy for local demos).
func New(keys []Key) *Authenticator {
	a := &Authenticator{keys: make(map[string]string)}
	for _, k := range keys {
		if k.Secret == "" {
			continue
		}
		owner := k.Owner
		if owner == "" {
			owner = "default"
		}
		a.keys[k.Secret] = owner
	}
	a.enabled = len(a.keys) > 0
	return a
}

// Enabled reports whether key checking is active.
func (a *Authenticator) Enabled() bool { return a.enabled }

// constantTimeLookup finds the owner for secret without leaking timing about
// which configured key matched.
func (a *Authenticator) lookup(secret string) (string, bool) {
	var owner string
	found := false
	for s, o := range a.keys {
		if subtle.ConstantTimeCompare([]byte(s), []byte(secret)) == 1 {
			owner, found = o, true
		}
	}
	return owner, found
}

// Middleware enforces authentication and stashes the owner in the context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled {
			next.ServeHTTP(w, r.WithContext(withOwner(r.Context(), "anonymous")))
			return
		}
		secret := bearer(r)
		owner, ok := a.lookup(secret)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"authentication_error"}}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(withOwner(r.Context(), owner)))
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return r.Header.Get("x-api-key")
}

func withOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ctxKey{}, owner)
}

// Owner returns the authenticated owner for the request context, or "anonymous".
func Owner(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return "anonymous"
}

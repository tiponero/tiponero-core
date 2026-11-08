package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const csrfCookieName = "tiponero_csrf"
const csrfHeaderName = "X-CSRF-Token"
const csrfFormField = "csrf_token"

// csrfCookie ensures every browser has a CSRF token. The cookie is readable by
// JavaScript on purpose: the admin layout copies it into a header and into a
// hidden form field.
func csrfCookie(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(csrfCookieName); err != nil {
				token := randomToken()
				http.SetCookie(w, &http.Cookie{
					Name:     csrfCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: false,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400 * 7,
				})
				r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfProtect rejects a state-changing admin request that does not echo the
// cookie value in the header or the form body.
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "missing CSRF cookie", http.StatusForbidden)
			return
		}

		token := r.Header.Get(csrfHeaderName)
		if token == "" {
			token = r.FormValue(csrfFormField)
		}
		if token == "" {
			http.Error(w, "missing CSRF token", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(cookie.Value)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

type rateBucket struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	lastGC  time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*rateBucket), lastGC: time.Now()}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b, ok := l.buckets[key]
	if !ok || now.After(b.reset) {
		l.buckets[key] = &rateBucket{count: 1, reset: now.Add(window)}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}

var globalLimiter = newRateLimiter()

// rateLimit applies a small per-IP budget to the endpoints that can be abused:
// login guessing, widget spam, and API floods.
func rateLimit(trustProxy bool, log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit, window := rateClass(r)
			if limit == 0 {
				next.ServeHTTP(w, r)
				return
			}

			key := clientIP(r, trustProxy) + "|" + rateKey(r)
			if !globalLimiter.allow(key, limit, window) {
				log.Warn().Str("ip", clientIP(r, trustProxy)).Str("path", r.URL.Path).Msg("rate limit exceeded")
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func rateClass(r *http.Request) (int, time.Duration) {
	path := r.URL.Path
	switch {
	case path == "/admin/login" || path == "/admin/login/totp":
		return 10, time.Minute
	case strings.HasPrefix(path, "/api/v1/"):
		return 240, time.Minute
	case strings.HasPrefix(path, "/widget/") && r.Method == http.MethodPost:
		return 60, time.Minute
	case strings.HasPrefix(path, "/admin"):
		return 120, time.Minute
	default:
		return 0, 0
	}
}

func rateKey(r *http.Request) string {
	switch {
	case r.URL.Path == "/admin/login":
		return "login"
	case strings.HasPrefix(r.URL.Path, "/api/v1/"):
		return "api"
	case strings.HasPrefix(r.URL.Path, "/widget/") && r.Method == http.MethodPost:
		return "widget-create"
	default:
		return "admin"
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, ok := strings.Cut(forwarded, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(forwarded)
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return strings.TrimSpace(real)
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

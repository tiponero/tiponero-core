package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/tiponero/tiponero-core/internal/database"
	"golang.org/x/crypto/bcrypt"
)

type APIKeyAuthenticator struct {
	db *database.DB
}

func NewAPIKeyAuthenticator(db *database.DB) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{db: db}
}

func (a *APIKeyAuthenticator) RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			writeAPIError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}

		rawKey := strings.TrimPrefix(header, "Bearer ")
		if !isValidKeyFormat(rawKey) {
			writeAPIError(w, http.StatusUnauthorized, "invalid API key format")
			return
		}

		prefix := rawKey[4:12]

		candidates, err := a.db.GetAPIKeysByPrefix(prefix)
		if err != nil || len(candidates) == 0 {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var matched *database.APIKey
		for i := range candidates {
			if bcrypt.CompareHashAndPassword([]byte(candidates[i].KeyHash), []byte(rawKey)) == nil {
				matched = &candidates[i]
				break
			}
		}

		if matched == nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if time.Now().Unix() > matched.ExpiresAt {
			writeAPIError(w, http.StatusUnauthorized, "API key expired")
			return
		}

		go a.db.TouchAPIKey(matched.ID)

		ctx := context.WithValue(r.Context(), userIDKey, matched.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isValidKeyFormat(key string) bool {
	if len(key) != 36 || !strings.HasPrefix(key, "tip_") {
		return false
	}
	for _, c := range key[4:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

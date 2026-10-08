package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/golang-jwt/jwt/v5"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleLogin validates credentials and issues an HttpOnly JWT cookie
func HandleLogin(cfg *config.Config, resolver DynamicUserResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Use our DRY helper to check static YAML or dynamic API
		authMethod, _ := VerifyPasswordAuth(cfg, resolver, req.Username, req.Password)
		if authMethod == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mint the JWT
		expirationTime := time.Now().Add(24 * time.Hour)
		claims := &jwt.RegisteredClaims{
			Subject:   req.Username,
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(cfg.Server.SessionSecret))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Set the secure HttpOnly cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "hook22_auth",
			Value:    tokenString,
			Expires:  expirationTime,
			HttpOnly: true,                 // Cannot be read by frontend JavaScript
			Secure:   true,                 // Only sent over HTTPS (set false if testing locally over HTTP)
			SameSite: http.SameSiteLaxMode, // Sent with top-level navigations
			Path:     "/",
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}
}

// RequireCookieAuth extracts the cookie, validates the JWT signature, and permits access
func RequireCookieAuth(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("hook22_auth")
		if err != nil {
			http.Error(w, `{"error":"Unauthorized - Missing Cookie"}`, http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(cookie.Value, func(token *jwt.Token) (interface{}, error) {
			// Enforce HMAC signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, `{"error":"Unauthorized - Invalid Token"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

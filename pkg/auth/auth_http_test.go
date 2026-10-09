package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestHandleLogin_Success(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.SessionSecret = "test_secret_123"

	// Generate a hash for the test
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpassword"), bcrypt.DefaultCost)

	cfg.Users = []config.UserConfig{
		{Username: "testuser", PasswordHash: string(hash)},
	}

	payload := LoginRequest{Username: "testuser", Password: "testpassword"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler := HandleLogin(cfg, nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Expected cookie to be set")
	}

	cookie := cookies[0]
	if cookie.Name != "hook22_auth" {
		t.Errorf("Expected cookie name hook22_auth, got %s", cookie.Name)
	}
	if !cookie.HttpOnly {
		t.Error("Expected cookie to be HttpOnly")
	}
}

func TestHandleLogin_InvalidCredentials(t *testing.T) {
	cfg := &config.Config{}

	// Generate a hash for the test
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpassword"), bcrypt.DefaultCost)

	cfg.Users = []config.UserConfig{
		{Username: "testuser", PasswordHash: string(hash)},
	}

	payload := LoginRequest{Username: "testuser", Password: "wrongpassword"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler := HandleLogin(cfg, nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestRequireCookieAuth_ValidToken(t *testing.T) {
	secret := "test_secret_123"

	// Create a valid JWT manually
	claims := &jwt.RegisteredClaims{
		Subject:   "testuser",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(secret))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "hook22_auth", Value: tokenString})

	rec := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireCookieAuth(secret, nextHandler)
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for valid token, got %d", rec.Code)
	}
}

func TestRequireCookieAuth_MissingCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireCookieAuth("secret", nextHandler)
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for missing cookie, got %d", rec.Code)
	}
}

func TestRequireCookieAuth_InvalidToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "hook22_auth", Value: "invalid.garbage.token"})
	rec := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireCookieAuth("secret", nextHandler)
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for invalid token, got %d", rec.Code)
	}
}

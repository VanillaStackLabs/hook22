package gateway

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// GET /api/v1/users
func HandleGetUsers(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Safely returns the user list; password hashes are stripped by json:"-"
		json.NewEncoder(w).Encode(cfg.Users)
	}
}

// POST /api/v1/users
func HandleAddUser(cfg *config.Config, mu *sync.RWMutex) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		// Hash the password
		hashBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		// Prevent duplicates
		for _, u := range cfg.Users {
			if u.Username == req.Username {
				http.Error(w, "User already exists", http.StatusConflict)
				return
			}
		}

		// Append to runtime configuration
		cfg.Users = append(cfg.Users, struct {
			Username     string   `yaml:"username" json:"username"`
			PasswordHash string   `yaml:"password_hash" json:"-"`
			PublicKeys   []string `yaml:"public_keys" json:"public_keys"`
		}{
			Username:     req.Username,
			PasswordHash: string(hashBytes),
			PublicKeys:   []string{},
		})

		// Persist to config.yaml atomically
		data, err := yaml.Marshal(cfg)
		if err != nil {
			http.Error(w, "Failed to serialize config", http.StatusInternalServerError)
			return
		}

		if err := os.WriteFile("config.yaml", data, 0644); err != nil {
			http.Error(w, "Failed to write config.yaml", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "user_added", "username": req.Username})
	}
}

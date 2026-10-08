package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"golang.org/x/crypto/ssh"
)

type UserPermissions struct {
	AllowedStorageBucket string `json:"allowed_storage_bucket"`
	S3PrefixPattern      string `json:"s3_prefix_pattern"` // "tenants/{username}/"
	WebhookOverrideURL   string `json:"webhook_override_url"`
}

type DynamicUserResolver interface {
	AuthenticatePassword(username, password string) (*UserPermissions, bool)
	AuthenticatePublicKey(username string, pubKey ssh.PublicKey) (*UserPermissions, bool)
}

// HTTPControlPlaneResolver queries a remote REST API for dynamic auth
type HTTPControlPlaneResolver struct {
	endpoint string
	client   *http.Client
}

func NewHTTPControlPlaneResolver(endpoint string) *HTTPControlPlaneResolver {
	return &HTTPControlPlaneResolver{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

type authRequestPayload struct {
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	AuthType  string `json:"auth_type"`
}

func (r *HTTPControlPlaneResolver) AuthenticatePassword(username, password string) (*UserPermissions, bool) {
	if r.endpoint == "" {
		return nil, false
	}

	payload := authRequestPayload{
		Username: username,
		Password: password,
		AuthType: "password",
	}

	return r.sendAuthQuery(payload)
}

func (r *HTTPControlPlaneResolver) AuthenticatePublicKey(username string, pubKey ssh.PublicKey) (*UserPermissions, bool) {
	if r.endpoint == "" {
		return nil, false
	}

	payload := authRequestPayload{
		Username:  username,
		PublicKey: string(ssh.MarshalAuthorizedKey(pubKey)),
		AuthType:  "publickey",
	}

	return r.sendAuthQuery(payload)
}

func (r *HTTPControlPlaneResolver) sendAuthQuery(payload authRequestPayload) (*UserPermissions, bool) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}

	req, err := http.NewRequest("POST", r.endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, false
	}
	defer resp.Body.Close()

	var perms UserPermissions
	if err := json.NewDecoder(resp.Body).Decode(&perms); err != nil {
		return nil, false
	}

	return &perms, true
}

// VerifyPasswordAuth returns the auth method ("static_password" or "dynamic_password")
// and the associated user permissions. It returns an empty string if auth fails.
func VerifyPasswordAuth(cfg *config.Config, resolver DynamicUserResolver, username, password string) (string, *UserPermissions) {
	// Static YAML Lookup
	for _, u := range cfg.Users {
		if u.Username == username && u.Password == password {
			return "static_password", nil
		}
	}

	// Dynamic Control Plane Fallback
	if resolver != nil {
		if perms, ok := resolver.AuthenticatePassword(username, password); ok {
			return "dynamic_password", perms
		}
	}

	return "", nil
}

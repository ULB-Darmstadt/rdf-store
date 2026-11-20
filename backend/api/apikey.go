package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rdf-store-backend/base"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	apiKeyHeader     = "X-API-Key"
	apiKeyPrefix     = "apikey:v1:"
	ownerKeyPrefix   = "apikey:owner:v1:"
	apiKeyAuthKey    = "apiKeyAuth"
	defaultAPIKeyTTL = 7 * 24 * time.Hour
)

type apiKeyIdentity struct {
	User   string `json:"user"`
	Email  string `json:"email"`
	Groups string `json:"groups"`
}

type APIKeyResponse struct {
	APIKey    string    `json:"apiKey"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type apiKeyStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

type redisAPIKeyStore struct {
	client *redis.Client
}

func (s redisAPIKeyStore) Get(ctx context.Context, key string) ([]byte, error) {
	return s.client.Get(ctx, key).Bytes()
}

func (s redisAPIKeyStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s redisAPIKeyStore) Del(ctx context.Context, keys ...string) error {
	return s.client.Del(ctx, keys...).Err()
}

var apiKeys apiKeyStore
var apiKeyEnabled bool
var apiKeyTTL = defaultAPIKeyTTL

func init() {
	apiKeyTTL = parseAPIKeyTTL(base.APIKeyTTL)
	url := strings.TrimSpace(base.APIKeyRedisURL)
	if !base.Configuration.AuthEnabled || url == "" {
		return
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		slog.Error("invalid API_KEY_REDIS_URL; API keys are disabled", "error", err)
		return
	}
	apiKeys = redisAPIKeyStore{client: redis.NewClient(opts)}
	apiKeyEnabled = true
}

func parseAPIKeyTTL(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultAPIKeyTTL
	}
	ttl, err := time.ParseDuration(value)
	if err != nil {
		slog.Error("invalid API_KEY_TTL; using default", "value", value, "error", err)
		return defaultAPIKeyTTL
	}
	if ttl <= 0 {
		slog.Error("API_KEY_TTL must be positive; using default", "value", value)
		return defaultAPIKeyTTL
	}
	return ttl
}

func apiKeyMiddleware(c *gin.Context) {
	if !apiKeyEnabled || c.GetHeader(base.AuthUserHeader) != "" {
		c.Next()
		return
	}
	token := strings.TrimSpace(c.GetHeader(apiKeyHeader))
	if token == "" {
		c.Next()
		return
	}
	data, err := apiKeys.Get(c.Request.Context(), apiKeyTokenKey(hashValue(token)))
	if err != nil {
		c.Next()
		return
	}
	var identity apiKeyIdentity
	if err := json.Unmarshal(data, &identity); err != nil || identity.User == "" {
		c.Next()
		return
	}
	c.Request.Header.Set(base.AuthUserHeader, identity.User)
	if identity.Email != "" {
		c.Request.Header.Set(base.AuthEmailHeader, identity.Email)
	}
	if identity.Groups != "" {
		c.Request.Header.Set(base.AuthGroupsHeader, identity.Groups)
	}
	c.Set(apiKeyAuthKey, true)
	c.Next()
}

func handleGenerateAPIKey(c *gin.Context) {
	if !apiKeyEnabled {
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "API keys are not configured."})
		return
	}
	if c.GetBool(apiKeyAuthKey) {
		c.Status(http.StatusForbidden)
		return
	}
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted || user == "" {
		c.Status(http.StatusForbidden)
		return
	}
	token, err := newAPIKeyToken()
	if err != nil {
		slog.Error("failed generating API key", "error", err)
		c.JSON(http.StatusInternalServerError, JSONError{Error: "Could not generate API key."})
		return
	}
	identity := apiKeyIdentity{
		User:   user,
		Email:  c.GetHeader(base.AuthEmailHeader),
		Groups: c.GetHeader(base.AuthGroupsHeader),
	}
	hash := hashValue(token)
	data, err := json.Marshal(identity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, JSONError{Error: "Could not generate API key."})
		return
	}
	ctx := c.Request.Context()
	ownerKey := apiKeyOwnerKey(user)
	var oldTokenKey string
	if oldHash, err := apiKeys.Get(ctx, ownerKey); err == nil {
		if oldHash := strings.TrimSpace(string(oldHash)); oldHash != "" {
			oldTokenKey = apiKeyTokenKey(oldHash)
		}
	}
	tokenKey := apiKeyTokenKey(hash)
	if err := apiKeys.Set(ctx, tokenKey, data, apiKeyTTL); err != nil {
		slog.Error("failed storing API key", "error", err)
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "Could not store API key."})
		return
	}
	if err := apiKeys.Set(ctx, ownerKey, []byte(hash), apiKeyTTL); err != nil {
		if delErr := apiKeys.Del(ctx, tokenKey); delErr != nil {
			slog.Error("failed removing untracked API key", "error", delErr)
		}
		slog.Error("failed storing API key owner", "error", err)
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "Could not store API key."})
		return
	}
	if oldTokenKey != "" {
		if err := apiKeys.Del(ctx, oldTokenKey); err != nil {
			slog.Error("failed revoking previous API key", "error", err)
		}
	}
	c.JSON(http.StatusOK, APIKeyResponse{APIKey: token, ExpiresAt: time.Now().Add(apiKeyTTL).UTC()})
}

func handleRenewAPIKey(c *gin.Context) {
	if !apiKeyEnabled {
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "API keys are not configured."})
		return
	}
	if c.GetBool(apiKeyAuthKey) {
		c.Status(http.StatusForbidden)
		return
	}
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted || user == "" {
		c.Status(http.StatusForbidden)
		return
	}
	ctx := c.Request.Context()
	ownerKey := apiKeyOwnerKey(user)
	hash, err := apiKeys.Get(ctx, ownerKey)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	hash = []byte(strings.TrimSpace(string(hash)))
	if len(hash) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	tokenKey := apiKeyTokenKey(string(hash))
	data, err := apiKeys.Get(ctx, tokenKey)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if err := apiKeys.Set(ctx, tokenKey, data, apiKeyTTL); err != nil {
		slog.Error("failed renewing API key", "error", err)
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "Could not renew API key."})
		return
	}
	if err := apiKeys.Set(ctx, ownerKey, hash, apiKeyTTL); err != nil {
		slog.Error("failed renewing API key owner", "error", err)
		c.JSON(http.StatusServiceUnavailable, JSONError{Error: "Could not renew API key."})
		return
	}
	c.Status(http.StatusNoContent)
}

func newAPIKeyToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func apiKeyTokenKey(hash string) string {
	return apiKeyPrefix + hash
}

func apiKeyOwnerKey(user string) string {
	return ownerKeyPrefix + hashValue(user)
}

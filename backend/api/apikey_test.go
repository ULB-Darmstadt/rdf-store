package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rdf-store-backend/base"

	"github.com/gin-gonic/gin"
)

type fakeAPIKeyStore struct {
	values   map[string][]byte
	ttls     map[string]time.Duration
	setError func(key string) error
}

func (s *fakeAPIKeyStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return value, nil
}

func (s *fakeAPIKeyStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if s.setError != nil {
		if err := s.setError(key); err != nil {
			return err
		}
	}
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
	return nil
}

func (s *fakeAPIKeyStore) Del(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(s.values, key)
		delete(s.ttls, key)
	}
	return nil
}

func setupAPIKeyTest(t *testing.T) *fakeAPIKeyStore {
	t.Helper()
	oldStore, oldEnabled, oldTTL := apiKeys, apiKeyEnabled, apiKeyTTL
	oldAuthEnabled := base.Configuration.AuthEnabled
	oldWriteGroup := base.AuthWriteAccessGroup
	t.Cleanup(func() {
		apiKeys = oldStore
		apiKeyEnabled = oldEnabled
		apiKeyTTL = oldTTL
		base.Configuration.AuthEnabled = oldAuthEnabled
		base.AuthWriteAccessGroup = oldWriteGroup
	})
	apiKeyTTL = defaultAPIKeyTTL
	base.Configuration.AuthEnabled = true
	base.AuthWriteAccessGroup = ""
	store := &fakeAPIKeyStore{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
	apiKeys = store
	apiKeyEnabled = true
	return store
}

func storeTestAPIKey(t *testing.T, store *fakeAPIKeyStore, token string, identity apiKeyIdentity) {
	t.Helper()
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(t.Context(), apiKeyTokenKey(hashValue(token)), data, apiKeyTTL); err != nil {
		t.Fatal(err)
	}
}

func TestParseAPIKeyTTL(t *testing.T) {
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{value: "", want: defaultAPIKeyTTL},
		{value: "  168h  ", want: 168 * time.Hour},
		{value: "12h", want: 12 * time.Hour},
		{value: "invalid", want: defaultAPIKeyTTL},
		{value: "0s", want: defaultAPIKeyTTL},
		{value: "-1h", want: defaultAPIKeyTTL},
	} {
		if got := parseAPIKeyTTL(test.value); got != test.want {
			t.Errorf("parseAPIKeyTTL(%q) = %s, want %s", test.value, got, test.want)
		}
	}
}

func TestAPIKeyMiddlewareRestoresIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := setupAPIKeyTest(t)
	identity := apiKeyIdentity{User: "alice", Email: "alice@example.org", Groups: "publisher"}
	storeTestAPIKey(t, store, "token", identity)

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set(apiKeyHeader, "token")
	apiKeyMiddleware(c)

	if got := c.Request.Header.Get(base.AuthUserHeader); got != identity.User {
		t.Fatalf("expected user %q, got %q", identity.User, got)
	}
	if got := c.Request.Header.Get(base.AuthEmailHeader); got != identity.Email {
		t.Fatalf("expected email %q, got %q", identity.Email, got)
	}
	if got := c.Request.Header.Get(base.AuthGroupsHeader); got != identity.Groups {
		t.Fatalf("expected groups %q, got %q", identity.Groups, got)
	}
	if !c.GetBool(apiKeyAuthKey) {
		t.Fatal("expected API-key authentication marker")
	}
	granted, user := writeAccessGranted(c.Request.Header)
	if !granted || user != identity.User {
		t.Fatalf("expected write access for %q, got %v/%q", identity.User, granted, user)
	}
}

func TestAPIKeyMiddlewareIgnoresUnknownKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAPIKeyTest(t)

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set(apiKeyHeader, "unknown")
	apiKeyMiddleware(c)

	if got := c.Request.Header.Get(base.AuthUserHeader); got != "" {
		t.Fatalf("expected no user, got %q", got)
	}
}

func TestHandleGenerateAPIKeyRotatesPreviousKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := setupAPIKeyTest(t)

	generate := func() APIKeyResponse {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Request.Header.Set(base.AuthUserHeader, "alice")
		c.Request.Header.Set(base.AuthEmailHeader, "alice@example.org")
		c.Request.Header.Set(base.AuthGroupsHeader, "publisher")
		handleGenerateAPIKey(c)
		if response.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
		}
		var result APIKeyResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := generate()
	firstHash := hashValue(first.APIKey)
	if got := first.ExpiresAt.Sub(time.Now()); got < 6*24*time.Hour || got > apiKeyTTL {
		t.Fatalf("unexpected expiry: %s", got)
	}
	data, ok := store.values[apiKeyTokenKey(firstHash)]
	if !ok {
		t.Fatal("first API key was not stored")
	}
	var identity apiKeyIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	if identity.User != "alice" || identity.Email != "alice@example.org" || identity.Groups != "publisher" {
		t.Fatalf("unexpected stored identity: %#v", identity)
	}
	if store.ttls[apiKeyTokenKey(firstHash)] != apiKeyTTL {
		t.Fatalf("unexpected TTL: %s", store.ttls[apiKeyTokenKey(firstHash)])
	}
	if got := string(store.values[apiKeyOwnerKey("alice")]); got != firstHash {
		t.Fatalf("expected owner to point to %q, got %q", firstHash, got)
	}

	second := generate()
	secondHash := hashValue(second.APIKey)
	if _, ok := store.values[apiKeyTokenKey(firstHash)]; ok {
		t.Fatal("previous API key was not revoked")
	}
	if _, ok := store.values[apiKeyTokenKey(secondHash)]; !ok {
		t.Fatal("new API key was not stored")
	}
}

func TestHandleGenerateAPIKeyRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := setupAPIKeyTest(t)
	token := "generate-token"
	storeTestAPIKey(t, store, token, apiKeyIdentity{User: "alice"})

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set(base.AuthUserHeader, "alice")
	c.Request.Header.Set(apiKeyHeader, token)
	c.Set(apiKeyAuthKey, true)
	handleGenerateAPIKey(c)
	c.Writer.WriteHeaderNow()

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", response.Code, response.Body)
	}
	if len(store.values) != 1 {
		t.Fatalf("expected no new API key, got %d stored values", len(store.values))
	}
	if _, ok := store.values[apiKeyTokenKey(hashValue(token))]; !ok {
		t.Fatal("existing API key was modified")
	}
}

func TestHandleGenerateAPIKeyKeepsPreviousKeyOnOwnerWriteFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := setupAPIKeyTest(t)
	oldToken := "old-token"
	oldHash := hashValue(oldToken)
	storeTestAPIKey(t, store, oldToken, apiKeyIdentity{User: "alice"})
	ownerKey := apiKeyOwnerKey("alice")
	if err := store.Set(t.Context(), ownerKey, []byte(oldHash), apiKeyTTL); err != nil {
		t.Fatal(err)
	}
	store.setError = func(key string) error {
		if key == ownerKey {
			return errors.New("owner write failed")
		}
		return nil
	}

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set(base.AuthUserHeader, "alice")
	handleGenerateAPIKey(c)
	c.Writer.WriteHeaderNow()

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body)
	}
	if _, ok := store.values[apiKeyTokenKey(oldHash)]; !ok {
		t.Fatal("previous API key was revoked before owner write succeeded")
	}
	if got := string(store.values[ownerKey]); got != oldHash {
		t.Fatalf("expected owner to keep pointing to %q, got %q", oldHash, got)
	}
	if len(store.values) != 2 {
		t.Fatalf("expected only previous key and owner to remain, got %d stored values", len(store.values))
	}
}

func TestHandleRenewAPIKeyRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := setupAPIKeyTest(t)
	token := "renew-token"
	hash := hashValue(token)
	storeTestAPIKey(t, store, token, apiKeyIdentity{User: "alice"})
	store.ttls[apiKeyTokenKey(hash)] = time.Hour
	if err := store.Set(t.Context(), apiKeyOwnerKey("alice"), []byte(hash), time.Hour); err != nil {
		t.Fatal(err)
	}

	withAPIKey := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(withAPIKey)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set(base.AuthUserHeader, "alice")
	c.Request.Header.Set(apiKeyHeader, token)
	c.Set(apiKeyAuthKey, true)
	handleRenewAPIKey(c)
	c.Writer.WriteHeaderNow()
	if withAPIKey.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", withAPIKey.Code, withAPIKey.Body)
	}
	if store.ttls[apiKeyTokenKey(hash)] != time.Hour {
		t.Fatal("API key TTL changed despite API-key authentication")
	}

	withSession := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(withSession)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set(base.AuthUserHeader, "alice")
	handleRenewAPIKey(c)
	c.Writer.WriteHeaderNow()
	if withSession.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", withSession.Code, withSession.Body)
	}
	if store.ttls[apiKeyTokenKey(hash)] != apiKeyTTL {
		t.Fatalf("expected renewed TTL %s, got %s", apiKeyTTL, store.ttls[apiKeyTokenKey(hash)])
	}
	if store.ttls[apiKeyOwnerKey("alice")] != apiKeyTTL {
		t.Fatalf("expected renewed owner TTL %s, got %s", apiKeyTTL, store.ttls[apiKeyOwnerKey("alice")])
	}
}

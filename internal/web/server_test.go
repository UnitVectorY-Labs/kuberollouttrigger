package web

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/oidc"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/valkey"

	"github.com/redis/go-redis/v9"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return key
}

func serveJWKS(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()

	nBytes := key.PublicKey.N.Bytes()
	eBytes := big.NewInt(int64(key.PublicKey.E)).Bytes()

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func createSignedToken(t *testing.T, key *rsa.PrivateKey, kid string, claims oidc.Claims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

// mockPublisher is a test double for valkey.Publisher
type mockPublisher struct {
	published []string
	failNext  bool
}

func (m *mockPublisher) Publish(ctx context.Context, message string) error {
	if m.failNext {
		return context.DeadlineExceeded
	}
	m.published = append(m.published, message)
	return nil
}

func TestHandleHealthz(t *testing.T) {
	v := oidc.NewValidator("aud", "org", true, testLogger())
	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected 'ok', got %q", w.Body.String())
	}
}

func TestHandleEvent_MissingAuth(t *testing.T) {
	v := oidc.NewValidator("aud", "org", true, testLogger())
	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	req := httptest.NewRequest("POST", "/event", strings.NewReader(`{"image":"ghcr.io/test/svc","tags":["dev"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleEvent_InvalidContentType(t *testing.T) {
	v := oidc.NewValidator("aud", "org", true, testLogger())
	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	req := httptest.NewRequest("POST", "/event", strings.NewReader(`{"image":"ghcr.io/test/svc","tags":["dev"]}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEvent_WrongOrg(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	jwksSrv := serveJWKS(t, key, kid)

	v := oidc.NewValidator("test-audience", "correct-org", false, testLogger())
	v.SetJWKSURL(jwksSrv.URL)

	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	claims := oidc.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    oidc.GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "wrong-org",
		Repository:      "wrong-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)

	req := httptest.NewRequest("POST", "/event", strings.NewReader(`{"image":"ghcr.io/test/svc","tags":["dev"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleEvent_InvalidPayload(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	jwksSrv := serveJWKS(t, key, kid)

	v := oidc.NewValidator("test-audience", "test-org", false, testLogger())
	v.SetJWKSURL(jwksSrv.URL)

	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	claims := oidc.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    oidc.GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)

	// Wrong prefix
	req := httptest.NewRequest("POST", "/event", strings.NewReader(`{"image":"docker.io/wrong/svc","tags":["dev"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEvent_MethodNotAllowed(t *testing.T) {
	v := oidc.NewValidator("aud", "org", true, testLogger())
	pub := valkey.NewPublisher(&redis.Options{Addr: "localhost:6379"}, "test", testLogger())
	srv := NewServer(v, pub, "ghcr.io/test/", testLogger())

	req := httptest.NewRequest("GET", "/event", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

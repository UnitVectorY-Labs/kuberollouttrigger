package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func createSignedToken(t *testing.T, key *rsa.PrivateKey, kid string, claims Claims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func TestValidateToken_ValidToken(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	srv := serveJWKS(t, key, kid)

	v := NewValidator("test-audience", "test-org", false, testLogger())
	v.jwksURL = srv.URL

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)
	result, err := v.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RepositoryOwner != "test-org" {
		t.Errorf("expected owner test-org, got %s", result.RepositoryOwner)
	}
}

func TestValidateToken_WrongOrg(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	srv := serveJWKS(t, key, kid)

	v := NewValidator("test-audience", "allowed-org", false, testLogger())
	v.jwksURL = srv.URL

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "wrong-org",
		Repository:      "wrong-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)
	_, err := v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong org")
	}
}

func TestValidateToken_WrongAudience(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	srv := serveJWKS(t, key, kid)

	v := NewValidator("expected-audience", "test-org", false, testLogger())
	v.jwksURL = srv.URL

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)
	_, err := v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	srv := serveJWKS(t, key, kid)

	v := NewValidator("test-audience", "test-org", false, testLogger())
	v.jwksURL = srv.URL

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)
	_, err := v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateToken_DevMode(t *testing.T) {
	v := NewValidator("test-audience", "test-org", true, testLogger())

	// Create a token signed with a random key (no JWKS needed in dev mode)
	key := generateTestKey(t)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, "random-kid", claims)
	result, err := v.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error in dev mode: %v", err)
	}
	if result.RepositoryOwner != "test-org" {
		t.Errorf("expected owner test-org, got %s", result.RepositoryOwner)
	}
}

func TestValidateToken_DevMode_WrongOrg(t *testing.T) {
	v := NewValidator("test-audience", "allowed-org", true, testLogger())

	key := generateTestKey(t)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "wrong-org",
		Repository:      "wrong-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, "random-kid", claims)
	_, err := v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong org in dev mode")
	}
}

func TestValidateToken_CaseInsensitiveOrg(t *testing.T) {
	key := generateTestKey(t)
	kid := "test-key-1"
	srv := serveJWKS(t, key, kid)

	v := NewValidator("test-audience", "Test-Org", false, testLogger())
	v.jwksURL = srv.URL

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    GitHubOIDCIssuer,
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		RepositoryOwner: "test-org",
		Repository:      "test-org/test-repo",
	}

	tokenStr := createSignedToken(t, key, kid, claims)
	result, err := v.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RepositoryOwner != "test-org" {
		t.Errorf("expected owner test-org, got %s", result.RepositoryOwner)
	}
}

func TestFetchJWKS_InvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v := NewValidator("test-audience", "test-org", false, testLogger())
	v.jwksURL = srv.URL

	_, err := v.fetchJWKS()
	if err == nil {
		t.Fatal("expected error for invalid JWKS response")
	}
}

func TestFetchJWKS_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	v := NewValidator("test-audience", "test-org", false, testLogger())
	v.jwksURL = srv.URL

	_, err := v.fetchJWKS()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

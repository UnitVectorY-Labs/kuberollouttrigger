package oidc

import (
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// GitHubOIDCIssuer is the OIDC issuer URL for GitHub Actions.
	GitHubOIDCIssuer = "https://token.actions.githubusercontent.com"

	// jwksCacheTTL is how long JWKS keys are cached.
	jwksCacheTTL = 1 * time.Hour
)

// Validator validates GitHub Actions OIDC tokens.
type Validator struct {
	audience   string
	allowedOrg string
	devMode    bool
	logger     *slog.Logger

	// httpClient is the HTTP client for fetching JWKS.
	httpClient *http.Client

	// jwksURL is the URL to fetch JWKS keys from.
	jwksURL string

	mu          sync.RWMutex
	cachedKeys  map[string]crypto.PublicKey
	cachedUntil time.Time
}

// NewValidator creates a new OIDC token validator.
func NewValidator(audience, allowedOrg string, devMode bool, logger *slog.Logger) *Validator {
	return &Validator{
		audience:   audience,
		allowedOrg: allowedOrg,
		devMode:    devMode,
		logger:     logger,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		jwksURL:    GitHubOIDCIssuer + "/.well-known/jwks",
	}
}

// Claims represents the relevant claims from a GitHub Actions OIDC token.
type Claims struct {
	jwt.RegisteredClaims
	RepositoryOwner string `json:"repository_owner"`
	Repository      string `json:"repository"`
}

// TokenInspection contains unverified, safe-to-log token metadata.
type TokenInspection struct {
	HeaderAlg       string
	HeaderKID       string
	Issuer          string
	Audience        []string
	RepositoryOwner string
	Repository      string
	ParseError      string
}

// SetJWKSURL overrides the JWKS URL (for testing).
func (v *Validator) SetJWKSURL(url string) {
	v.jwksURL = url
}

// Audience returns the expected audience value.
func (v *Validator) Audience() string {
	return v.audience
}

// AllowedOrg returns the configured allowed GitHub organization.
func (v *Validator) AllowedOrg() string {
	return v.allowedOrg
}

// InspectToken parses token header and claims without signature verification.
// It is intended only for diagnostics and logging.
func InspectToken(tokenString string) TokenInspection {
	var claims Claims
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(tokenString, &claims)
	if err != nil {
		return TokenInspection{
			ParseError: err.Error(),
		}
	}

	inspection := TokenInspection{
		Issuer:          claims.Issuer,
		Audience:        []string(claims.Audience),
		RepositoryOwner: claims.RepositoryOwner,
		Repository:      claims.Repository,
	}
	if token != nil {
		if alg, ok := token.Header["alg"].(string); ok {
			inspection.HeaderAlg = alg
		}
		if kid, ok := token.Header["kid"].(string); ok {
			inspection.HeaderKID = kid
		}
	}

	return inspection
}

// ValidateToken validates the given JWT token string and returns the parsed claims.
func (v *Validator) ValidateToken(tokenString string) (*Claims, error) {
	parserOpts := []jwt.ParserOption{
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(GitHubOIDCIssuer),
		jwt.WithExpirationRequired(),
	}

	var claims Claims
	var token *jwt.Token
	var err error

	if v.devMode {
		// In dev mode, parse without signature verification
		parser := jwt.NewParser(append(parserOpts,
			jwt.WithoutClaimsValidation(),
		)...)
		token, _, err = parser.ParseUnverified(tokenString, &claims)
		if err != nil {
			return nil, fmt.Errorf("failed to parse token: %w", err)
		}
	} else {
		// Production mode: verify signature using JWKS
		token, err = jwt.ParseWithClaims(tokenString, &claims, v.keyFunc, parserOpts...)
		if err != nil {
			return nil, fmt.Errorf("token validation failed: %w", err)
		}
	}

	if !token.Valid && !v.devMode {
		return nil, fmt.Errorf("invalid token")
	}

	// Enforce organization restriction
	if !strings.EqualFold(claims.RepositoryOwner, v.allowedOrg) {
		return nil, fmt.Errorf("token organization %q does not match allowed org %q", claims.RepositoryOwner, v.allowedOrg)
	}

	return &claims, nil
}

func (v *Validator) keyFunc(token *jwt.Token) (interface{}, error) {
	// Ensure the signing method is RSA
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}

	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("token header missing kid")
	}

	keys, err := v.getKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	key, ok := keys[kid]
	if !ok {
		// Try refreshing the cache in case keys rotated
		v.mu.Lock()
		v.cachedUntil = time.Time{}
		v.mu.Unlock()

		keys, err = v.getKeys()
		if err != nil {
			return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
		}
		key, ok = keys[kid]
		if !ok {
			return nil, fmt.Errorf("key %q not found in JWKS", kid)
		}
	}

	return key, nil
}

func (v *Validator) getKeys() (map[string]crypto.PublicKey, error) {
	v.mu.RLock()
	if v.cachedKeys != nil && time.Now().Before(v.cachedUntil) {
		keys := v.cachedKeys
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock
	if v.cachedKeys != nil && time.Now().Before(v.cachedUntil) {
		return v.cachedKeys, nil
	}

	keys, err := v.fetchJWKS()
	if err != nil {
		return nil, err
	}

	v.cachedKeys = keys
	v.cachedUntil = time.Now().Add(jwksCacheTTL)
	v.logger.Debug("refreshed JWKS cache", "key_count", len(keys))

	return keys, nil
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KID string `json:"kid"`
	KTY string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *Validator) fetchJWKS() (map[string]crypto.PublicKey, error) {
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	keys := make(map[string]crypto.PublicKey)
	for _, k := range jwks.Keys {
		if k.KTY != "RSA" {
			continue
		}

		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			v.logger.Warn("failed to decode JWK modulus", "kid", k.KID, "error", err)
			continue
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			v.logger.Warn("failed to decode JWK exponent", "kid", k.KID, "error", err)
			continue
		}

		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}

		keys[k.KID] = &rsa.PublicKey{N: n, E: e}
	}

	return keys, nil
}

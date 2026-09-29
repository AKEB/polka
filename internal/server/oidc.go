package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	oidcStateCookie = "polka_oidc_state"
	oidcNonceCookie = "polka_oidc_nonce"
	oidcCookieTTL   = 10 * time.Minute
)

type oidcAuth struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
	name     string
}

// oidcKeySet verifies asymmetric signatures via the provider JWKS and
// HS* signatures with the OAuth client secret (common for Authentik et al.).
type oidcKeySet struct {
	remote oidc.KeySet
	secret []byte
}

func (k *oidcKeySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	alg, err := jwtSigningAlg(jwt)
	if err != nil {
		return nil, err
	}
	switch alg {
	case "HS256", "HS384", "HS512":
		if len(k.secret) == 0 {
			return nil, fmt.Errorf("oidc: %s id_token requires a client secret", alg)
		}
		jws, err := jose.ParseSigned(jwt, []jose.SignatureAlgorithm{
			jose.HS256, jose.HS384, jose.HS512,
		})
		if err != nil {
			return nil, err
		}
		return jws.Verify(k.secret)
	default:
		if k.remote == nil {
			return nil, fmt.Errorf("oidc: no JWKS to verify %s", alg)
		}
		return k.remote.VerifySignature(ctx, jwt)
	}
}

func jwtSigningAlg(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("oidc: malformed jwt")
	}
	hdr, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("oidc: malformed jwt header: %w", err)
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(hdr, &h); err != nil || h.Alg == "" {
		return "", fmt.Errorf("oidc: jwt header missing alg")
	}
	return h.Alg, nil
}

func (s *Server) initOIDC(ctx context.Context) {
	if !s.cfg.OIDCEnabled() {
		return
	}
	provider, err := oidc.NewProvider(ctx, s.cfg.OIDCIssuer)
	if err != nil {
		s.log.Error("oidc provider discovery failed", "issuer", s.cfg.OIDCIssuer, "error", err)
		return
	}
	var meta struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := provider.Claims(&meta); err != nil || meta.Issuer == "" || meta.JWKSURI == "" {
		s.log.Error("oidc provider metadata incomplete", "error", err, "issuer", meta.Issuer, "jwks", meta.JWKSURI)
		return
	}
	name := strings.TrimSpace(s.cfg.OIDCName)
	if name == "" {
		name = "SSO"
	}
	// go-oidc filters HS* out of discovery; Authentik and similar IdPs still
	// issue HS256 id_tokens signed with the client secret.
	algs := []string{
		oidc.RS256, oidc.RS384, oidc.RS512,
		oidc.ES256, oidc.ES384, oidc.ES512,
		oidc.PS256, oidc.PS384, oidc.PS512,
		oidc.EdDSA,
		"HS256", "HS384", "HS512",
	}
	keySet := &oidcKeySet{
		remote: oidc.NewRemoteKeySet(ctx, meta.JWKSURI),
		secret: []byte(s.cfg.OIDCClientSecret),
	}
	s.oidc = &oidcAuth{
		provider: provider,
		verifier: oidc.NewVerifier(meta.Issuer, keySet, &oidc.Config{
			ClientID:             s.cfg.OIDCClientID,
			SupportedSigningAlgs: algs,
		}),
		oauth2: oauth2.Config{
			ClientID:     s.cfg.OIDCClientID,
			ClientSecret: s.cfg.OIDCClientSecret,
			RedirectURL:  s.cfg.OIDCRedirect(),
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		name: name,
	}
	s.log.Info("oidc enabled", "issuer", s.cfg.OIDCIssuer, "redirect", s.cfg.OIDCRedirect(), "name", name)
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	if !s.loginLimiter.allow(clientIP(r)) {
		http.Error(w, "too many login attempts, try again later", http.StatusTooManyRequests)
		return
	}
	state, err := randomOIDCToken()
	if err != nil {
		s.apiError(w, err)
		return
	}
	nonce, err := randomOIDCToken()
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.setOIDCCookie(w, r, oidcStateCookie, state)
	s.setOIDCCookie(w, r, oidcNonceCookie, nonce)
	url := s.oidc.oauth2.AuthCodeURL(state, oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		s.log.Warn("oidc callback error", "error", errParam, "description", desc)
		http.Error(w, "OIDC login failed: "+errParam, http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OIDC state", http.StatusBadRequest)
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		http.Error(w, "missing OIDC nonce", http.StatusBadRequest)
		return
	}
	// One-time cookies.
	s.setOIDCCookie(w, r, oidcStateCookie, "")
	s.setOIDCCookie(w, r, oidcNonceCookie, "")

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	oauth2Token, err := s.oidc.oauth2.Exchange(r.Context(), code)
	if err != nil {
		s.log.Warn("oidc token exchange", "error", err)
		http.Error(w, "OIDC token exchange failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "OIDC response missing id_token", http.StatusBadGateway)
		return
	}
	idToken, err := s.oidc.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		s.log.Warn("oidc id_token verify", "error", err)
		http.Error(w, "invalid OIDC id_token", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != nonceCookie.Value {
		http.Error(w, "invalid OIDC nonce", http.StatusBadRequest)
		return
	}

	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid OIDC claims", http.StatusBadGateway)
		return
	}

	login := claims.PreferredUsername
	if login == "" {
		login = claims.Email
	}
	display := claims.Name
	if display == "" {
		display = claims.PreferredUsername
	}
	if display == "" {
		display = claims.Email
	}

	u, err := s.users.FindOrCreateOIDC(r.Context(), idToken.Issuer, idToken.Subject, login, display)
	if err != nil {
		s.log.Warn("oidc find-or-create", "error", err)
		http.Error(w, "could not create user", http.StatusForbidden)
		return
	}
	token, err := s.users.CreateSession(r.Context(), u.ID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.setSessionCookie(w, r, token, int((7 * 24 * time.Hour).Seconds()))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) setOIDCCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	maxAge := int(oidcCookieTTL.Seconds())
	if value == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/oidc",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func randomOIDCToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Handler exposes login, callback, current user and local logout on the native listener.
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", m.login)
	mux.HandleFunc("GET /auth/callback", m.callback)
	mux.HandleFunc("GET /auth/me", m.me)
	mux.HandleFunc("POST /auth/logout", m.logout)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !m.OIDCEnabled() {
			jsonError(w, 404, "OIDC is not configured")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (m *Manager) login(w http.ResponseWriter, r *http.Request) {
	token, state, nonce, verifier := secret(), secret(), secret(), oauth2.GenerateVerifier()
	flow := Flow{TokenHash: digest(token), StateHash: digest(state), Nonce: nonce, Verifier: verifier, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err := m.store.SaveFlow(r.Context(), flow); err != nil {
		internalError(w, err)
		return
	}
	m.setCookie(w, m.flowCookie, token, flow.ExpiresAt, 600)
	http.Redirect(w, r, m.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}
func (m *Manager) callback(w http.ResponseWriter, r *http.Request) {
	flowToken := cookieToken(r, m.flowCookie)
	m.clearCookie(w, m.flowCookie)
	if flowToken == "" {
		jsonError(w, 400, "invalid login transaction")
		return
	}
	flow, err := m.store.ConsumeFlow(r.Context(), digest(flowToken))
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			jsonError(w, 400, "invalid login transaction")
		} else {
			internalError(w, err)
		}
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" || !equal(digest(state), flow.StateHash) {
		jsonError(w, 400, "invalid login state")
		return
	}
	if r.URL.Query().Get("error") != "" {
		jsonError(w, 400, "identity provider rejected login")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		jsonError(w, 400, "missing authorization code")
		return
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(r.Context(), m.client), 15*time.Second)
	defer cancel()
	token, err := m.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		jsonError(w, 401, "OIDC code exchange failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		jsonError(w, 401, "identity provider did not return an ID token")
		return
	}
	idToken, err := m.verifier.Verify(ctx, raw)
	if err != nil || idToken.Subject == "" || !equal(idToken.Nonce, flow.Nonce) {
		jsonError(w, 401, "invalid ID token")
		return
	}
	var claims struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err = idToken.Claims(&claims); err != nil {
		jsonError(w, 401, "invalid ID token claims")
		return
	}
	// Issuer+subject is the identity. Email is display metadata, never an account-linking key.
	user := User{ID: digest(idToken.Issuer + "\x00" + idToken.Subject), Issuer: idToken.Issuer, Subject: idToken.Subject, Name: claims.Name, Email: claims.Email}
	sessionToken := secret()
	session := Session{TokenHash: digest(sessionToken), CSRF: secret(), User: user, ExpiresAt: time.Now().Add(12 * time.Hour)}
	if err = m.store.CreateSession(r.Context(), session); err != nil {
		internalError(w, err)
		return
	}
	m.setCookie(w, m.sessionCookie, sessionToken, session.ExpiresAt, 12*60*60)
	http.Redirect(w, r, "/auth/me", http.StatusSeeOther)
}
func (m *Manager) me(w http.ResponseWriter, r *http.Request) {
	session, err := m.browserSession(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		User      User      `json:"user"`
		CSRF      string    `json:"csrf_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}{session.User, session.CSRF, session.ExpiresAt})
}
func (m *Manager) logout(w http.ResponseWriter, r *http.Request) {
	session, err := m.browserSession(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !m.validCSRF(r, session) {
		jsonError(w, 403, "invalid CSRF token or origin")
		return
	}
	if err = m.store.DeleteSession(r.Context(), session.TokenHash); err != nil {
		internalError(w, err)
		return
	}
	m.clearCookie(w, m.sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}
func sessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrUnauthenticated) {
		jsonError(w, 401, "unauthorized")
		return
	}
	internalError(w, err)
}
func internalError(w http.ResponseWriter, err error) {
	slog.Error("authentication storage operation failed", "error", err)
	jsonError(w, 500, "authentication unavailable")
}
func jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

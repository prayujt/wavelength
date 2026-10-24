package auth

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Browser session owners can inspect/revoke their delegations without possessing
// an AI client's refresh token. Returned records never contain credential hashes.
func (m *Manager) connections(w http.ResponseWriter, r *http.Request) {
	if !m.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	session, err := m.browserSession(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	records, err := m.oauthStore.List(r.Context(), "oauth/grants")
	if err != nil {
		internalError(w, err)
		return
	}
	type connection struct {
		ID        string    `json:"id"`
		Client    string    `json:"client_name"`
		Scopes    []string  `json:"scopes"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	items := []connection{}
	for id, raw := range records {
		var g oauthGrant
		if err = json.Unmarshal(raw, &g); err != nil {
			internalError(w, err)
			return
		}
		if g.User.ID != session.User.ID || g.User.Issuer != session.User.Issuer || g.Resource != m.ResourceURL() || !time.Now().Before(g.ExpiresAt) {
			continue
		}
		c, err := m.oauthClient(r.Context(), g.ClientID)
		if err != nil {
			continue
		}
		items = append(items, connection{ID: id, Client: c.Name, Scopes: g.Scopes, ExpiresAt: g.ExpiresAt})
	}
	slices.SortFunc(items, func(a, b connection) int { return strings.Compare(a.ID, b.ID) })
	writeJSON(w, 200, map[string]any{"connections": items})
}
func (m *Manager) revokeConnection(w http.ResponseWriter, r *http.Request) {
	if !m.OAuthEnabled() {
		http.NotFound(w, r)
		return
	}
	session, err := m.browserSession(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !m.validCSRF(r, session) {
		jsonError(w, 403, "invalid CSRF token or origin")
		return
	}
	err = m.oauthStore.Update(r.Context(), "oauth/grants", r.PathValue("id"), func(raw json.RawMessage) (json.RawMessage, error) {
		var g oauthGrant
		if len(raw) == 0 {
			return nil, ErrUnauthenticated
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, err
		}
		if g.User.ID != session.User.ID || g.User.Issuer != session.User.Issuer {
			return nil, ErrUnauthenticated
		}
		return nil, nil
	})
	if err != nil {
		jsonError(w, 404, "connection not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

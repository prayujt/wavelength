package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/state"
	"golang.org/x/oauth2"
)

const (
	ScopeRead   = "wavelength:read"
	ScopeWrite  = "wavelength:write"
	ScopeStream = "wavelength:stream"
)

var oauthScopes = []string{ScopeRead, ScopeWrite, ScopeStream}
var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// Wavelength is the resource and a small OAuth authorization server. OIDC remains
// the sole source of user identity/access policy. Downstream clients never receive
// provider credentials or tokens and need no separate identity-provider app.
type oauthClient struct {
	ID         string    `json:"client_id"`
	Name       string    `json:"client_name"`
	Redirects  []string  `json:"redirect_uris"`
	Method     string    `json:"token_endpoint_auth_method"`
	SecretHash string    `json:"secret_hash,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type authorization struct {
	ClientID, Redirect, State, Challenge, Resource string
	SessionHash                                    string
	Scopes                                         []string
	ExpiresAt                                      time.Time `json:"expires_at"`
}
type authorizationCode struct {
	Authorization authorization
	User          User
	ExpiresAt     time.Time `json:"expires_at"`
}
type oauthGrant struct {
	ClientID, Resource, AccessHash, RefreshHash string
	User                                        User
	Scopes                                      []string
	AccessExpiresAt                             time.Time
	ExpiresAt                                   time.Time `json:"expires_at"`
	UsedRefreshHashes                           []string
}
type oauthContextKey struct{}

func (m *Manager) OAuthEnabled() bool  { return m.OIDCEnabled() && m.oauthStore != nil }
func (m *Manager) ResourceURL() string { return m.origin + "/mcp" }
func (m *Manager) MetadataURL() string { return m.origin + "/.well-known/oauth-protected-resource/mcp" }
func (m *Manager) oauthCookie() string {
	if m.secure {
		return "__Host-wavelength_oauth"
	}
	return "wavelength_oauth"
}

// OAuthHandler serves discovery, registration, authorization, token exchange and
// revocation. It is mounted on the existing native listener only when OIDC is on.
func (m *Manager) OAuthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", m.authorizationMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", m.resourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", m.resourceMetadata)
	mux.HandleFunc("POST /oauth/register", m.registerClient)
	mux.HandleFunc("GET /oauth/authorize", m.oauthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", m.oauthConsent)
	mux.HandleFunc("POST /oauth/token", m.oauthToken)
	mux.HandleFunc("POST /oauth/revoke", m.oauthRevoke)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !m.OAuthEnabled() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/.well-known/") || r.URL.Path == "/oauth/token" || r.URL.Path == "/oauth/register" || r.URL.Path == "/oauth/revoke" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if !strings.HasPrefix(r.URL.Path, "/.well-known/") {
			// A global budget avoids trusting unverified proxy/IP headers.
			if !m.oauthLimit.Allow() {
				w.Header().Set("Retry-After", "1")
				oauthError(w, 429, "temporarily_unavailable")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		mux.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
func (m *Manager) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"issuer": m.origin, "authorization_endpoint": m.origin + "/oauth/authorize",
		"token_endpoint": m.origin + "/oauth/token", "registration_endpoint": m.origin + "/oauth/register",
		"revocation_endpoint": m.origin + "/oauth/revoke", "response_types_supported": []string{"code"},
		"grant_types_supported":            []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported": []string{"S256"}, "scopes_supported": oauthScopes,
		"token_endpoint_auth_methods_supported":          []string{"none", "client_secret_basic", "client_secret_post"},
		"authorization_response_iss_parameter_supported": true,
	})
}
func (m *Manager) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"resource": m.ResourceURL(), "authorization_servers": []string{m.origin},
		"scopes_supported": oauthScopes, "bearer_methods_supported": []string{"header"},
		"resource_name": "Wavelength",
	})
}

// Expiring records are bounded and cleaned when creating a client/flow/grant.
// This uses application-record transactions; raw credentials are never persisted.
func (m *Manager) room(ctx context.Context, scope string) error {
	records, err := m.oauthStore.List(ctx, scope)
	if err != nil {
		return err
	}
	count := 0
	for key, raw := range records {
		var entry struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err = json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if !time.Now().Before(entry.ExpiresAt) {
			if err = state.Delete(ctx, m.oauthStore, scope, key); err != nil {
				return err
			}
		} else {
			count++
		}
	}
	if count >= 4096 {
		return errors.New("authorization capacity reached")
	}
	return nil
}
func (m *Manager) oauthClient(ctx context.Context, id string) (oauthClient, error) {
	c, err := state.Read[oauthClient](ctx, m.oauthStore, "oauth/clients", id)
	if err != nil {
		return c, err
	}
	if !time.Now().Before(c.ExpiresAt) {
		return c, ErrUnauthenticated
	}
	return c, nil
}
func validRedirect(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Fragment != "" || u.RawFragment != "" {
		return false
	}
	// validatedURL also forbids credentials, opaque URIs and non-loopback HTTP.
	u.RawQuery = ""
	_, err = validatedURL(u.String(), false)
	return err == nil
}
func (m *Manager) registerClient(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
		Method    string   `json:"token_endpoint_auth_method"`
		Grants    []string `json:"grant_types"`
		Responses []string `json:"response_types"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil || len(input.Redirects) == 0 || len(input.Redirects) > 10 || len(input.Name) > 200 {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	for _, redirect := range input.Redirects {
		if len(redirect) > 2048 || !validRedirect(redirect) {
			oauthError(w, 400, "invalid_redirect_uri")
			return
		}
	}
	for _, grant := range input.Grants {
		if grant != "authorization_code" && grant != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	for _, response := range input.Responses {
		if response != "code" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	if input.Method == "" {
		input.Method = "client_secret_basic"
	}
	if input.Method != "none" && input.Method != "client_secret_basic" && input.Method != "client_secret_post" {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	if err := m.room(r.Context(), "oauth/clients"); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	c := oauthClient{ID: secret(), Name: input.Name, Redirects: input.Redirects, Method: input.Method, ExpiresAt: time.Now().Add(365 * 24 * time.Hour)}
	if c.Name == "" {
		c.Name = "Unnamed client"
	}
	response := map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.Redirects,
		"token_endpoint_auth_method": c.Method, "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "client_id_issued_at": time.Now().Unix()}
	if c.Method != "none" {
		credential := secret()
		c.SecretHash = digest(credential)
		response["client_secret"] = credential
		response["client_secret_expires_at"] = c.ExpiresAt.Unix()
	}
	if err := state.Write(r.Context(), m.oauthStore, "oauth/clients", c.ID, c); err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	writeJSON(w, 201, response)
}
func scopes(value string) ([]string, bool) {
	if value == "" {
		return []string{ScopeRead, ScopeWrite}, true
	}
	result := []string{}
	for _, scope := range strings.Fields(value) {
		if !slices.Contains(oauthScopes, scope) {
			return nil, false
		}
		if !slices.Contains(result, scope) {
			result = append(result, scope)
		}
	}
	return result, slices.Contains(result, ScopeRead)
}
func single(values url.Values) bool {
	for _, v := range values {
		if len(v) != 1 {
			return false
		}
	}
	return true
}
func (m *Manager) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if id := q.Get("request"); id != "" {
		m.showConsent(w, r, id)
		return
	}
	c, err := m.oauthClient(r.Context(), q.Get("client_id"))
	if err != nil || !single(q) || !slices.Contains(c.Redirects, q.Get("redirect_uri")) {
		oauthError(w, 400, "invalid_request")
		return
	}
	challenge, err := base64.RawURLEncoding.DecodeString(q.Get("code_challenge"))
	requested, ok := scopes(q.Get("scope"))
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || err != nil || len(challenge) != 32 || len(q.Get("code_challenge")) != 43 || q.Get("state") == "" || len(q.Get("state")) > 2048 {
		oauthError(w, 400, "invalid_request")
		return
	}
	if !ok {
		oauthError(w, 400, "invalid_scope")
		return
	}
	resource := q.Get("resource")
	if resource == "" {
		resource = m.ResourceURL()
	} // Older OAuth clients omit RFC 8707.
	if resource != m.ResourceURL() {
		oauthError(w, 400, "invalid_target")
		return
	}
	if err = m.room(r.Context(), "oauth/requests"); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	token := secret()
	id := digest(token)
	pending := authorization{ClientID: c.ID, Redirect: q.Get("redirect_uri"), State: q.Get("state"), Challenge: q.Get("code_challenge"), Resource: resource, Scopes: requested, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err = state.Write(r.Context(), m.oauthStore, "oauth/requests", id, pending); err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	m.setCookie(w, m.oauthCookie(), token, pending.ExpiresAt, 600)
	// Always revisit the same provider application, so its current group policies
	// are checked before every new delegated grant, even with an old local session.
	m.startLogin(w, r, "/oauth/authorize?request="+id)
}
func (m *Manager) pending(r *http.Request, id string) (authorization, Session, error) {
	var a authorization
	if id == "" || !equal(id, digest(cookieToken(r, m.oauthCookie()))) {
		return a, Session{}, ErrUnauthenticated
	}
	a, err := state.Read[authorization](r.Context(), m.oauthStore, "oauth/requests", id)
	if err != nil || !time.Now().Before(a.ExpiresAt) {
		return a, Session{}, ErrUnauthenticated
	}
	session, err := m.browserSession(r)
	if err == nil && (a.SessionHash == "" || !equal(a.SessionHash, session.TokenHash)) {
		err = ErrUnauthenticated
	}
	return a, session, err
}

// Complete only the request bound to this browser's OIDC flow. Merely starting an
// authorization request with an old local session cannot bypass upstream policy.
func (m *Manager) resumeOAuth(r *http.Request, returnTo string, session Session) error {
	u, err := url.Parse(returnTo)
	if err != nil || !m.OAuthEnabled() || u.Path != "/oauth/authorize" {
		return ErrUnauthenticated
	}
	id := u.Query().Get("request")
	if id == "" || !equal(id, digest(cookieToken(r, m.oauthCookie()))) {
		return ErrUnauthenticated
	}
	return state.Modify(r.Context(), m.oauthStore, "oauth/requests", id, func(a *authorization, exists bool) error {
		if !exists || !time.Now().Before(a.ExpiresAt) {
			return ErrUnauthenticated
		}
		a.SessionHash = session.TokenHash
		return nil
	})
}

func scopeLabel(scope string) string {
	switch scope {
	case ScopeRead:
		return "Browse the music library and read your playlists and listening activity."
	case ScopeWrite:
		return "Create and change your playlists, playback queue, bookmarks and listening history."
	case ScopeStream:
		return "Stream audio files from the music library."
	default:
		return scope
	}
}

var consentPage = template.Must(template.New("consent").Funcs(template.FuncMap{"scopeLabel": scopeLabel}).Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Authorize Wavelength</title><body><h1>Connect to Wavelength</h1><p>Signed in as {{.User.Name}} ({{.User.Email}}).</p><p>Application: <strong>{{.Client.Name}}</strong>. This client name is supplied by the application and is not verified.</p><p>Return address: <code>{{.Redirect}}</code></p><p>Requested permissions:</p><ul>{{range .Scopes}}<li>{{scopeLabel .}}</li>{{end}}</ul><p>Only approve a connection you started. This grants access to the shared library and your own collections. Audio streaming requires its separate permission.</p><form method="post" action="/oauth/authorize"><input type="hidden" name="request" value="{{.ID}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><button name="decision" value="allow">Allow</button> <button name="decision" value="deny">Deny</button></form></body></html>`))

func (m *Manager) showConsent(w http.ResponseWriter, r *http.Request, id string) {
	a, session, err := m.pending(r, id)
	if err != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	c, err := m.oauthClient(r.Context(), a.ClientID)
	if err != nil {
		oauthError(w, 400, "invalid_client")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	_ = consentPage.Execute(w, struct {
		User     User
		Client   oauthClient
		Redirect string
		Scopes   []string
		ID, CSRF string
	}{session.User, c, a.Redirect, a.Scopes, id, session.CSRF})
}
func (m *Manager) oauthConsent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || !single(r.PostForm) {
		oauthError(w, 400, "invalid_request")
		return
	}
	id := r.PostForm.Get("request")
	a, session, err := m.pending(r, id)
	if err != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	if r.Header.Get("Origin") != m.origin || !equal(r.PostForm.Get("csrf"), session.CSRF) {
		oauthError(w, 403, "access_denied")
		return
	}
	decision := r.PostForm.Get("decision")
	if decision != "allow" && decision != "deny" {
		oauthError(w, 400, "invalid_request")
		return
	}
	// Consume atomically; parallel form submissions cannot mint multiple codes.
	err = m.oauthStore.Update(r.Context(), "oauth/requests", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, ErrUnauthenticated
		}
		return nil, nil
	})
	if err != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	m.clearCookie(w, m.oauthCookie())
	u, _ := url.Parse(a.Redirect)
	q := u.Query()
	q.Set("state", a.State)
	q.Set("iss", m.origin)
	if decision == "deny" {
		q.Set("error", "access_denied")
	} else {
		if err = m.room(r.Context(), "oauth/codes"); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		code := secret()
		record := authorizationCode{Authorization: a, User: session.User, ExpiresAt: time.Now().Add(time.Minute)}
		if err = state.Write(r.Context(), m.oauthStore, "oauth/codes", digest(code), record); err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		q.Set("code", code)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func (m *Manager) authenticateClient(r *http.Request) (oauthClient, error) {
	id, credential, method := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret"), "none"
	if credential != "" {
		method = "client_secret_post"
	}
	if r.Header.Get("Authorization") != "" {
		basicID, basicSecret, ok := r.BasicAuth()
		if !ok || credential != "" {
			return oauthClient{}, ErrUnauthenticated
		}
		var err error
		basicID, err = url.QueryUnescape(basicID)
		if err != nil {
			return oauthClient{}, ErrUnauthenticated
		}
		basicSecret, err = url.QueryUnescape(basicSecret)
		if err != nil || id != "" && id != basicID {
			return oauthClient{}, ErrUnauthenticated
		}
		id, credential, method = basicID, basicSecret, "client_secret_basic"
	}
	c, err := m.oauthClient(r.Context(), id)
	if err != nil || method != c.Method || c.SecretHash != "" && !equal(c.SecretHash, digest(credential)) {
		return c, ErrUnauthenticated
	}
	return c, nil
}
func (m *Manager) oauthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || !single(r.PostForm) {
		oauthError(w, 400, "invalid_request")
		return
	}
	c, err := m.authenticateClient(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Basic")
		oauthError(w, 401, "invalid_client")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		m.exchangeCode(w, r, c)
	case "refresh_token":
		m.refreshToken(w, r, c)
	default:
		oauthError(w, 400, "unsupported_grant_type")
	}
}
func (m *Manager) exchangeCode(w http.ResponseWriter, r *http.Request, c oauthClient) {
	var record authorizationCode
	err := m.oauthStore.Update(r.Context(), "oauth/codes", digest(r.PostForm.Get("code")), func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, ErrUnauthenticated
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		a := record.Authorization
		verifier := r.PostForm.Get("code_verifier")
		if !time.Now().Before(record.ExpiresAt) || a.ClientID != c.ID || a.Redirect != r.PostForm.Get("redirect_uri") || !pkceVerifier.MatchString(verifier) || !equal(oauth2.S256ChallengeFromVerifier(verifier), a.Challenge) || record.User.Issuer != m.config.Issuer || a.Resource != m.ResourceURL() {
			return nil, ErrUnauthenticated
		}
		if resource := r.PostForm.Get("resource"); resource != "" && resource != a.Resource {
			return nil, ErrUnauthenticated
		}
		return nil, nil
	})
	if err != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	if err = m.room(r.Context(), "oauth/grants"); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	a := record.Authorization
	g := oauthGrant{ClientID: c.ID, Resource: a.Resource, User: record.User, Scopes: a.Scopes, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
	m.issueGrant(w, r, secret(), g)
}
func newGrantTokens(id string, g *oauthGrant) (string, string) {
	access, refresh := "wla_"+id+"."+secret(), "wlr_"+id+"."+secret()
	g.AccessHash, g.RefreshHash = digest(access), digest(refresh)
	g.AccessExpiresAt = time.Now().Add(time.Hour)
	if g.AccessExpiresAt.After(g.ExpiresAt) {
		g.AccessExpiresAt = g.ExpiresAt
	}
	return access, refresh
}
func tokenReply(w http.ResponseWriter, g oauthGrant, access, refresh string) {
	writeJSON(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": int64(time.Until(g.AccessExpiresAt).Seconds()), "scope": strings.Join(g.Scopes, " ")})
}
func (m *Manager) issueGrant(w http.ResponseWriter, r *http.Request, id string, g oauthGrant) {
	access, refresh := newGrantTokens(id, &g)
	if err := state.Write(r.Context(), m.oauthStore, "oauth/grants", id, g); err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	tokenReply(w, g, access, refresh)
}
func grantID(token, prefix string) string {
	if !strings.HasPrefix(token, prefix) || len(token) > 256 {
		return ""
	}
	id, credential, ok := strings.Cut(strings.TrimPrefix(token, prefix), ".")
	if !ok || len(id) < 26 || len(id) > 128 || len(credential) < 26 || len(credential) > 128 {
		return ""
	}
	return id
}
func (m *Manager) refreshToken(w http.ResponseWriter, r *http.Request, c oauthClient) {
	token := r.PostForm.Get("refresh_token")
	id := grantID(token, "wlr_")
	if id == "" {
		oauthError(w, 400, "invalid_grant")
		return
	}
	var g oauthGrant
	var access, refresh string
	replayed := false
	err := m.oauthStore.Update(r.Context(), "oauth/grants", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, ErrUnauthenticated
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, err
		}
		if g.ClientID == c.ID && slices.Contains(g.UsedRefreshHashes, digest(token)) {
			replayed = true
			return nil, nil // Known rotated credential: revoke its entire family.
		}
		if !equal(g.RefreshHash, digest(token)) || g.ClientID != c.ID || !time.Now().Before(g.ExpiresAt) || g.User.Issuer != m.config.Issuer || g.Resource != m.ResourceURL() {
			return nil, ErrUnauthenticated
		}
		if resource := r.PostForm.Get("resource"); resource != "" && resource != g.Resource {
			return nil, ErrUnauthenticated
		}
		if scope := r.PostForm.Get("scope"); scope != "" {
			requested, ok := scopes(scope)
			if !ok {
				return nil, ErrUnauthenticated
			}
			for _, s := range requested {
				if !slices.Contains(g.Scopes, s) {
					return nil, ErrUnauthenticated
				}
			}
			g.Scopes = requested
		}
		if len(g.UsedRefreshHashes) >= 4096 {
			return nil, ErrUnauthenticated
		}
		g.UsedRefreshHashes = append(g.UsedRefreshHashes, g.RefreshHash)
		access, refresh = newGrantTokens(id, &g)
		return json.Marshal(g)
	})
	if err != nil || replayed {
		oauthError(w, 400, "invalid_grant")
		return
	}
	tokenReply(w, g, access, refresh)
}
func (m *Manager) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || !single(r.PostForm) {
		oauthError(w, 400, "invalid_request")
		return
	}
	c, err := m.authenticateClient(r)
	if err != nil {
		oauthError(w, 401, "invalid_client")
		return
	}
	token := r.PostForm.Get("token")
	id := grantID(token, "wlr_")
	if id == "" {
		id = grantID(token, "wla_")
	}
	if id != "" {
		err = m.oauthStore.Update(r.Context(), "oauth/grants", id, func(raw json.RawMessage) (json.RawMessage, error) {
			var g oauthGrant
			if len(raw) == 0 {
				return nil, nil
			}
			if err := json.Unmarshal(raw, &g); err != nil {
				return nil, err
			}
			if g.ClientID == c.ID && (equal(g.RefreshHash, digest(token)) || equal(g.AccessHash, digest(token))) {
				return nil, nil
			}
			return raw, nil
		})
	}
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	w.WriteHeader(200)
}
func (m *Manager) authorizeOAuth(r *http.Request) (context.Context, error) {
	if !m.OAuthEnabled() || len(r.Header.Values("Authorization")) != 1 {
		return nil, &RequestError{401, "unauthorized"}
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, &RequestError{401, "unauthorized"}
	}
	id := grantID(parts[1], "wla_")
	if id == "" {
		return nil, &RequestError{401, "unauthorized"}
	}
	g, err := state.Read[oauthGrant](r.Context(), m.oauthStore, "oauth/grants", id)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return nil, &RequestError{401, "unauthorized"}
		}
		return nil, &RequestError{500, "authentication unavailable"}
	}
	if !equal(g.AccessHash, digest(parts[1])) || !time.Now().Before(g.AccessExpiresAt) || !time.Now().Before(g.ExpiresAt) || g.Resource != m.ResourceURL() || g.User.Issuer != m.config.Issuer || g.User.Subject == "" {
		return nil, &RequestError{401, "unauthorized"}
	}
	ctx := context.WithValue(r.Context(), userKey{}, g.User)
	ctx = context.WithValue(ctx, oauthContextKey{}, g.Scopes)
	if err = RequireScope(ctx, ScopeRead); err != nil {
		return nil, err
	}
	return ctx, nil
}

// RequireScope applies to delegated OAuth credentials. Same-origin browser sessions
// retain the signed-in user's full permissions, and existing static-token library
// access is unchanged. Personal collections separately require a named identity.
func RequireScope(ctx context.Context, required string) error {
	if scopes, delegated := ctx.Value(oauthContextKey{}).([]string); delegated && !slices.Contains(scopes, required) {
		return &RequestError{403, "insufficient scope"}
	}
	return nil
}

// PersonalID never maps a shared development token or legacy username to a user.
func PersonalID(ctx context.Context, write bool) (string, error) {
	u, ok := UserFromContext(ctx)
	if !ok || u.ID == "" {
		return "", &RequestError{401, "a signed-in user is required"}
	}
	if write {
		if err := RequireScope(ctx, ScopeWrite); err != nil {
			return "", err
		}
	}
	return "oidc:" + u.ID, nil
}

// ProtectMCP requires a delegated token even if local native APIs are anonymous.
// It never accepts a browser cookie or the shared development/Subsonic credential.
func (m *Manager) ProtectMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, err := m.authorizeOAuth(r)
		if err != nil {
			status := 500
			var failure *RequestError
			if errors.As(err, &failure) {
				status = failure.StatusCode
			}
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+m.MetadataURL()+`", scope="`+ScopeRead+` `+ScopeWrite+`"`)
			oauthError(w, status, "invalid_token")
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

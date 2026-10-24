// Package auth owns provider-neutral OIDC login, browser sessions and delegated OAuth.
// Legacy Subsonic credentials are deliberately outside this package.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/prayujt/wavelength/internal/state"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

var ErrUnauthenticated = errors.New("unauthenticated")

// Config uses only standard OIDC discovery, never vendor-specific endpoints or claims.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	PublicURL    string
	Token        string // Optional development/automation credential, independent of OIDC.
}

type User struct {
	ID      string `json:"id"`
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}
type Flow struct {
	TokenHash, StateHash, Nonce, Verifier string
	ReturnTo                              string
	ExpiresAt                             time.Time
}
type Session struct {
	TokenHash, CSRF string
	User            User
	ExpiresAt       time.Time
}

// Store persists only hashes of browser bearer credentials, not their raw values.
type Store interface {
	SaveFlow(context.Context, Flow) error
	ConsumeFlow(context.Context, string) (Flow, error)
	CreateSession(context.Context, Session) error
	Session(context.Context, string) (Session, error)
	DeleteSession(context.Context, string) error
}

type Manager struct {
	config        Config
	store         Store
	oauth         oauth2.Config
	verifier      *oidc.IDTokenVerifier
	client        *http.Client
	origin        string
	secure        bool
	sessionCookie string
	flowCookie    string
	oauthStore    state.Store
	oauthLimit    *rate.Limiter
}

type Option func(*Manager)

// WithOAuthStore enables delegated API authorization using the same OIDC app.
func WithOAuthStore(store state.Store) Option {
	return func(m *Manager) { m.oauthStore = store }
}

// New fails closed on partial/invalid OIDC settings or failed provider discovery.
func New(ctx context.Context, c Config, store Store, options ...Option) (*Manager, error) {
	m := &Manager{
		config:        c,
		store:         store,
		client:        &http.Client{Timeout: 10 * time.Second},
		sessionCookie: "wavelength_session",
		flowCookie:    "wavelength_login",
		oauthLimit:    rate.NewLimiter(5, 20),
	}
	for _, option := range options {
		option(m)
	}
	if c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" && c.PublicURL == "" {
		return m, nil
	}
	if c.Issuer == "" || c.ClientID == "" || c.PublicURL == "" {
		return nil, errors.New("OIDC requires issuer, client ID and public URL")
	}
	if store == nil {
		return nil, errors.New("OIDC requires a session store")
	}
	if _, err := validatedURL(c.Issuer, false); err != nil {
		return nil, fmt.Errorf("OIDC issuer: %w", err)
	}
	public, err := validatedURL(c.PublicURL, true)
	if err != nil {
		return nil, fmt.Errorf("public URL: %w", err)
	}
	m.origin = strings.TrimSuffix(public.String(), "/")
	m.secure = public.Scheme == "https"
	if m.secure {
		m.sessionCookie = "__Host-wavelength_session"
		m.flowCookie = "__Host-wavelength_login"
	}
	discovery, cancel := context.WithTimeout(oidc.ClientContext(ctx, m.client), 10*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(discovery, c.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	// Discovery documents must not downgrade token/auth/key endpoints to non-loopback HTTP.
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err = provider.Claims(&metadata); err != nil {
		return nil, err
	}
	endpoint := provider.Endpoint()
	for _, value := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKSURL} {
		if _, err = validatedURL(value, false); err != nil {
			return nil, fmt.Errorf("OIDC discovered endpoint: %w", err)
		}
	}
	m.oauth = oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  m.origin + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	// A background context avoids tying future JWKS refreshes to startup's deadline.
	m.verifier = provider.VerifierContext(oidc.ClientContext(context.Background(), m.client), &oidc.Config{ClientID: c.ClientID})
	return m, nil
}
func (m *Manager) OIDCEnabled() bool { return m.verifier != nil }
func digest(value string) string     { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
func equal(a, b string) bool {
	x := sha256.Sum256([]byte(a))
	y := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func cookieToken(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil || len(cookie.Value) > 256 {
		return ""
	}
	return cookie.Value
}
func (m *Manager) browserSession(r *http.Request) (Session, error) {
	if !m.OIDCEnabled() {
		return Session{}, ErrUnauthenticated
	}
	token := cookieToken(r, m.sessionCookie)
	if token == "" {
		return Session{}, ErrUnauthenticated
	}
	session, err := m.store.Session(r.Context(), digest(token))
	if err != nil {
		return Session{}, err
	}
	if session.User.Issuer != m.config.Issuer {
		return Session{}, ErrUnauthenticated
	}
	return session, nil
}

// Protect authenticates native API requests, with CSRF checks for cookie-authenticated writes.
// RequestError carries transport-neutral authentication failures.
type RequestError struct {
	StatusCode int
	Message    string
}

func (e *RequestError) Error() string { return e.Message }

// Authorize is shared by HTTP media and protobuf RPC adapters.
func (m *Manager) Authorize(r *http.Request) (context.Context, error) {
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		if m.config.Token != "" && equal(authorization, "Bearer "+m.config.Token) {
			return r.Context(), nil
		}
		if m.OAuthEnabled() {
			return m.authorizeOAuth(r)
		}
		return nil, &RequestError{401, "unauthorized"}
	}
	if !m.OIDCEnabled() && m.config.Token == "" {
		return r.Context(), nil
	}
	session, err := m.browserSession(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			return nil, &RequestError{401, "unauthorized"}
		}
		return nil, &RequestError{500, "authentication unavailable"}
	}
	if !m.validCSRF(r, session) {
		return nil, &RequestError{403, "invalid CSRF token or origin"}
	}
	return context.WithValue(r.Context(), userKey{}, session.User), nil
}
func (m *Manager) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, err := m.Authorize(r)
		if err != nil {
			var requestError *RequestError
			if errors.As(err, &requestError) {
				if requestError.StatusCode == 401 {
					w.Header().Set("WWW-Authenticate", "Bearer")
				}
				jsonError(w, requestError.StatusCode, requestError.Message)
			} else {
				jsonError(w, 500, "authentication unavailable")
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Manager) validCSRF(r *http.Request, s Session) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return r.Header.Get("Origin") == m.origin && equal(r.Header.Get("X-CSRF-Token"), s.CSRF)
}
func (m *Manager) setCookie(w http.ResponseWriter, name, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}
func (m *Manager) clearCookie(w http.ResponseWriter, name string) {
	m.setCookie(w, name, "", time.Unix(1, 0), -1)
}
func validatedURL(value string, originOnly bool) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("must be an absolute URL without userinfo, query or fragment")
	}
	if originOnly && u.Path != "" && u.Path != "/" {
		return nil, errors.New("must be an origin URL without a path")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("HTTPS required except for loopback development")
		}
	}
	return u, nil
}

// Keep all authentication secrets crypto-random rather than deriving them from claims.
func secret() string { return rand.Text() }

type userKey struct{}

// UserFromContext returns the OIDC identity for browser and delegated API requests.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey{}).(User)
	return user, ok
}

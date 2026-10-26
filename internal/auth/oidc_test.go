package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/playfeed"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/testutil"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
	transportmcp "github.com/prayujt/wavelength/internal/transport/mcp"
	"github.com/prayujt/wavelength/internal/userdata"
)

// This fixture is an actual HTTP discovery/authorize/token/JWKS provider, not a
// bypass of OIDC verification. It requires S256 PKCE and signs real RSA ID tokens.
func provider(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signingKey := key
	if mode == "signature" {
		signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	type grant struct{ nonce, challenge, redirect, subject string }
	var mu sync.Mutex
	grants := map[string]grant{}
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" || q.Get("client_id") != "wavelength" || q.Get("response_type") != "code" {
			http.Error(w, "invalid authorization request", 400)
			return
		}
		code := rand.Text()
		mu.Lock()
		subject := q.Get("login_hint")
		if subject == "" {
			subject = "subject-1"
		}
		grants[code] = grant{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), subject}
		mu.Unlock()
		callback, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			http.Error(w, "redirect", 400)
			return
		}
		values := callback.Query()
		values.Set("state", q.Get("state"))
		values.Set("code", code)
		callback.RawQuery = values.Encode()
		http.Redirect(w, r, callback.String(), 302)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "form", 400)
			return
		}
		client, secret, ok := r.BasicAuth()
		if !ok || client != "wavelength" || secret != "test-secret" {
			http.Error(w, "client", 401)
			return
		}
		mu.Lock()
		grant, ok := grants[r.Form.Get("code")]
		delete(grants, r.Form.Get("code"))
		mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge || r.Form.Get("redirect_uri") != grant.redirect || r.Form.Get("grant_type") != "authorization_code" {
			http.Error(w, "grant", 400)
			return
		}
		claims := map[string]any{
			"iss":   server.URL,
			"aud":   "wavelength",
			"sub":   grant.subject,
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": grant.nonce,
			"name":  "Test Listener",
			"email": "listener@example.test",
		}
		switch mode {
		case "nonce":
			claims["nonce"] = "wrong"
		case "audience":
			claims["aud"] = "other-app"
		case "issuer":
			claims["iss"] = "https://wrong.example.test"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Hour).Unix()
		}
		body, err := json.Marshal(claims)
		if err != nil {
			http.Error(w, "claims", 500)
			return
		}
		signed, err := signer.Sign(body)
		if err != nil {
			http.Error(w, "sign", 500)
			return
		}
		token, err := signed.CompactSerialize()
		if err != nil {
			http.Error(w, "serialize", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": token})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func app(t *testing.T, issuer, dataDir, music string, acquisition ...func(*sql.DB, *library.Library, *userdata.Service) transportmcp.Features) (*httptest.Server, *sql.DB) {
	return appWithOptions(t, issuer, dataDir, music, nil, acquisition...)
}

func appWithOptions(t *testing.T, issuer, dataDir, music string, options []httpserver.Option, acquisition ...func(*sql.DB, *library.Library, *userdata.Service) transportmcp.Features) (*httptest.Server, *sql.DB) {
	t.Helper()
	db, err := database.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	testutil.SeedCatalog(t, db, music)
	server := httptest.NewUnstartedServer(nil)
	publicURL := "http://" + server.Listener.Addr().String()
	manager, err := auth.New(context.Background(), auth.Config{Issuer: issuer, ClientID: "wavelength", ClientSecret: "test-secret", PublicURL: publicURL, Token: "test-native"}, database.AuthStore{DB: db}, auth.WithOAuthStore(database.StateStore{DB: db}))
	if err != nil {
		t.Fatal(err)
	}
	data := userdata.New(database.StateStore{DB: db}, lib)
	features := []transportmcp.Features{}
	if len(acquisition) > 0 {
		features = append(features, acquisition[0](db, lib, data))
	}
	links := sharing.New(database.GrantStore{DB: db}, lib, publicURL)
	if len(features) == 0 {
		features = append(features, transportmcp.Features{})
	}
	features[0].Links = links
	features[0].Listening = listening.New(database.ListeningStore{DB: db}, lib)
	options = append(options, httpserver.WithSharing(lib, links))
	feedCtx, stopFeed := context.WithCancel(context.Background())
	options = append(options, httpserver.WithPlayFeed(playfeed.New(feedCtx, lib, database.PlayFeedStore{DB: db}, database.ListeningStore{DB: db}, nil, nil), manager))
	options = append(options, httpserver.WithAuth(manager), httpserver.WithNative(lib, manager), httpserver.WithPersonal(lib, data, manager, features...))
	server.Config.Handler = httpserver.New("", db.PingContext, options...).Handler
	server.Start()
	t.Cleanup(func() {
		stopFeed()
		server.Close()
		for _, feature := range features {
			if feature.Ingest != nil {
				feature.Ingest.Close()
			}
		}
		lib.Close()
		db.Close()
	})
	return server, db
}
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func do(t *testing.T, c *http.Client, method, target string, headers map[string]string) *http.Response {
	t.Helper()
	var body io.Reader
	if strings.Contains(target, "/wavelength.v1.LibraryService/") {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	result, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { result.Body.Close() })
	return result
}
func beginLogin(t *testing.T, c *http.Client, target string) (string, *http.Cookie) {
	t.Helper()
	response := do(t, c, "GET", target+"/auth/login", nil)
	if response.StatusCode != 302 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("login: %d %s", response.StatusCode, body)
	}
	var flow *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == "wavelength_login" {
			flow = cookie
		}
	}
	if flow == nil || !flow.HttpOnly || flow.SameSite != http.SameSiteLaxMode {
		t.Fatal("invalid login cookie")
	}
	authorized := do(t, c, "GET", response.Header.Get("Location"), nil)
	if authorized.StatusCode != 302 {
		t.Fatalf("authorize: %d", authorized.StatusCode)
	}
	return authorized.Header.Get("Location"), flow
}

type identity struct {
	User auth.User `json:"user"`
	CSRF string    `json:"csrf_token"`
}

func me(t *testing.T, c *http.Client, target string) identity {
	t.Helper()
	response := do(t, c, "GET", target+"/auth/me", nil)
	if response.StatusCode != 200 {
		t.Fatalf("me: %d", response.StatusCode)
	}
	var id identity
	if err := json.NewDecoder(response.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOIDCLoginCookieStreamingPersistenceCSRFAndLogout(t *testing.T) {
	issuer := provider(t, "")
	data, music := t.TempDir(), t.TempDir()
	audio := []byte("RIFF original audio bytes")
	if err := os.WriteFile(filepath.Join(music, "tone.wav"), audio, 0600); err != nil {
		t.Fatal(err)
	}
	server, db := app(t, issuer.URL, data, music)
	client := browser(t)
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", nil); r.StatusCode != 401 {
		t.Fatal("anonymous native access")
	}
	callback, _ := beginLogin(t, client, server.URL)
	response := do(t, client, "GET", callback, nil)
	if response.StatusCode != 303 || response.Header.Get("Location") != "/auth/me" {
		t.Fatalf("callback: %d", response.StatusCode)
	}
	var cookie *http.Cookie
	for _, value := range response.Cookies() {
		if value.Name == "wavelength_session" {
			cookie = value
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 43200 {
		t.Fatalf("session cookie: %+v", cookie)
	}
	current := me(t, client, server.URL)
	if current.User.Issuer != issuer.URL || current.User.Subject != "subject-1" || current.CSRF == "" {
		t.Fatalf("identity: %+v", current)
	}
	var stored string
	if err := db.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == cookie.Value || stored != fmt.Sprintf("%x", sha256.Sum256([]byte(cookie.Value))) {
		t.Fatal("raw session token persisted")
	}
	response = do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Origin": server.URL, "X-CSRF-Token": current.CSRF})
	var page library.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Tracks) != 1 {
		t.Fatal(page)
	}
	response = do(t, client, "GET", server.URL+"/media/tracks/"+page.Tracks[0].ID, map[string]string{"Range": "bytes=0-3"})
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 206 || string(body) != "RIFF" {
		t.Fatalf("cookie stream: %d %s", response.StatusCode, body)
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", nil); r.StatusCode != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Origin": "https://evil.test", "X-CSRF-Token": current.CSRF}); r.StatusCode != 403 {
		t.Fatal("wrong origin accepted")
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Origin": server.URL, "X-CSRF-Token": current.CSRF}); r.StatusCode != 200 {
		t.Fatalf("valid CSRF rejected: %d", r.StatusCode)
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Authorization": "Bearer test-native"}); r.StatusCode != 200 {
		t.Fatal("independent bearer rejected")
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Authorization": "Bearer wrong"}); r.StatusCode != 401 {
		t.Fatal("invalid bearer fell back to cookie")
	}
	// The same cookie must work after a new manager, new listener and database reopen.
	server.Close()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	server, db = app(t, issuer.URL, data, music)
	restored := me(t, client, server.URL)
	if restored.User.ID != current.User.ID || restored.CSRF != current.CSRF {
		t.Fatal("session did not survive restart")
	}
	if r := do(t, client, "POST", server.URL+"/auth/logout", nil); r.StatusCode != 403 {
		t.Fatal("logout CSRF bypass")
	}
	response = do(t, client, "POST", server.URL+"/auth/logout", map[string]string{"Origin": server.URL, "X-CSRF-Token": current.CSRF})
	if response.StatusCode != 204 {
		t.Fatalf("logout: %d", response.StatusCode)
	}
	if r := do(t, client, "POST", server.URL+"/wavelength.v1.LibraryService/ListTracks", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}); r.StatusCode != 401 {
		t.Fatal("revoked session replay")
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("logout did not remove session")
	}
}

func TestOIDCRejectsInvalidTokens(t *testing.T) {
	for _, mode := range []string{"nonce", "audience", "issuer", "expired", "signature"} {
		t.Run(mode, func(t *testing.T) {
			issuer := provider(t, mode)
			server, db := app(t, issuer.URL, t.TempDir(), t.TempDir())
			client := browser(t)
			callback, _ := beginLogin(t, client, server.URL)
			if r := do(t, client, "GET", callback, nil); r.StatusCode != 401 {
				t.Fatalf("invalid token accepted: %d", r.StatusCode)
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
				t.Fatal("invalid token created session")
			}
		})
	}
}

func TestOIDCStateExpiryAndReplay(t *testing.T) {
	issuer := provider(t, "")
	for _, mode := range []string{"state", "expiry", "missing-cookie"} {
		t.Run(mode, func(t *testing.T) {
			server, db := app(t, issuer.URL, t.TempDir(), t.TempDir())
			client := browser(t)
			callback, cookie := beginLogin(t, client, server.URL)
			target := callback
			switch mode {
			case "state":
				u, _ := url.Parse(callback)
				values := u.Query()
				values.Set("state", "wrong")
				u.RawQuery = values.Encode()
				target = u.String()
			case "expiry":
				if _, err := db.Exec("UPDATE auth_flows SET expires_at=0"); err != nil {
					t.Fatal(err)
				}
			case "missing-cookie":
				client = browser(t)
			}
			if r := do(t, client, "GET", target, nil); r.StatusCode != 400 {
				t.Fatalf("invalid flow accepted: %d", r.StatusCode)
			}
			if mode != "missing-cookie" {
				// Restore the original browser binding explicitly; even that cannot replay a consumed/expired flow.
				if r := do(t, browser(t), "GET", callback, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}); r.StatusCode != 400 {
					t.Fatal("login flow replay")
				}
			}
		})
	}
}

func TestOIDCConfigurationAndSecureCookies(t *testing.T) {
	for _, config := range []auth.Config{
		{ClientID: "partial"},
		{Issuer: "http://public.example.test", ClientID: "id", PublicURL: "https://music.example.test"},
		{Issuer: "https://issuer.example.test", ClientID: "id", PublicURL: "https://music.example.test/subpath"},
	} {
		db, err := database.Open(context.Background(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = auth.New(context.Background(), config, database.AuthStore{DB: db}); err == nil {
			t.Fatalf("invalid config accepted: %+v", config)
		}
		db.Close()
	}
	issuer := provider(t, "")
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager, err := auth.New(context.Background(), auth.Config{Issuer: issuer.URL, ClientID: "wavelength", PublicURL: "https://music.example.test"}, database.AuthStore{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	manager.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://music.example.test/auth/login", nil))
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || !strings.HasPrefix(cookie.Name, "__Host-") || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("insecure cookie: %+v", cookie)
	}
}

func TestSessionDoesNotCrossConfiguredIssuers(t *testing.T) {
	first, second := provider(t, ""), provider(t, "")
	data, music := t.TempDir(), t.TempDir()
	server, db := app(t, first.URL, data, music)
	client := browser(t)
	callback, _ := beginLogin(t, client, server.URL)
	if response := do(t, client, "GET", callback, nil); response.StatusCode != 303 {
		t.Fatal("login failed")
	}
	_ = me(t, client, server.URL)
	server.Close()
	db.Close()
	replacement, _ := app(t, second.URL, data, music)
	if response := do(t, client, "GET", replacement.URL+"/auth/me", nil); response.StatusCode != 401 {
		t.Fatal("old issuer session accepted")
	}
}

package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// This uses an actual browser: hand-written Origin headers in API tests cannot
// reproduce referrer-policy's Origin:null behavior on normal HTML form POSTs.
func TestOAuthBrowserConsent(t *testing.T) {
	python := os.Getenv("WAVELENGTH_BROWSER_PYTHON")
	if python == "" {
		t.Skip("set WAVELENGTH_BROWSER_PYTHON to Python with Playwright; required in CI")
	}
	for _, mode := range []string{"normal", "proxy-no-referrer", "cancel", "old-policy-control"} {
		t.Run(mode, func(t *testing.T) {
			issuer := provider(t, "")
			server, _ := app(t, issuer.URL, t.TempDir(), t.TempDir())
			type result struct{ Code, State, Issuer, Error string }
			callbacks := make(chan result, 1)
			callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/callback" {
					http.NotFound(w, r)
					return
				}
				q := r.URL.Query()
				select {
				case callbacks <- result{q.Get("code"), q.Get("state"), q.Get("iss"), q.Get("error")}:
				default:
					http.Error(w, "duplicate callback", 409)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, "<h1>Connection completed</h1>")
			}))
			defer callback.Close()
			body, _ := json.Marshal(map[string]any{"client_name": "Test AI", "redirect_uris": []string{callback.URL + "/callback?keep=1"}, "token_endpoint_auth_method": "none"})
			c := decode[registeredClient](t, requestBody(t, http.DefaultClient, "POST", server.URL+"/oauth/register", "application/json", strings.NewReader(string(body)), nil), 201)
			verifier := oauth2.GenerateVerifier()
			input, _ := json.Marshal(map[string]any{
				"url": authorizationURL(server, c, verifier, ""), "origin": server.URL,
				"callback": callback.URL, "mode": mode,
				"browser":    os.Getenv("WAVELENGTH_BROWSER_BIN"),
				"screenshot": os.Getenv("WAVELENGTH_BROWSER_SCREENSHOT"),
			})
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, python, "testdata/consent_browser.py")
			command.Stdin = strings.NewReader(string(input))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("browser consent check %s: %v\n%s", mode, err, output)
			}
			if mode == "old-policy-control" {
				select {
				case <-callbacks:
					t.Fatal("Origin:null must not issue a grant")
				default:
				}
				return
			}
			select {
			case received := <-callbacks:
				if received.State != "test-state" || received.Issuer != server.URL {
					t.Fatal("browser callback lost OAuth binding")
				}
				if mode == "cancel" {
					if received.Error != "access_denied" || received.Code != "" {
						t.Fatal("cancel issued an authorization code")
					}
				} else {
					decode[delegatedToken](t, exchange(t, server, c, received.Code, verifier), 200)
				}
			default:
				t.Fatal("browser did not reach the client callback")
			}
		})
	}
}

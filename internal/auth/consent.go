package auth

import (
	_ "embed"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

//go:embed consent.html
var consentHTML string

var consentPage = template.Must(template.New("consent").Parse(consentHTML))

type permission struct{ Title, Description string }
type consentView struct {
	User                                       User
	ClientName, Redirect, ID, CSRF, StyleNonce string
	Permissions                                []permission
	ErrorTitle, ErrorMessage                   string
}

func renderConsent(w http.ResponseWriter, status int, view consentView) {
	view.StyleNonce = secret()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A regular HTML form uses Origin:null under no-referrer. Keep the strict
	// Origin check and withhold referrers on cross-origin navigations instead.
	// The document's matching meta policy also handles proxies overriding this header.
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+view.StyleNonce+"'; form-action "+consentFormAction(view.Redirect)+"; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = consentPage.Execute(w, view)
}

func consentFormAction(redirect string) string {
	// Chromium applies form-action to the redirect after a form POST as well.
	// Permit only the registered client's origin, never untrusted URI path/query
	// text (which could contain CSP directive punctuation).
	u, err := url.Parse(redirect)
	if err != nil || !validRedirect(redirect) || strings.ContainsAny(u.Host, "\t\r\n ;'\"\\") {
		return "'self'"
	}
	return "'self' " + u.Scheme + "://" + u.Host
}

func consentFailure(w http.ResponseWriter, status int, title, message string) {
	renderConsent(w, status, consentView{ErrorTitle: title, ErrorMessage: message})
}

func (m *Manager) showConsent(w http.ResponseWriter, r *http.Request, id string) {
	a, session, err := m.pending(r, id)
	if err != nil {
		consentFailure(w, 400, "This connection has expired", "Return to your app and start a fresh connection. You can close this tab.")
		return
	}
	c, err := m.oauthClient(r.Context(), a.ClientID)
	if err != nil {
		consentFailure(w, 400, "This app is no longer available", "Return to your app and start a fresh connection.")
		return
	}
	view := consentView{User: session.User, ClientName: c.Name, Redirect: a.Redirect, ID: id, CSRF: session.CSRF}
	for _, scope := range a.Scopes {
		switch scope {
		case ScopeRead:
			view.Permissions = append(view.Permissions, permission{"Read your music", "Browse the shared library and read your playlists, queue, bookmarks, and listening history."})
		case ScopeWrite:
			view.Permissions = append(view.Permissions, permission{"Manage your collections", "Create and update your playlists, playback queue, bookmarks, and listening history."})
		case ScopeStream:
			view.Permissions = append(view.Permissions, permission{"Play music", "Stream audio files from the shared library."})
		}
	}
	renderConsent(w, 200, view)
}

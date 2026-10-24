package subsonic

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// New builds only the protocol boundary; callers retain ownership of its services.
func New(c Config, services Dependencies) (*http.Server, error) {
	if services.Library == nil || services.UserData == nil || services.Transcoder == nil {
		return nil, errors.New("Subsonic requires application services")
	}
	if c.Username == "" || c.Password == "" {
		return nil, errors.New("Subsonic requires a username and password")
	}
	api := &api{
		lib: services.Library, data: services.UserData, transcoder: services.Transcoder,
		username: c.Username, password: c.Password,
	}
	return &http.Server{Addr: c.Addr, Handler: api, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}, nil
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/share/") {
		http.NotFound(w, r)
		return
	}
	result := response{XMLNS: "http://subsonic.org/restapi", Status: "ok", Version: "1.16.1", Type: "wavelength", ServerVersion: "0.1.0"}
	fail := func(code int, message string) {
		result.Status = "failed"
		result.Error = &apiError{Code: code, Message: message}
	}
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if result.Status != "streamed" {
			writeResponse(w, r, result)
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		fail(10, "Invalid request parameters")
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" {
		fail(0, "Unsupported HTTP method")
		return
	}
	if format := r.Form.Get("f"); format != "" && format != "xml" && format != "json" && format != "jsonp" {
		fail(0, "Unsupported response format")
		return
	}
	if !authenticated(r, a.username, a.password) {
		fail(40, "Wrong username or password")
		return
	}
	if r.Form.Get("v") == "" || r.Form.Get("c") == "" {
		fail(10, "Parameters v and c are required")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/rest/") {
		fail(70, "Endpoint not found")
		return
	}
	endpoint := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), ".view"), ".m3u8")
	if err := validateVersion(r.Form.Get("v")); err != nil {
		fail(err.Code, err.Message)
		return
	}
	if r.Form.Get("f") == "jsonp" && !callbackPattern.MatchString(r.Form.Get("callback")) {
		fail(10, "Invalid callback")
		return
	}
	for _, key := range requiredParameters[endpoint] {
		if !r.Form.Has(key) || (r.Form.Get(key) == "" && key != "query") {
			fail(10, "Parameter "+key+" is required")
			return
		}
	}
	if a.handle(w, r, endpoint, &result) {
		return
	}
	switch endpoint {
	case "ping":
	case "getLicense":
		result.License = &license{Valid: true}
	case "getMusicFolders":
		result.Folders = &folders{Folders: []folder{{ID: 1, Name: "Music"}}}
	default:
		fail(70, "Endpoint not implemented")
	}
}

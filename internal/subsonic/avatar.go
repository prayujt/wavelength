package subsonic

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func WithAvatarDirectory(dir string) Option {
	return func(a *api) error {
		if dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			a.avatarDir = dir
		}
		return nil
	}
}
func (a *api) avatarFile(username string) (*os.File, string, error) {
	root, err := os.OpenRoot(a.avatarDir)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	for _, extension := range []string{".png", ".jpg", ".jpeg", ".gif"} {
		file, err := root.Open(url.PathEscape(username) + extension)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
			file.Close()
			continue
		}
		contentType := "image/jpeg"
		if extension == ".png" {
			contentType = "image/png"
		}
		if extension == ".gif" {
			contentType = "image/gif"
		}
		return file, contentType, nil
	}
	return nil, "", os.ErrNotExist
}
func (a *api) avatar(w http.ResponseWriter, r *http.Request, result *response) {
	name := required(r, result, "username")
	if result.Error != nil {
		return
	}
	if _, err := a.account(r.Context(), name); err != nil {
		failure(result, 70, "User not found")
		return
	}
	file, contentType, err := a.avatarFile(name)
	if err != nil {
		failure(result, 70, "User has no avatar")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		legacyError(result, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, strings.TrimSpace(name), info.ModTime(), file)
	result.Status = "streamed"
}
func (a *api) avatarChanged(username string) string {
	file, _, err := a.avatarFile(username)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

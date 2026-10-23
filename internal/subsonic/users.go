package subsonic

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/state"
)

var accountRoles = []string{"adminRole", "settingsRole", "streamRole", "jukeboxRole", "downloadRole", "uploadRole", "playlistRole", "coverArtRole", "commentRole", "podcastRole", "shareRole", "videoConversionRole"}

type account struct {
	Username, Email, Password string
	Roles                     map[string]bool
	Folders                   []string
}

func (a *api) defaultAccount() account {
	return account{Username: a.user, Roles: map[string]bool{"adminRole": true, "settingsRole": true, "streamRole": true, "downloadRole": true, "playlistRole": true, "commentRole": true}, Folders: []string{"1"}}
}
func (a *api) account(ctx context.Context, name string) (account, error) {
	raw, err := a.store.Get(ctx, "subsonic-users", name)
	if errors.Is(err, state.ErrNotFound) && name == a.user {
		return a.defaultAccount(), nil
	}
	if err != nil {
		return account{}, err
	}
	var user account
	err = json.Unmarshal(raw, &user)
	return user, err
}
func (a *api) seal(password string) (string, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(box.Seal(nonce, nonce, []byte(password), []byte("wavelength/subsonic/v1"))), nil
}
func (a *api) unseal(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < box.NonceSize() {
		return "", errors.New("invalid encrypted password")
	}
	body, err := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], []byte("wavelength/subsonic/v1"))
	return string(body), err
}
func (a *api) authenticate(r *http.Request) (account, bool, error) {
	user, err := a.account(r.Context(), r.Form.Get("u"))
	if errors.Is(err, state.ErrNotFound) {
		return account{}, false, nil
	}
	if err != nil {
		return account{}, false, err
	}
	password := a.password
	if user.Password != "" {
		password, err = a.unseal(user.Password)
		if err != nil {
			return account{}, false, err
		}
	} else if user.Username != a.user {
		return account{}, false, nil
	}
	return user, authenticated(r, user.Username, password), nil
}
func (a *api) authorized(r *http.Request, endpoint string, user account, result *response) bool {
	role := ""
	switch endpoint {
	case "stream", "hls":
		role = "streamRole"
	case "setRating":
		role = "commentRole"
	case "download":
		role = "downloadRole"
	case "getUsers", "createUser", "updateUser", "deleteUser", "createInternetRadioStation", "updateInternetRadioStation", "deleteInternetRadioStation", "startScan":
		role = "adminRole"
	case "jukeboxControl":
		role = "jukeboxRole"
	case "createShare", "updateShare", "deleteShare":
		role = "shareRole"
	case "createPodcastChannel", "deletePodcastChannel", "downloadPodcastEpisode", "deletePodcastEpisode", "refreshPodcasts":
		role = "podcastRole"
	}
	if role != "" && !user.Roles[role] {
		failure(result, 50, "User is not authorized for this operation")
		return false
	}
	if (endpoint == "getUser" || endpoint == "changePassword") && r.Form.Get("username") != user.Username && !user.Roles["adminRole"] {
		failure(result, 50, "User cannot manage another account")
		return false
	}
	if endpoint == "changePassword" && !user.Roles["settingsRole"] && !user.Roles["adminRole"] {
		failure(result, 50, "User cannot change settings")
		return false
	}
	return true
}
func accountNode(user account) map[string]any {
	out := map[string]any{"username": user.Username, "email": user.Email, "scrobblingEnabled": false, "folder": user.Folders}
	for _, role := range accountRoles {
		out[role] = user.Roles[role]
	}
	return out
}
func (a *api) users(r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getUser", "getUsers", "createUser", "updateUser", "deleteUser", "changePassword":
	default:
		return false
	}
	ctx := r.Context()
	if endpoint == "getUsers" {
		records, err := a.store.List(ctx, "subsonic-users")
		if err != nil {
			legacyError(result, err)
			return true
		}
		users := map[string]account{a.user: a.defaultAccount()}
		for key, raw := range records {
			var user account
			if err = json.Unmarshal(raw, &user); err != nil {
				legacyError(result, err)
				return true
			}
			users[key] = user
		}
		keys := make([]string, 0, len(users))
		for key := range users {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := []map[string]any{}
		for _, key := range keys {
			out = append(out, accountNode(users[key]))
		}
		payload(result, "users", map[string]any{"user": out})
		return true
	}
	name := required(r, result, "username")
	if result.Error != nil {
		return true
	}
	current, err := a.account(ctx, name)
	if endpoint == "getUser" {
		if errors.Is(err, state.ErrNotFound) {
			failure(result, 70, "User not found")
		} else if err != nil {
			legacyError(result, err)
		} else {
			payload(result, "user", accountNode(current))
		}
		return true
	}
	creating := endpoint == "createUser"
	if creating && err == nil {
		failure(result, 0, "User already exists")
		return true
	}
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		legacyError(result, err)
		return true
	}
	if !creating && errors.Is(err, state.ErrNotFound) {
		failure(result, 70, "User not found")
		return true
	}
	if endpoint == "deleteUser" {
		if name == a.user {
			failure(result, 50, "Cannot delete bootstrap administrator")
			return true
		}
		if err = a.remove(ctx, "subsonic-users", name); err != nil {
			legacyError(result, err)
		}
		return true
	}
	if r.Form.Get("ldapAuthenticated") == "true" {
		failure(result, 50, "LDAP is not configured")
		return true
	}
	if creating {
		required(r, result, "password")
		required(r, result, "email")
		if result.Error != nil {
			return true
		}
		current = account{Username: name, Roles: map[string]bool{"settingsRole": true, "streamRole": true, "playlistRole": true}, Folders: []string{"1"}}
	}
	if endpoint == "changePassword" {
		required(r, result, "password")
		if result.Error != nil {
			return true
		}
	}
	if r.Form.Has("password") {
		password, err := decodeLegacyPassword(r.Form.Get("password"))
		if err != nil || password == "" {
			failure(result, 10, "Invalid password")
			return true
		}
		current.Password, err = a.seal(password)
		if err != nil {
			legacyError(result, err)
			return true
		}
	}
	if endpoint != "changePassword" {
		if r.Form.Has("email") {
			current.Email = r.Form.Get("email")
		}
		for _, role := range accountRoles {
			if !r.Form.Has(role) {
				continue
			}
			flag, err := strconv.ParseBool(r.Form.Get(role))
			if err != nil {
				failure(result, 10, "Invalid "+role)
				return true
			}
			current.Roles[role] = flag
		}
		if r.Form.Has("musicFolderId") {
			for _, folder := range r.Form["musicFolderId"] {
				if folder != "1" {
					failure(result, 70, "Music folder not found")
					return true
				}
			}
			current.Folders = r.Form["musicFolderId"]
		}
	}
	if name == a.user {
		current.Roles["adminRole"] = true
	}
	if err = a.save(ctx, "subsonic-users", name, current); err != nil {
		legacyError(result, err)
	}
	return true
}

func decodeLegacyPassword(value string) (string, error) {
	if strings.HasPrefix(value, "enc:") {
		raw, err := hex.DecodeString(value[4:])
		return string(raw), err
	}
	return value, nil
}

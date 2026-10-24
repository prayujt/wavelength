package subsonic

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/accounts"
)

var accountRoles = []string{"adminRole", "settingsRole", "streamRole", "jukeboxRole", "downloadRole", "uploadRole", "playlistRole", "coverArtRole", "commentRole", "podcastRole", "shareRole", "videoConversionRole"}

type account = accounts.Account

func (a *api) account(ctx context.Context, name string) (account, error) {
	return a.accounts.Get(ctx, name)
}
func (a *api) authenticate(r *http.Request) (account, bool, error) {
	return a.accounts.Verify(r.Context(), r.Form.Get("u"), func(password string) bool { return authenticated(r, r.Form.Get("u"), password) })
}
func (a *api) authorized(r *http.Request, endpoint string, user account, result *response) bool {
	role := ""
	switch endpoint {
	case "stream", "hls", "hlsSegment":
		role = "streamRole"
	case "setRating":
		role = "commentRole"
	case "download":
		role = "downloadRole"
	case "getUsers", "createUser", "updateUser", "deleteUser", "createInternetRadioStation", "updateInternetRadioStation", "deleteInternetRadioStation", "startScan":
		role = "adminRole"
	case "createShare", "updateShare", "deleteShare":
		role = "shareRole"
	case "createPodcastChannel", "deletePodcastChannel", "downloadPodcastEpisode", "deletePodcastEpisode", "refreshPodcasts":
		role = "podcastRole"
	}
	if role != "" && !user.Roles[role] {
		failure(result, 50, "User is not authorized for this operation")
		return false
	}
	if endpoint == "getPlaylists" && r.Form.Has("username") && !user.Roles["adminRole"] {
		failure(result, 50, "Listing another user requires administrator role")
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
	out := map[string]any{"username": user.Username, "email": user.Email, "scrobblingEnabled": false, "folder": user.Folders, "maxBitRate": user.MaxBitRate}
	for _, role := range accountRoles {
		out[role] = user.Roles[role]
	}
	out["jukeboxRole"] = false
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
		users, err := a.accounts.List(ctx)
		if err != nil {
			legacyError(result, err)
			return true
		}
		nodes := []map[string]any{}
		for _, user := range users {
			nodes = append(nodes, accountNode(user))
		}
		payload(result, "users", map[string]any{"user": nodes})
		return true
	}
	name := r.Form.Get("username")
	if endpoint == "getUser" {
		user, err := a.accounts.Get(ctx, name)
		if err != nil {
			legacyError(result, err)
			return true
		}
		node := accountNode(user)
		if changed := a.avatarChanged(user.Username); changed != "" {
			node["avatarLastChanged"] = changed
		}
		payload(result, "user", node)
		return true
	}
	if endpoint == "deleteUser" {
		if err := a.accounts.Delete(ctx, name); err != nil {
			legacyError(result, err)
		}
		return true
	}
	if r.Form.Get("ldapAuthenticated") == "true" {
		failure(result, 50, "LDAP is not configured")
		return true
	}
	changes := accounts.Changes{Roles: map[string]bool{}}
	if r.Form.Has("password") {
		password, err := decodeLegacyPassword(r.Form.Get("password"))
		if err != nil {
			failure(result, 10, "Invalid password")
			return true
		}
		changes.Password = &password
	}
	if endpoint != "changePassword" {
		changes.Email = optionalString(r, "email")
		for _, role := range accountRoles {
			if r.Form.Has(role) {
				value, err := strconv.ParseBool(r.Form.Get(role))
				if err != nil {
					failure(result, 10, "Invalid "+role)
					return true
				}
				changes.Roles[role] = value
			}
		}
		if r.Form.Has("maxBitRate") {
			rate, err := number(r, "maxBitRate", 0, 320)
			if err != nil {
				failure(result, 10, err.Error())
				return true
			}
			changes.MaxBitRate = &rate
		}
		if r.Form.Has("musicFolderId") {
			folders := r.Form["musicFolderId"]
			for _, id := range folders {
				if id != "1" {
					failure(result, 70, "Music folder not found")
					return true
				}
			}
			changes.Folders = &folders
		}
	}
	var err error
	if endpoint == "createUser" {
		err = a.accounts.Create(ctx, name, changes)
	} else {
		err = a.accounts.Update(ctx, name, changes)
	}
	if err != nil {
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

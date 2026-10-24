package subsonic

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/userdata"
)

type collection struct {
	artists    []artist
	albums     []album
	songs      []song
	tracks     map[string]library.Track
	lastPlayed map[string]time.Time
	history    map[string]userdata.History
}

func (a *api) collection(ctx context.Context, user string) (collection, error) {
	c := collection{artists: []artist{}, albums: []album{}, songs: []song{}, tracks: map[string]library.Track{}, lastPlayed: map[string]time.Time{}}
	all, err := catalog(ctx, a.lib)
	if err != nil {
		return c, err
	}
	history, err := a.data.History(ctx, user)
	if err != nil {
		return c, err
	}
	c.history = history
	for _, art := range all {
		for i := range art.Albums {
			alb := &art.Albums[i]
			for j := range alb.Songs {
				s := &alb.Songs[j]
				ann := history[s.ID]
				s.PlayCount = ann.Plays
				alb.PlayCount += ann.Plays
				played, _ := time.Parse(time.RFC3339Nano, ann.LastPlayed)
				if played.After(c.lastPlayed[alb.ID]) {
					c.lastPlayed[alb.ID] = played
				}
			}
			if len(alb.Songs) > 0 {
				art.CoverArt = alb.Songs[0].ID
			}
			c.songs = append(c.songs, alb.Songs...)
			c.albums = append(c.albums, *alb)
		}
		c.artists = append(c.artists, art)
	}
	for offset := 0; ; offset += 500 {
		page, err := a.lib.List(ctx, library.Query{Limit: 500, Offset: offset})
		if err != nil {
			return c, err
		}
		for _, t := range page.Tracks {
			c.tracks[t.ID] = t
		}
		if len(page.Tracks) < 500 {
			break
		}
	}
	return c, nil
}
func withoutSongs(albums []album) []album {
	out := append([]album{}, albums...)
	for i := range out {
		out[i].Songs = nil
	}
	return out
}
func withoutAlbums(artists []artist) []artist {
	out := append([]artist{}, artists...)
	for i := range out {
		out[i].Albums = nil
	}
	return out
}
func groupArtists(artists []artist) []artistIndex {
	groups := map[string][]artist{}
	for _, a := range withoutAlbums(artists) {
		name := strings.TrimSpace(a.Name)
		for _, article := range []string{"the ", "el ", "la ", "los ", "las ", "le ", "les "} {
			if strings.HasPrefix(strings.ToLower(name), article) {
				name = name[len(article):]
				break
			}
		}
		group := "#"
		for _, r := range name {
			if unicode.IsLetter(r) {
				group = string(unicode.ToUpper(r))
			}
			break
		}
		groups[group] = append(groups[group], a)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []artistIndex{}
	for _, key := range keys {
		values := groups[key]
		sort.SliceStable(values, func(i, j int) bool { return strings.ToLower(values[i].Name) < strings.ToLower(values[j].Name) })
		out = append(out, artistIndex{Name: key, Artists: values})
	}
	return out
}
func (a *api) browse(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	switch endpoint {
	case "getArtists", "getArtist", "getAlbum", "getSong", "getIndexes", "getMusicDirectory", "getGenres", "getAlbumList", "getAlbumList2", "getRandomSongs", "getSongsByGenre", "search", "search2", "search3", "getArtistInfo", "getArtistInfo2", "getAlbumInfo", "getAlbumInfo2", "getSimilarSongs", "getSimilarSongs2", "getTopSongs":
	default:
		return false
	}
	c, err := a.collection(r.Context(), r.Form.Get("u"))
	if err != nil {
		legacyError(result, err)
		return true
	}
	folder := r.Form.Get("musicFolderId")
	if folder != "" && folder != "1" {
		failure(result, 70, "Music folder not found")
		return true
	}
	if endpoint == "search2" || endpoint == "getAlbumList" {
		c = c.filesystem()
	}
	switch endpoint {
	case "getArtists":
		result.Artists = &artists{IgnoredArticles: "The El La Los Las Le Les", Index: groupArtists(c.artists)}
	case "getArtist":
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		for _, art := range c.artists {
			if art.ID == id {
				art.Albums = withoutSongs(art.Albums)
				result.Artist = &art
				return true
			}
		}
		failure(result, 70, "Artist not found")
	case "getAlbum":
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		for _, alb := range c.albums {
			if alb.ID == id {
				result.Album = &alb
				return true
			}
		}
		failure(result, 70, "Album not found")
	case "getSong":
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		for _, s := range c.songs {
			if s.ID == id {
				result.Song = &s
				return true
			}
		}
		failure(result, 70, "Song not found")
	case "getIndexes", "getMusicDirectory":
		a.folders(r, result, c, endpoint)
	case "getGenres":
		counts := map[string]int{}
		albums := map[string]map[string]bool{}
		for _, s := range c.songs {
			if s.Genre == "" {
				continue
			}
			counts[s.Genre]++
			if albums[s.Genre] == nil {
				albums[s.Genre] = map[string]bool{}
			}
			albums[s.Genre][s.AlbumID] = true
		}
		keys := make([]string, 0, len(counts))
		for key := range counts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		genres := []map[string]any{}
		for _, key := range keys {
			genres = append(genres, map[string]any{"value": key, "songCount": counts[key], "albumCount": len(albums[key])})
		}
		payload(result, "genres", map[string]any{"genre": genres})
	case "search2", "search3":
		if !r.Form.Has("query") {
			failure(result, 10, "Parameter query is required")
			return true
		}
		query := strings.ToLower(strings.TrimSpace(r.Form.Get("query")))
		arts := []artist{}
		albs := []album{}
		songs := []song{}
		for _, v := range withoutAlbums(c.artists) {
			if matches(v.Name, query) {
				arts = append(arts, v)
			}
		}
		for _, v := range withoutSongs(c.albums) {
			if matches(v.Name+" "+v.Artist, query) {
				albs = append(albs, v)
			}
		}
		for _, v := range c.songs {
			if matches(v.Title+" "+v.Artist+" "+v.Album, query) {
				songs = append(songs, v)
			}
		}
		ac, ao, ok := paging(r, result, "artistCount", "artistOffset", 20, maxResultCount)
		if !ok {
			return true
		}
		bc, bo, ok := paging(r, result, "albumCount", "albumOffset", 20, maxResultCount)
		if !ok {
			return true
		}
		sc, so, ok := paging(r, result, "songCount", "songOffset", 20, maxResultCount)
		if !ok {
			return true
		}
		arts = pageSlice(arts, ao, ac)
		albs = pageSlice(albs, bo, bc)
		songs = pageSlice(songs, so, sc)
		if endpoint == "search3" {
			result.Search = &searchResult{Artists: arts, Albums: albs, Songs: songs}
		} else {
			dirs := []map[string]any{}
			for _, alb := range albs {
				dirs = append(dirs, albumDirectory(alb))
			}
			payload(result, "searchResult2", map[string]any{"artist": arts, "album": dirs, "song": songs})
		}
	case "search":
		count, offset, ok := paging(r, result, "count", "offset", 20, maxResultCount)
		if !ok {
			return true
		}
		newer, err := int64Parameter(r, "newerThan", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		songs := []song{}
		for _, s := range c.songs {
			if !matches(s.Artist, strings.ToLower(r.Form.Get("artist"))) || !matches(s.Album, strings.ToLower(r.Form.Get("album"))) || !matches(s.Title, strings.ToLower(r.Form.Get("title"))) || !matches(s.Title+" "+s.Artist+" "+s.Album, strings.ToLower(r.Form.Get("any"))) {
				continue
			}
			if newer > 0 && c.tracks[s.ID].ModifiedAt.UnixMilli() <= newer {
				continue
			}
			songs = append(songs, s)
		}
		payload(result, "searchResult", map[string]any{"offset": offset, "totalHits": len(songs), "match": pageSlice(songs, offset, count)})
	case "getAlbumList", "getAlbumList2":
		a.albumList(r, result, c, endpoint)
	case "getRandomSongs", "getSongsByGenre":
		key := "size"
		if endpoint == "getSongsByGenre" {
			key = "count"
			required(r, result, "genre")
			if result.Error != nil {
				return true
			}
		}
		count, offset, ok := paging(r, result, key, "offset", 10, 500)
		if !ok {
			return true
		}
		from, err := int64Parameter(r, "fromYear", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		to, err := int64Parameter(r, "toYear", 9999)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		songs := []song{}
		for _, s := range c.songs {
			if g := r.Form.Get("genre"); g != "" && !strings.EqualFold(g, s.Genre) {
				continue
			}
			if int64(s.Year) < from || int64(s.Year) > to {
				continue
			}
			songs = append(songs, s)
		}
		name := "songsByGenre"
		if endpoint == "getRandomSongs" {
			rand.Shuffle(len(songs), func(i, j int) { songs[i], songs[j] = songs[j], songs[i] })
			name = "randomSongs"
			offset = 0
		}
		payload(result, name, map[string]any{"song": pageSlice(songs, offset, count)})
	case "getArtistInfo", "getArtistInfo2", "getAlbumInfo", "getAlbumInfo2", "getSimilarSongs", "getSimilarSongs2", "getTopSongs":
		a.enrichment(r, result, c, endpoint)

	}
	return true
}
func matches(value, query string) bool {
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(strings.ToLower(value), word) {
			return false
		}
	}
	return true
}
func required(r *http.Request, result *response, key string) string {
	value := r.Form.Get(key)
	if value == "" {
		failure(result, 10, "Parameter "+key+" is required")
	}
	return value
}
func paging(r *http.Request, result *response, countKey, offsetKey string, defaultCount, max int) (int, int, bool) {
	count, err := number(r, countKey, defaultCount, max)
	if err != nil {
		failure(result, 10, err.Error())
		return 0, 0, false
	}
	offset, err := number(r, offsetKey, 0, int(^uint(0)>>1))
	if err != nil {
		failure(result, 10, err.Error())
		return 0, 0, false
	}
	return count, offset, true
}
func int64Parameter(r *http.Request, key string, fallback int64) (int64, error) {
	value := r.Form.Get(key)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("Invalid " + key)
	}
	return n, nil
}
func albumDirectory(alb album) map[string]any {
	m := asMap(alb)
	delete(m, "song")
	m["title"] = alb.Name
	m["parent"] = alb.ArtistID
	m["isDir"] = true
	return m
}
func (a *api) albumList(r *http.Request, result *response, c collection, endpoint string) {
	kind := required(r, result, "type")
	if result.Error != nil {
		return
	}
	count, offset, ok := paging(r, result, "size", "offset", 10, 500)
	if !ok {
		return
	}
	albums := withoutSongs(c.albums)
	switch kind {
	case "random":
		rand.Shuffle(len(albums), func(i, j int) { albums[i], albums[j] = albums[j], albums[i] })
	case "alphabeticalByName":
		sort.SliceStable(albums, func(i, j int) bool { return strings.ToLower(albums[i].Name) < strings.ToLower(albums[j].Name) })
	case "alphabeticalByArtist":
		sort.SliceStable(albums, func(i, j int) bool {
			return strings.ToLower(albums[i].Artist+"\x00"+albums[i].Name) < strings.ToLower(albums[j].Artist+"\x00"+albums[j].Name)
		})
	case "newest":
		sort.SliceStable(albums, func(i, j int) bool { return albums[i].Created > albums[j].Created })
	case "highest", "starred":
		albums = []album{}
	case "frequent":
		sort.SliceStable(albums, func(i, j int) bool { return albums[i].PlayCount > albums[j].PlayCount })
	case "recent":
		filtered := []album{}
		for _, v := range albums {
			if !c.lastPlayed[v.ID].IsZero() {
				filtered = append(filtered, v)
			}
		}
		albums = filtered
		sort.SliceStable(albums, func(i, j int) bool { return c.lastPlayed[albums[i].ID].After(c.lastPlayed[albums[j].ID]) })
	case "byGenre":
		genre := required(r, result, "genre")
		if result.Error != nil {
			return
		}
		filtered := []album{}
		for _, v := range albums {
			if strings.EqualFold(v.Genre, genre) {
				filtered = append(filtered, v)
			}
		}
		albums = filtered
	case "byYear":
		required(r, result, "fromYear")
		required(r, result, "toYear")
		if result.Error != nil {
			return
		}
		from, err := int64Parameter(r, "fromYear", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return
		}
		to, err := int64Parameter(r, "toYear", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return
		}
		low, high := from, to
		if low > high {
			low, high = high, low
		}
		filtered := []album{}
		for _, v := range albums {
			if int64(v.Year) >= low && int64(v.Year) <= high {
				filtered = append(filtered, v)
			}
		}
		albums = filtered
		sort.SliceStable(albums, func(i, j int) bool {
			if from > to {
				return albums[i].Year > albums[j].Year
			}
			return albums[i].Year < albums[j].Year
		})
	default:
		failure(result, 10, "Unknown album list type")
		return
	}
	albums = pageSlice(albums, offset, count)
	name := "albumList2"
	if endpoint == "getAlbumList" {
		name = "albumList"
		dirs := []map[string]any{}
		for _, alb := range albums {
			dirs = append(dirs, albumDirectory(alb))
		}
		payload(result, name, map[string]any{"album": dirs})
		return
	}
	payload(result, name, map[string]any{"album": albums})
}
func (a *api) folders(r *http.Request, result *response, c collection, endpoint string) {
	dirs := map[string]string{"1": "."}
	children := map[string][]map[string]any{}
	// Directory nodes are derived from actual paths, not album tags.
	for _, s := range c.songs {
		track := c.tracks[s.ID]
		parent := path.Dir(track.Path)
		parts := strings.Split(parent, "/")
		current := "."
		if parent != "." {
			for _, part := range parts {
				next := path.Join(current, part)
				nextID := directoryID(next)
				if _, ok := dirs[nextID]; !ok {
					dirs[nextID] = next
					children[directoryID(current)] = append(children[directoryID(current)], map[string]any{"id": nextID, "parent": directoryID(current), "title": part, "isDir": true, "coverArt": s.ID})
				}
				current = next
			}
		}
		node := asMap(s)
		node["parent"] = directoryID(parent)
		children[directoryID(parent)] = append(children[directoryID(parent)], node)
	}
	if endpoint == "getIndexes" {
		indices := map[string][]map[string]any{}
		rootSongs := []map[string]any{}
		for _, node := range children["1"] {
			if node["isDir"] == true {
				name := node["title"].(string)
				group := "#"
				for _, r := range name {
					group = string(unicode.ToUpper(r))
					break
				}
				indices[group] = append(indices[group], map[string]any{"id": node["id"], "name": name})
			} else {
				rootSongs = append(rootSongs, node)
			}
		}
		keys := make([]string, 0, len(indices))
		for key := range indices {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		index := []map[string]any{}
		for _, key := range keys {
			index = append(index, map[string]any{"name": key, "artist": indices[key]})
		}
		modified := a.lib.LastScan()
		since, err := int64Parameter(r, "ifModifiedSince", 0)
		if err != nil {
			failure(result, 10, err.Error())
			return
		}
		if since > 0 && since >= modified {
			return
		}
		payload(result, "indexes", map[string]any{"lastModified": modified, "ignoredArticles": "The El La Los Las Le Les", "index": index, "child": rootSongs})
		return
	}
	key := required(r, result, "id")
	if result.Error != nil {
		return
	}
	if p, ok := dirs[key]; ok {
		nodes := children[key]
		if nodes == nil {
			nodes = []map[string]any{}
		}
		sort.SliceStable(nodes, func(i, j int) bool {
			if nodes[i]["isDir"] != nodes[j]["isDir"] {
				return nodes[i]["isDir"] == true
			}
			return strings.ToLower(nodes[i]["title"].(string)) < strings.ToLower(nodes[j]["title"].(string))
		})
		node := map[string]any{"id": key, "name": path.Base(p), "child": nodes}
		if key == "1" {
			node["name"] = "Music"
		} else {
			node["parent"] = directoryID(path.Dir(p))
		}
		payload(result, "directory", node)
		return
	}
	// Album IDs returned by legacy album lists must also resolve as directories.
	for _, alb := range c.albums {
		if alb.ID == key {
			node := albumDirectory(alb)
			node["name"] = alb.Name
			node["child"] = alb.Songs
			payload(result, "directory", node)
			return
		}
	}
	for _, art := range c.artists {
		if art.ID == key {
			nodes := []map[string]any{}
			for _, alb := range art.Albums {
				nodes = append(nodes, albumDirectory(alb))
			}
			payload(result, "directory", map[string]any{"id": art.ID, "name": art.Name, "child": nodes})
			return
		}
	}
	failure(result, 70, "Directory not found")
}

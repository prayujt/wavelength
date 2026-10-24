// Package subsonic projects application services onto the legacy Subsonic REST API.
package subsonic

import "encoding/xml"

type response struct {
	Payload       map[string]any `json:"-" xml:"-"`
	XMLName       xml.Name       `xml:"subsonic-response" json:"-"`
	XMLNS         string         `xml:"xmlns,attr" json:"-"`
	Status        string         `xml:"status,attr" json:"status"`
	Version       string         `xml:"version,attr" json:"version"`
	Type          string         `xml:"type,attr" json:"type"`
	ServerVersion string         `xml:"serverVersion,attr" json:"serverVersion"`
	Error         *apiError      `xml:"error,omitempty" json:"error,omitempty"`
	License       *license       `xml:"license,omitempty" json:"license,omitempty"`
	Folders       *folders       `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *artists       `xml:"artists,omitempty" json:"artists,omitempty"`
	Artist        *artist        `xml:"artist,omitempty" json:"artist,omitempty"`
	Album         *album         `xml:"album,omitempty" json:"album,omitempty"`
	Song          *song          `xml:"song,omitempty" json:"song,omitempty"`
	Search        *searchResult  `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
}
type apiError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}
type license struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}
type folders struct {
	Folders []folder `xml:"musicFolder" json:"musicFolder"`
}
type folder struct {
	ID   string `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}
type artists struct {
	IgnoredArticles string        `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []artistIndex `xml:"index" json:"index"`
}
type artistIndex struct {
	Name    string   `xml:"name,attr" json:"name"`
	Artists []artist `xml:"artist" json:"artist"`
}
type artist struct {
	ID         string  `xml:"id,attr" json:"id"`
	Name       string  `xml:"name,attr" json:"name"`
	AlbumCount int     `xml:"albumCount,attr" json:"albumCount"`
	CoverArt   string  `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Starred    string  `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Albums     []album `xml:"album,omitempty" json:"album,omitempty"`
}
type album struct {
	ID         string `xml:"id,attr" json:"id"`
	Name       string `xml:"name,attr" json:"name"`
	Artist     string `xml:"artist,attr" json:"artist"`
	ArtistID   string `xml:"artistId,attr" json:"artistId"`
	SongCount  int    `xml:"songCount,attr" json:"songCount"`
	Duration   int    `xml:"duration,attr" json:"duration"`
	Year       int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre      string `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	Created    string `xml:"created,attr" json:"created"`
	CoverArt   string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Starred    string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	PlayCount  int    `xml:"playCount,attr" json:"playCount"`
	UserRating int    `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	Songs      []song `xml:"song,omitempty" json:"song,omitempty"`
}
type song struct {
	IsVideo     bool   `json:"isVideo"`
	Path        string `json:"path,omitempty"`
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr" json:"parent"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
	Title       string `xml:"title,attr" json:"title"`
	Artist      string `xml:"artist,attr" json:"artist"`
	Album       string `xml:"album,attr" json:"album"`
	ArtistID    string `xml:"artistId,attr" json:"artistId"`
	AlbumID     string `xml:"albumId,attr" json:"albumId"`
	Track       int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year        int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	Size        int64  `xml:"size,attr" json:"size"`
	ContentType string `xml:"contentType,attr" json:"contentType"`
	Suffix      string `xml:"suffix,attr" json:"suffix"`
	Type        string `xml:"type,attr" json:"type"`
	Duration    int    `xml:"duration,attr" json:"duration"`
	Genre       string `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	DiscNumber  int    `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Created     string `xml:"created,attr" json:"created"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Starred     string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	UserRating  int    `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	PlayCount   int    `xml:"playCount,attr" json:"playCount"`
}
type searchResult struct {
	Artists []artist `xml:"artist" json:"artist"`
	Albums  []album  `xml:"album" json:"album"`
	Songs   []song   `xml:"song" json:"song"`
}

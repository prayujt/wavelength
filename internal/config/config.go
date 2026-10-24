// Package config reads and validates process configuration at startup.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	MusicDir      string   `env:"WAVELENGTH_MUSIC_DIR"`
	DataDir       string   `env:"WAVELENGTH_DATA_DIR" envDefault:"./data"`
	Addr          string   `env:"WAVELENGTH_ADDR" envDefault:"127.0.0.1:8080"`
	Token         string   `env:"WAVELENGTH_TOKEN"`
	PublicURL     string   `env:"WAVELENGTH_PUBLIC_URL"`
	OIDC          OIDC     `envPrefix:"WAVELENGTH_OIDC_"`
	Subsonic      Subsonic `envPrefix:"WAVELENGTH_SUBSONIC_"`
	LastFM        LastFM   `envPrefix:"WAVELENGTH_LASTFM_"`
	JukeboxPlayer string   `env:"WAVELENGTH_JUKEBOX_PLAYER"`
}

type OIDC struct {
	Issuer       string `env:"ISSUER"`
	ClientID     string `env:"CLIENT_ID"`
	ClientSecret string `env:"CLIENT_SECRET"`
}

type Subsonic struct {
	Addr      string `env:"ADDR"`
	User      string `env:"USER"`
	Password  string `env:"PASSWORD"`
	PublicURL string `env:"PUBLIC_URL"`
}

func (c Subsonic) Enabled() bool { return c.Password != "" }

type LastFM struct {
	APIKey   string   `env:"API_KEY"`
	Secret   string   `env:"SECRET"`
	Sessions Sessions `env:"SESSIONS"`
}

// Sessions preserves the JSON format used by existing deployments.
type Sessions map[string]string

func (s *Sessions) UnmarshalText(text []byte) error {
	var sessions map[string]string
	if err := json.Unmarshal(text, &sessions); err != nil {
		return errors.New("expected a JSON object mapping usernames to session keys")
	}
	*s = sessions
	return nil
}

func Load() (Config, error) {
	c, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, err
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	if c.Subsonic.Addr == "" {
		c.Subsonic.Addr = "127.0.0.1:8081"
	}
	if c.Subsonic.User == "" {
		c.Subsonic.User = "admin"
	}
	return c, nil
}

func (c Config) validate() error {
	if !c.Subsonic.Enabled() && (c.Subsonic.Addr != "" || c.Subsonic.User != "") {
		return errors.New("WAVELENGTH_SUBSONIC_PASSWORD is required to enable the legacy listener")
	}
	if c.Subsonic.Enabled() && c.MusicDir == "" {
		return errors.New("legacy Subsonic requires WAVELENGTH_MUSIC_DIR")
	}
	oidc := c.OIDC.Issuer != "" || c.OIDC.ClientID != "" || c.OIDC.ClientSecret != "" || c.PublicURL != ""
	if oidc && (c.OIDC.Issuer == "" || c.OIDC.ClientID == "" || c.PublicURL == "") {
		return errors.New("OIDC requires issuer, client ID and public URL")
	}
	return validateNativeAddress(c.Addr, c.Token != "" || oidc)
}

func validateNativeAddress(addr string, protected bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid native listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if !protected && (ip == nil || !ip.IsLoopback()) {
		return errors.New("OIDC or WAVELENGTH_TOKEN is required for non-loopback native listeners")
	}
	return nil
}

package config

import (
	"os"
	"strings"
	"testing"
)

func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "WAVELENGTH_") {
			value := os.Getenv(key)
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, value) })
		}
	}
}

func TestLoad(t *testing.T) {
	cleanEnvironment(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:8080" || c.DataDir != "./data" || c.Subsonic.Enabled() {
		t.Fatalf("unexpected defaults: %#v", c)
	}
	t.Setenv("WAVELENGTH_MUSIC_DIR", "/music")
	t.Setenv("WAVELENGTH_SUBSONIC_PASSWORD", "secret")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Subsonic.User != "admin" || c.Subsonic.Addr != "127.0.0.1:8081" {
		t.Fatal("nested configuration was not decoded")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, settings := range []map[string]string{
		{"WAVELENGTH_ADDR": "0.0.0.0:8080"},
		{"WAVELENGTH_SUBSONIC_USER": "admin"},
		{"WAVELENGTH_SUBSONIC_PASSWORD": "secret"},
		{"WAVELENGTH_OIDC_ISSUER": "https://issuer.example"},
	} {
		t.Run("invalid", func(t *testing.T) {
			cleanEnvironment(t)
			for key, value := range settings {
				t.Setenv(key, value)
			}
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestNativeBinding(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080"} {
		if validateNativeAddress(addr, false) == nil {
			t.Fatalf("unprotected public bind: %s", addr)
		}
		if err := validateNativeAddress(addr, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNativeAddress("127.0.0.1:8080", false); err != nil {
		t.Fatal(err)
	}
}

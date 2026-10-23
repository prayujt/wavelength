package state

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

// LoadKey creates the private key needed for reversible legacy app passwords.
func LoadKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "subsonic.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid Subsonic key")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(key); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

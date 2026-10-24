package subsonic

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// Salted MD5 is part of the legacy wire contract.
func authenticated(r *http.Request, user, password string) bool {
	if !equal(r.Form.Get("u"), user) {
		return false
	}
	if token := r.Form.Get("t"); token != "" {
		salt := r.Form.Get("s")
		if salt == "" {
			return false
		}
		expected := fmt.Sprintf("%x", md5.Sum([]byte(password+salt)))
		return equal(token, expected)
	}
	value := r.Form.Get("p")
	if strings.HasPrefix(value, "enc:") {
		decoded, err := hex.DecodeString(strings.TrimPrefix(value, "enc:"))
		if err != nil {
			return false
		}
		value = string(decoded)
	}
	return equal(value, password)
}
func equal(a, b string) bool {
	x := sha256.Sum256([]byte(a))
	y := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

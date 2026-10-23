package subsonic

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
)

type Option func(*api) error
type api struct {
	key            []byte
	scanQueued     atomic.Bool
	lib            *library.Library
	store          state.Store
	user, password string
	ctx            context.Context
}

func newAPI(lib *library.Library, user, password string) *api {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &api{key: key, lib: lib, store: &state.Memory{}, user: user, password: password, ctx: context.Background()}
}
func WithState(store state.Store) Option     { return func(a *api) error { a.store = store; return nil } }
func WithContext(ctx context.Context) Option { return func(a *api) error { a.ctx = ctx; return nil } }

var callbackPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)

func validateVersion(version string) *apiError {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return &apiError{10, "Invalid protocol version"}
	}
	values := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return &apiError{10, "Invalid protocol version"}
		}
		values[i] = n
	}
	if values[0] < 1 {
		return &apiError{20, "Client protocol must upgrade"}
	}
	if values[0] > 1 || values[1] > 16 {
		return &apiError{30, "Server protocol must upgrade"}
	}
	return nil
}

// Encode JSON and XML from the same payload to avoid divergent endpoint behavior.
func writeResponse(w http.ResponseWriter, r *http.Request, result response) {
	raw, _ := json.Marshal(result)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var body map[string]any
	_ = dec.Decode(&body)
	for k, v := range result.Payload {
		body[k] = v
	}
	format := r.Form.Get("f")
	if format == "json" || format == "jsonp" {
		data, err := json.Marshal(map[string]any{"subsonic-response": body})
		if err != nil {
			http.Error(w, "Response encoding failed", 500)
			return
		}
		if format == "jsonp" && callbackPattern.MatchString(r.Form.Get("callback")) {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = fmt.Fprintf(w, "%s(%s);", r.Form.Get("callback"), data)
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
		}
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	var out bytes.Buffer
	out.WriteString(xml.Header)
	encoder := xml.NewEncoder(&out)
	root := xml.StartElement{Name: xml.Name{Local: "subsonic-response"}, Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: "http://subsonic.org/restapi"}}}
	for _, key := range []string{"status", "version", "type", "serverVersion"} {
		root.Attr = append(root.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: fmt.Sprint(body[key])})
		delete(body, key)
	}
	_ = encoder.EncodeToken(root)
	keys := sortedKeys(body)
	for _, key := range keys {
		_ = encodeNode(encoder, key, body[key])
	}
	_ = encoder.EncodeToken(root.End())
	_ = encoder.Flush()
	_, _ = w.Write(out.Bytes())
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func encodeNode(e *xml.Encoder, name string, value any) error {
	// Normalize typed payloads using JSON's field names, preserving integer values.
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var normalized any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&normalized); err != nil {
		return err
	}
	return encodeValue(e, name, normalized)
}
func encodeValue(e *xml.Encoder, name string, value any) error {
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if err := encodeValue(e, name, item); err != nil {
				return err
			}
		}
		return nil
	}
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if object, ok := value.(map[string]any); ok {
		keys := sortedKeys(object)
		for _, key := range keys {
			switch object[key].(type) {
			case map[string]any, []any:
			default:
				if key != "value" && object[key] != nil {
					start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: fmt.Sprint(object[key])})
				}
			}
		}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		if text, ok := object["value"]; ok {
			if err := e.EncodeToken(xml.CharData(fmt.Sprint(text))); err != nil {
				return err
			}
		}
		for _, key := range keys {
			switch object[key].(type) {
			case map[string]any, []any:
				if err := encodeValue(e, key, object[key]); err != nil {
					return err
				}
			}
		}
	} else {
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		if value != nil {
			if err := e.EncodeToken(xml.CharData(fmt.Sprint(value))); err != nil {
				return err
			}
		}
	}
	return e.EncodeToken(start.End())
}
func failure(result *response, code int, message string) {
	result.Status = "failed"
	result.Error = &apiError{code, message}
}
func payload(result *response, name string, value any) {
	if result.Payload == nil {
		result.Payload = map[string]any{}
	}
	result.Payload[name] = value
}
func asMap(value any) map[string]any {
	body, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}
func pageSlice[T any](items []T, offset, count int) []T {
	if offset >= len(items) || count == 0 {
		return []T{}
	}
	end := offset + count
	if end < offset || end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func WithEncryptionKey(key []byte) Option {
	return func(a *api) error {
		if len(key) != 32 {
			return fmt.Errorf("credential key must be 32 bytes")
		}
		a.key = append([]byte(nil), key...)
		return nil
	}
}

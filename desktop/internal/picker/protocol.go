// Package picker runs a file picker in a disposable child process.
package picker

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ChildArg        = "--muxray-picker-child-v1"
	Version         = 1
	MaxMessageBytes = 64 * 1024
)

var (
	ErrProtocol    = errors.New("picker: invalid child protocol")
	ErrCanceled    = errors.New("picker: canceled")
	ErrChildFailed = errors.New("picker: child failed")
	ErrStart       = errors.New("picker: child could not start")
	ErrContainment = errors.New("picker: child containment failed")
	ErrCleanup     = errors.New("picker: child cleanup failed")
	ErrUnsupported = errors.New("picker: unsupported platform")
)

type Request struct {
	Version       int    `json:"version"`
	Nonce         string `json:"nonce"`
	Owner         string `json:"owner"`
	Kind          string `json:"kind"`
	ParentPID     uint32 `json:"parentPID"`
	ParentCreated string `json:"parentCreated"`
}

type Response struct {
	Version int    `json:"version"`
	Nonce   string `json:"nonce"`
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

func validNonce(n string) bool {
	if len(n) != 32 || strings.ToLower(n) != n {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

func decimal(s string, bits int) bool {
	n, err := strconv.ParseUint(s, 10, bits)
	return err == nil && n != 0 && strconv.FormatUint(n, 10) == s
}

func validRequest(r Request) bool {
	return r.Version == Version && validNonce(r.Nonce) && decimal(r.Owner, strconv.IntSize) &&
		r.Kind == "open" && r.ParentPID != 0 && decimal(r.ParentCreated, 64)
}

// readMessage waits for EOF. Closing stdin is part of the request handshake;
// the child must not initialize COM or show UI until ReadRequest succeeds.
func readMessage(r io.Reader, v any) error {
	b, err := io.ReadAll(io.LimitReader(r, MaxMessageBytes+1))
	if err != nil || len(b) > MaxMessageBytes || !utf8.Valid(b) || !validUnicodeEscapes(b) {
		return ErrProtocol
	}
	var allowed map[string]bool
	switch v.(type) {
	case *Request:
		allowed = map[string]bool{"version": true, "nonce": true, "owner": true, "kind": true, "parentPID": true, "parentCreated": true}
	case *Response:
		allowed = map[string]bool{"version": true, "nonce": true, "status": true, "path": true, "error": true}
	default:
		return ErrProtocol
	}
	// Reject duplicate fields as well as unknown fields and extra JSON values.
	tokens := json.NewDecoder(bytes.NewReader(b))
	t, err := tokens.Token()
	if err != nil || t != json.Delim('{') {
		return ErrProtocol
	}
	seen := map[string]bool{}
	for tokens.More() {
		t, err := tokens.Token()
		if err != nil {
			return ErrProtocol
		}
		key, ok := t.(string)
		if !ok || !allowed[key] || seen[key] {
			return ErrProtocol
		}
		seen[key] = true
		var value json.RawMessage
		if tokens.Decode(&value) != nil {
			return ErrProtocol
		}
	}
	if t, err = tokens.Token(); err != nil || t != json.Delim('}') {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrProtocol
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrProtocol
	}
	return nil
}

// encoding/json replaces lone escaped UTF-16 surrogates with U+FFFD. Reject
// them instead of silently changing a path or another protocol field.
func validUnicodeEscapes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		u, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if u >= 0xdc00 && u <= 0xdfff {
			return false
		}
		if u >= 0xd800 && u <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func ReadRequest(r io.Reader) (Request, error) {
	var req Request
	if err := readMessage(r, &req); err != nil || !validRequest(req) {
		return Request{}, ErrProtocol
	}
	return req, nil
}

func writeMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil || len(b)+1 > MaxMessageBytes {
		return ErrProtocol
	}
	b = append(b, '\n')
	n, err := w.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

func WriteRequest(w io.Writer, r Request) error {
	if !validRequest(r) {
		return ErrProtocol
	}
	return writeMessage(w, r)
}

func validPath(path string) bool {
	if !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, c := range path {
		if c < 32 {
			return false
		}
	}
	if strings.HasPrefix(path, `\\?\`) {
		path = strings.TrimPrefix(path, `\\?\`)
		if strings.HasPrefix(strings.ToUpper(path), `UNC\`) {
			return validUNC(path[4:])
		}
		return driveAbsolute(path)
	}
	if driveAbsolute(path) {
		return true
	}
	// Allow filesystem UNC paths, excluding Win32 device namespaces.
	if strings.HasPrefix(path, `\\`) {
		return validUNC(strings.TrimPrefix(path, `\\`))
	}
	return false
}

func driveAbsolute(path string) bool {
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func validUNC(path string) bool {
	parts := strings.Split(path, `\`)
	return len(parts) >= 2 && parts[0] != "" && parts[1] != "" && parts[0] != "." && parts[0] != "?"
}

func validResponse(r Response) bool {
	if r.Version != Version || !validNonce(r.Nonce) {
		return false
	}
	switch r.Status {
	case "selected":
		return r.Error == "" && validPath(r.Path)
	case "canceled":
		return r.Error == "" && r.Path == ""
	case "error":
		if r.Path != "" || len(r.Error) == 0 || len(r.Error) > 64 {
			return false
		}
		for _, c := range r.Error {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				return false
			}
		}
		return true
	}
	return false
}

func WriteResponse(w io.Writer, r Response) error {
	if !validResponse(r) {
		return ErrProtocol
	}
	return writeMessage(w, r)
}

func readResponse(r io.Reader, nonce string) (Response, error) {
	var response Response
	if err := readMessage(r, &response); err != nil || !validResponse(response) || response.Nonce != nonce {
		return Response{}, ErrProtocol
	}
	return response, nil
}

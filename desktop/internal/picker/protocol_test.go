package picker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func testRequest() Request {
	return Request{Version: 1, Nonce: strings.Repeat("a", 32), Owner: "123", Kind: "open", ParentPID: 42, ParentCreated: "134358139573658578"}
}

func TestRequestProtocol(t *testing.T) {
	r := testRequest()
	var b bytes.Buffer
	if err := WriteRequest(&b, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(&b)
	if err != nil || got != r {
		t.Fatalf("round trip: %#v %v", got, err)
	}
	valid, _ := json.Marshal(r)
	for name, data := range map[string][]byte{
		"empty": nil, "null": []byte("null"), "invalid_utf8": {0xff}, "oversize": bytes.Repeat([]byte(" "), MaxMessageBytes+1),
		"extra_json":     append(append([]byte{}, valid...), []byte("{}")...),
		"duplicate":      []byte(strings.Replace(string(valid), `"version":1`, `"version":1,"version":1`, 1)),
		"case_alias":     []byte(strings.Replace(string(valid), `"version":1`, `"Version":1`, 1)),
		"case_duplicate": []byte(strings.Replace(string(valid), `"version":1`, `"version":1,"VERSION":1`, 1)),
		"unknown":        []byte(strings.Replace(string(valid), `"version":1`, `"extra":true,"version":1`, 1)),
		"owner_overflow": []byte(strings.Replace(string(valid), `"123"`, `"18446744073709551616"`, 1)),
		"wrong_version":  []byte(strings.Replace(string(valid), `"version":1`, `"version":2`, 1)),
		"owner_zero":     []byte(strings.Replace(string(valid), `"123"`, `"0"`, 1)),
		"owner_hex":      []byte(strings.Replace(string(valid), `"123"`, `"0x123"`, 1)),
		"parent_zero":    []byte(strings.Replace(string(valid), `"parentPID":42`, `"parentPID":0`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadRequest(bytes.NewReader(data)); !errors.Is(err, ErrProtocol) {
				t.Fatalf("accepted malformed request: %v", err)
			}
		})
	}
}

func TestRequestWaitsForEOF(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	done := make(chan error, 1)
	go func() { _, err := ReadRequest(r); done <- err }()
	data, _ := json.Marshal(testRequest())
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("request accepted before EOF: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF did not release request")
	}
}

func TestResponseProtocol(t *testing.T) {
	nonce := testRequest().Nonce
	for _, r := range []Response{{Version: 1, Nonce: nonce, Status: "selected", Path: `C:\media\片段.flv`}, {Version: 1, Nonce: nonce, Status: "canceled"}, {Version: 1, Nonce: nonce, Status: "error", Error: "dialog_result"}} {
		var b bytes.Buffer
		if err := WriteResponse(&b, r); err != nil {
			t.Fatal(err)
		}
		got, err := readResponse(&b, nonce)
		if err != nil || got != r {
			t.Fatalf("round trip: %#v %v", got, err)
		}
	}
	for name, r := range map[string]Response{
		"nonce_mismatch":       {Version: 1, Nonce: strings.Repeat("b", 32), Status: "canceled"},
		"relative":             {Version: 1, Nonce: nonce, Status: "selected", Path: `media\sample.flv`},
		"device":               {Version: 1, Nonce: nonce, Status: "selected", Path: `\\.\PhysicalDrive0`},
		"cancel_with_path":     {Version: 1, Nonce: nonce, Status: "canceled", Path: `C:\secret.flv`},
		"error_with_path_text": {Version: 1, Nonce: nonce, Status: "error", Error: `C:\secret.flv`},
		"embedded_null":        {Version: 1, Nonce: nonce, Status: "selected", Path: "C:\\test\x00.flv"},
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(r)
			if _, err := readResponse(bytes.NewReader(data), nonce); !errors.Is(err, ErrProtocol) {
				t.Fatalf("accepted malformed response: %v", err)
			}
		})
	}
}

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestShortWrite(t *testing.T) {
	if !errors.Is(WriteRequest(shortWriter{}, testRequest()), io.ErrShortWrite) {
		t.Fatal("short write not detected")
	}
}

func TestStrictUnicodeAndWindowsPaths(t *testing.T) {
	nonce := testRequest().Nonce
	for _, path := range []string{`C:\\file\ud800.flv`, `C:\\file\udc00.flv`, `C:\\file\ud800\u0041.flv`} {
		data := []byte(`{"version":1,"nonce":"` + nonce + `","status":"selected","path":"` + path + `"}`)
		if _, err := readResponse(bytes.NewReader(data), nonce); !errors.Is(err, ErrProtocol) {
			t.Fatal("unpaired escaped surrogate accepted")
		}
	}
	data := []byte(`{"version":1,"nonce":"` + nonce + `","status":"selected","path":"C:\\file\ud83d\ude00.flv"}`)
	if _, err := readResponse(bytes.NewReader(data), nonce); err != nil {
		t.Fatal("paired surrogate rejected", err)
	}
	for _, path := range []string{`C:\file.flv`, `\\server\share\file.flv`, `\\?\C:\long\file.flv`, `\\?\UNC\server\share\file.flv`} {
		if !validPath(path) {
			t.Fatal("filesystem path rejected")
		}
	}
	for _, path := range []string{`\\?\GLOBALROOT\Device\HarddiskVolume1`, `\\.\PhysicalDrive0`, `relative.flv`, `C:relative.flv`} {
		if validPath(path) {
			t.Fatal("non-filesystem namespace accepted")
		}
	}
}

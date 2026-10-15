package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/session"
)

func TestSnapshotV4RealPagesNoIOAndLegacyIsolation(t *testing.T) {
	for _, kind := range []string{"mp3", "sei"} {
		t.Run(kind, func(t *testing.T) {
			path, _ := codecSearchFixture(t, kind)
			app := New(nil)
			cleanupApp(t, app)
			b := app.Bindings()
			id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
			finishReal(t, app, id)
			q := SnapshotRequest{SchemaVersion: 4, SessionID: id}
			capture, failure := app.CaptureSnapshot(q)
			report := decodeSnapshot(t, capture, failure)
			if len(report.Omitted) != 9 || snapshotOmissions(report)["codecSearchPage"] != "not_requested" || !bytes.Contains(capture.Data, []byte(`"codecSearchPage":null`)) {
				t.Fatalf("minimal %s", capture.Data)
			}
			if kind == "sei" {
				snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v4.json", report)
			}
			cursor := "0"
			var want CodecSearchResponse
			for i := range map[string]int{"mp3": 3, "sei": 2}[kind] {
				req := decimal(uint64(9007199254740993 + i))
				b.StartCodecSearch(CodecSearchRequest{id, req, cursor, kind, "all"})
				want = finishCodecSearch(t, b, id, req)
				if want.Error != nil || want.Page == nil {
					t.Fatal(want)
				}
				if q.CodecSearch != nil {
					old, oldFailure := app.CaptureSnapshot(q)
					oldReport := decodeSnapshot(t, old, oldFailure)
					if oldReport.CodecSearchPage != nil || snapshotOmissions(oldReport)["codecSearchPage"] != CodeStaleRequest {
						t.Fatal("superseded page was exported")
					}
				}
				q.CodecSearch = &SnapshotCodecSearchRequest{req, cursor, kind, "all"}
				capture, failure = app.CaptureSnapshot(q)
				report = decodeSnapshot(t, capture, failure)
				if !reflect.DeepEqual(report.CodecSearchPage, &want) {
					t.Fatalf("retained page mismatch: %+v omissions=%+v", want.Page, report.Omitted)
				}
				cursor = want.Page.NextCursor
			}
			// A separate source read does not discard an already completed page.
			b.StartHex(HexRequest{id, "9007199254740996", "0", 4096})
			if got := finishHex(t, b, id, "9007199254740996"); got.Error != nil {
				t.Fatal(got.Error)
			}
			q.HexRequestID = snapshotText("9007199254740996")
			if err := os.WriteFile(path, []byte("changed but not discovered"), 0600); err != nil {
				t.Fatal(err)
			}
			capture, failure = app.CaptureSnapshot(q)
			report = decodeSnapshot(t, capture, failure)
			if !reflect.DeepEqual(report.CodecSearchPage, &want) {
				t.Fatal("capture reread or replaced evidence")
			}
			name := "full"
			if kind == "mp3" {
				name = "mp3"
			}
			snapshotGolden(t, "desktop-diagnostic-snapshot-"+name+"-v4.json", report)
			for _, version := range []uint8{0, 1, 2, 3} {
				old := q
				old.SchemaVersion = version
				_, failure = app.CaptureSnapshot(old)
				checkFault(t, failure, CodeInvalidArgument)
				old.CodecSearch = nil
				legacy, fault := app.CaptureSnapshot(old)
				decodeSnapshot(t, legacy, fault)
				if bytes.Contains(legacy.Data, []byte("codecSearch")) {
					t.Fatal("legacy contract gained v4 keys")
				}
			}
			frozen := bytes.Clone(capture.Data)
			b.StartHex(HexRequest{id, "9007199254740997", "0", 4})
			if got := finishHex(t, b, id, "9007199254740997"); got.Error == nil || got.Error.Code != CodeSourceChanged {
				t.Fatal("source change did not latch")
			}
			_, failure = app.CaptureSnapshot(q)
			checkFault(t, failure, CodeSourceChanged)
			b.CloseFile(id)
			if !bytes.Equal(frozen, capture.Data) {
				t.Fatal("capture changed after close")
			}
		})
	}
}

type snapshotCodecOverride struct {
	scanSession
	result document.CodecSearchResult
	err    error
}

func (s *snapshotCodecOverride) SnapshotCodecSearch(uint64) (document.CodecSearchResult, error) {
	return s.result, s.err
}

func TestSnapshotV4RejectsMismatchedAndMalformedRetainedPages(t *testing.T) {
	path, _ := codecSearchFixture(t, "mp3")
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	cursor := "0"
	for _, req := range []string{"1", "2", "3"} {
		b.StartCodecSearch(CodecSearchRequest{id, req, cursor, "mp3", "all"})
		page := finishCodecSearch(t, b, id, req)
		if req != "3" {
			cursor = page.Page.NextCursor
		}
	}
	app.mu.Lock()
	e := app.findLocked(id)
	result, err := e.scan.(snapshotCodecSearchSession).SnapshotCodecSearch(3)
	wrapper := &snapshotCodecOverride{scanSession: e.scan, result: result}
	e.scan = wrapper
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	q := SnapshotRequest{SchemaVersion: 4, SessionID: id, CodecSearch: &SnapshotCodecSearchRequest{"3", cursor, "mp3", "all"}}
	for _, tc := range []struct {
		err  error
		code string
	}{{document.ErrStaleRequest, CodeStaleRequest}, {document.ErrSourceChanged, CodeSourceChanged}, {document.ErrUnknownRequest, CodeCodecSearchNotFound}, {session.ErrNotReady, CodeNotReady}} {
		wrapper.err = tc.err
		capture, failure := app.CaptureSnapshot(q)
		report := decodeSnapshot(t, capture, failure)
		if report.CodecSearchPage != nil || snapshotOmissions(report)["codecSearchPage"] != tc.code {
			t.Fatal(report.Omitted)
		}
	}
	wrapper.err = nil
	for _, state := range []string{"pending", "canceling"} {
		wrapper.result.State = state
		capture, failure := app.CaptureSnapshot(q)
		report := decodeSnapshot(t, capture, failure)
		if report.CodecSearchPage != nil || snapshotOmissions(report)["codecSearchPage"] != CodeNotReady {
			t.Fatal("unfinished page exported")
		}
	}
	wrapper.result.State = "ready"
	for _, mutate := range []func(*document.CodecSearchResult){
		func(r *document.CodecSearchResult) { r.RequestID-- },
		func(r *document.CodecSearchResult) { r.Page.Cursor++ },
		func(r *document.CodecSearchResult) { r.Page.Kind = "sei" },
		func(r *document.CodecSearchResult) { r.Page.Filter = "stops" },
		func(r *document.CodecSearchResult) { r.Page.Total++ },
		func(r *document.CodecSearchResult) { r.Page.Next.FrameIndex = 1 },
		func(r *document.CodecSearchResult) { r.Page.ScannedTags = 257 },
		func(r *document.CodecSearchResult) { r.Page.ScannedFrames = 1025 },
		func(r *document.CodecSearchResult) { r.Page.BytesRead = 65537 },
		func(r *document.CodecSearchResult) { r.Page.Pending = 1 },
		func(r *document.CodecSearchResult) { r.Page.NextCursor = 1 },
		func(r *document.CodecSearchResult) { r.Page.Rows[0].Span.ByteOffset = 0 },
		func(r *document.CodecSearchResult) { r.Page.Rows[0].Span.BitLength = 0 },
		func(r *document.CodecSearchResult) { r.Page.Rows[0].DTS++ },
		func(r *document.CodecSearchResult) { r.Page.Rows[0].Offset++ },
		func(r *document.CodecSearchResult) { r.Page.Rows[0].Complete = false },
		func(r *document.CodecSearchResult) {
			r.Page.Rows[0].Values = append(r.Page.Rows[0].Values, r.Page.Rows[0].Values[0])
		},
	} {
		encoded, _ := json.Marshal(result)
		var cloned document.CodecSearchResult
		if err := json.Unmarshal(encoded, &cloned); err != nil {
			t.Fatal(err)
		}
		wrapper.result = cloned
		mutate(&wrapper.result)
		capture, failure := app.CaptureSnapshot(q)
		report := decodeSnapshot(t, capture, failure)
		if report.CodecSearchPage != nil || snapshotOmissions(report)["codecSearchPage"] != "identity_mismatch" {
			t.Fatalf("bad page escaped: %+v %+v", wrapper.result.Page, report.Omitted)
		}
	}
	for _, selection := range []SnapshotCodecSearchRequest{{"0", "0", "mp3", "all"}, {"01", "0", "mp3", "all"}, {"3", "01", "mp3", "all"}, {"3", "0", "mp3", "unknown"}, {"3", "0", "sei", "parameters"}} {
		q.CodecSearch = &selection
		_, failure := app.CaptureSnapshot(q)
		checkFault(t, failure, CodeInvalidArgument)
	}
}

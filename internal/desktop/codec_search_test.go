package desktop

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func codecSearchFixture(t *testing.T, kind string) (string, []byte) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	add := func(typ byte, p []byte) {
		h := make([]byte, 11)
		h[0] = typ
		h[1] = byte(len(p) >> 16)
		h[2] = byte(len(p) >> 8)
		h[3] = byte(len(p))
		data = append(data, h...)
		data = append(data, p...)
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(11+len(p)))
		data = append(data, n[:]...)
	}
	if kind == "mp3" {
		p := make([]byte, 1+417*3000)
		p[0] = 0x2f
		for i := 0; i < 3000; i++ {
			copy(p[1+i*417:], []byte{0xff, 0xfb, 0x90, 0x64})
		}
		p[1+2600*417+3] = 0xe4
		add(8, p)
		add(8, []byte{0x2f, 0, 0, 0, 0})
	} else {
		add(9, []byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xff, 0xe0, 0})
		p := []byte{0x27, 1, 0, 0, 0}
		for _, count := range []int{64, 64, 2} {
			nal := []byte{6}
			for i := 0; i < count; i++ {
				nal = append(nal, 3, 1, 0x80)
			}
			nal = append(nal, 0x80)
			var size [4]byte
			binary.BigEndian.PutUint32(size[:], uint32(len(nal)))
			p = append(p, size[:]...)
			p = append(p, nal...)
		}
		add(9, p)
	}
	path := filepath.Join(t.TempDir(), kind+"-search.flv")
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	return path, data
}
func finishCodecSearch(t *testing.T, b *Bindings, session, id string) CodecSearchResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r := b.GetCodecSearch(DetailQuery{session, id})
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("codec search pending")
		default:
			runtime.Gosched()
		}
	}
}
func TestCodecSearchAPIContinuationAndSource(t *testing.T) {
	for _, kind := range []string{"mp3", "sei"} {
		t.Run(kind, func(t *testing.T) {
			path, data := codecSearchFixture(t, kind)
			app := New(nil)
			cleanupApp(t, app)
			b := app.Bindings()
			session := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
			finishReal(t, app, session)
			q := CodecSearchRequest{SessionID: session, RequestID: "9007199254740993", Cursor: "0", Kind: kind, Filter: "all"}
			checkFault(t, b.GetCodecSearch(DetailQuery{session, q.RequestID}).Error, CodeCodecSearchNotFound)
			var rows []CodecSearchRow
			for i := 0; i < 3; i++ {
				q.RequestID = decimal(uint64(9007199254740993 + i))
				if r := b.StartCodecSearch(q); r.Error != nil {
					t.Fatal(r.Error)
				}
				r := finishCodecSearch(t, b, session, q.RequestID)
				if r.Error != nil || r.Page == nil || r.Page.Kind != kind || r.Page.Cursor != q.Cursor || r.Page.Rows == nil {
					t.Fatalf("%+v", r)
				}
				if i < 2 && (r.Page.Exhausted || r.Page.NextCursor != q.RequestID) {
					t.Fatalf("continuation %+v", r.Page)
				}
				if kind == "sei" && i == 1 && (r.Page.ScannedTags != 0 || r.Page.BytesRead != "0" || r.Page.Start.TagID != "1" || r.Page.Next.TagID != "1" || len(r.Page.Rows) != 64) {
					t.Fatalf("same Tag pending %+v", r.Page)
				}
				for _, row := range r.Page.Rows {
					if row.Span == nil {
						t.Fatal("missing physical source")
					}
					offset, valid := parseStart(row.Span.ByteOffset)
					if !valid || offset >= uint64(len(data)) {
						t.Fatal("source outside file")
					}
				}
				rows = append(rows, r.Page.Rows...)
				q.Cursor = r.Page.NextCursor
			}
			if kind == "mp3" {
				if len(rows) != 3 || *rows[0].FrameIndex != 2600 || rows[0].Kind != "parameters" || rows[2].Kind != "stop" || rows[2].Status != "malformedHeader" {
					t.Fatalf("MP3 rows %+v", rows)
				}
			} else {
				if len(rows) != 130 || *rows[129].MessageIndex != 1 || *rows[129].NALIndex != 2 {
					t.Fatalf("SEI count %d", len(rows))
				}
			}
			encoded, e := json.Marshal(rows)
			if e != nil || strings.Contains(string(encoded), path) || !strings.Contains(string(encoded), `"values":`) {
				t.Fatal("wire contract")
			}
			q.RequestID = "9007199254740996"
			q.Cursor = "999"
			checkFault(t, b.StartCodecSearch(q).Error, CodeCodecSearchCursorExpired)
			b.CloseFile(session)
			checkFault(t, b.GetCodecSearch(DetailQuery{session, q.RequestID}).Error, CodeSessionClosed)
		})
	}
}
func TestCodecSearchAPIValidation(t *testing.T) {
	path, _ := codecSearchFixture(t, "mp3")
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	session := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, session)
	for _, bad := range []string{"", "00", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("1", 10000)} {
		checkFault(t, b.StartCodecSearch(CodecSearchRequest{session, "1", bad, "mp3", "all"}).Error, CodeInvalidArgument)
		checkFault(t, b.GetCodecSearch(DetailQuery{session, bad}).Error, CodeInvalidArgument)
	}
	for _, q := range []CodecSearchRequest{{session, "0", "0", "mp3", "all"}, {session, "1", "0", "bad", "all"}, {session, "1", "0", "mp3", "pictureTiming"}, {session, "1", "0", "sei", "parameters"}} {
		checkFault(t, b.StartCodecSearch(q).Error, CodeInvalidArgument)
	}
}

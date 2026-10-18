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

func finishTimeSearch(t *testing.T, b *Bindings, session, id string) TimeSearchResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r := b.GetTimeSearch(DetailQuery{session, id})
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("time search pending")
		default:
			runtime.Gosched()
		}
	}
}
func TestTimeSearchAPIWireContractAndContinuation(t *testing.T) {
	data := fixture(9000)
	// The only observation is a raw uint32 wrap at the last batch boundary.
	for i := 0; i < 9000; i++ {
		stamp := uint32(i)
		if i == 8191 {
			stamp = ^uint32(0)
		}
		o := 13 + 15*i
		data[o+4], data[o+5], data[o+6], data[o+7] = byte(stamp>>16), byte(stamp>>8), byte(stamp), byte(stamp>>24)
		binary.BigEndian.PutUint32(data[o+11:o+15], 11)
	}
	path := filepath.Join(t.TempDir(), "time.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	session := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, session)
	q := TimeSearchRequest{session, "9007199254740993", "0", "regressions", "1000"}
	checkFault(t, b.GetTimeSearch(DetailQuery{session, q.RequestID}).Error, CodeTimeSearchNotFound)
	for i := 0; i < 3; i++ {
		q.RequestID = decimal(uint64(9007199254740993 + i))
		if r := b.StartTimeSearch(q); r.Error != nil {
			t.Fatal(r.Error)
		}
		r := finishTimeSearch(t, b, session, q.RequestID)
		if r.Error != nil || r.APIVersion != APIVersion || r.RequestID != q.RequestID || r.Page == nil || r.Page.Rows == nil || r.Page.Cursor != q.Cursor || r.Page.GapThreshold != "1000" || r.Page.Filter != "regressions" || r.Page.Total != "9000" || !r.Page.ScanComplete || r.Page.UnknownPredecessors != 0 {
			t.Fatalf("wire %+v", r)
		}
		if i < 2 {
			if r.Page.Exhausted || r.Page.NextCursor != q.RequestID || r.Page.Scanned != 4096 || len(r.Page.Rows) != 0 {
				t.Fatalf("batch %+v", r.Page)
			}
		} else {
			if !r.Page.Exhausted || r.Page.NextCursor != "0" || r.Page.Start != "8192" || r.Page.Next != "9000" || r.Page.Scanned != 808 || len(r.Page.Rows) != 1 {
				t.Fatalf("last %+v", r.Page)
			}
			p := r.Page.Rows[0].Point
			if r.Page.Rows[0].Kind != "regression" || p.TagID != "8192" || *p.PreviousTagID != "8191" || *p.PreviousDTS != "4294967295" || *p.DeltaMS != "-4294959103" || p.Context != "found" {
				t.Fatalf("point %+v", p)
			}
			encoded, err := json.Marshal(r)
			if err != nil || strings.Contains(string(encoded), path) || !strings.Contains(string(encoded), `"tagId":"8192"`) {
				t.Fatal("wire numeric/path contract", err)
			}
		}
		q.Cursor = r.Page.NextCursor
	}
	q.RequestID, q.Cursor = "9007199254740996", "999"
	checkFault(t, b.StartTimeSearch(q).Error, CodeTimeSearchCursorExpired)
	b.CloseFile(session)
	checkFault(t, b.GetTimeSearch(DetailQuery{session, q.RequestID}).Error, CodeSessionClosed)
}
func TestTimeSearchAPIValidation(t *testing.T) {
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	q := TimeSearchRequest{"missing", "1", "0", "all", "1000"}
	for _, bad := range []string{"", "00", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("1", 10000)} {
		copy := q
		copy.Cursor = bad
		checkFault(t, b.StartTimeSearch(copy).Error, CodeInvalidArgument)
		copy = q
		copy.RequestID = bad
		checkFault(t, b.StartTimeSearch(copy).Error, CodeInvalidArgument)
		copy = q
		copy.GapThreshold = bad
		checkFault(t, b.StartTimeSearch(copy).Error, CodeInvalidArgument)
		checkFault(t, b.GetTimeSearch(DetailQuery{q.SessionID, bad}).Error, CodeInvalidArgument)
	}
	for _, threshold := range []string{"0", "4294967296"} {
		copy := q
		copy.GapThreshold = threshold
		checkFault(t, b.StartTimeSearch(copy).Error, CodeInvalidArgument)
	}
	for _, filter := range []string{"", "regression", "invalid"} {
		copy := q
		copy.Filter = filter
		checkFault(t, b.StartTimeSearch(copy).Error, CodeInvalidArgument)
	}
	for _, threshold := range []string{"1", "4294967295"} {
		copy := q
		copy.GapThreshold = threshold
		checkFault(t, b.StartTimeSearch(copy).Error, CodeSessionNotFound)
	}
}

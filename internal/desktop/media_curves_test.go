package desktop

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
)

func TestMediaCurvesAPIIdentityBoundsAndNulls(t *testing.T) {
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: detailFixture(t)}).Session.SessionID
	finishReal(t, app, id)
	q := MediaCurveRequest{id, "9007199254740993", "0", 0}
	checkFault(t, b.GetMediaCurves(q).Error, CodeSummaryNotFound)
	b.StartSummary(SummaryQuery{id, q.RequestID})
	if r := finishSummary(t, b, id, q.RequestID); r.Error != nil {
		t.Fatal(r.Error)
	}
	r := b.GetMediaCurves(q)
	if r.Error != nil || r.APIVersion != APIVersion || r.SessionID != id || r.RequestID != q.RequestID || r.Page == nil || r.Page.Bounds != (PageBounds{"0", 64, "1", "1"}) || r.Page.WindowTags != 256 || r.Page.IndexedTags != "2" {
		t.Fatalf("curve response: %+v", r)
	}
	point := r.Page.Points[0]
	if point.Audio.BitsPerSecond != nil || point.Audio.TagID != nil || point.GOP.AverageDurationMS != nil || point.GOP.TagID != nil || point.GOP.ClosedCount != "0" {
		t.Fatalf("unknown became zero: %+v", point)
	}
	data, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(data), `"bitsPerSecond":null`) || !strings.Contains(string(data), `"requestId":"9007199254740993"`) {
		t.Fatalf("wire precision/nulls: %s %v", data, err)
	}
	for _, bad := range []MediaCurveRequest{{id, "0", "0", 1}, {id, "01", "0", 1}, {id, q.RequestID, "01", 1}, {id, q.RequestID, "0", 129}, {id, q.RequestID, "2", 1}} {
		checkFault(t, b.GetMediaCurves(bad).Error, CodeInvalidArgument)
	}
	q.Start = "1"
	if end := b.GetMediaCurves(q); end.Error != nil || end.Page.Points == nil || len(end.Page.Points) != 0 {
		t.Fatalf("empty page: %+v", end)
	}
	b.StartSummary(SummaryQuery{id, "9007199254740994"})
	checkFault(t, b.GetMediaCurves(q).Error, CodeStaleRequest)
	b.CloseFile(id)
	checkFault(t, b.GetMediaCurves(q).Error, CodeSessionClosed)
}

func TestCurveRateProjectionRetainsUint64Precision(t *testing.T) {
	r := mapCurveRate(analysis.MediaCurveRate{Present: true, CoveredBytes: 1<<53 + 1, CoveredIntervalMS: 26, BitsPerSecond: math.MaxUint64, TagID: 1<<53 + 3})
	if r.CoveredBytes != "9007199254740993" || *r.BitsPerSecond != "18446744073709551615" || *r.TagID != "9007199254740995" {
		t.Fatalf("precision lost: %+v", r)
	}
}

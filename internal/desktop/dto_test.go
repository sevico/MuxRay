package desktop

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestExactIntegerDTOs(t *testing.T) {
	const offset = uint64(1<<53 + 1)
	for _, dts := range []int64{math.MinInt64, -1, 0, math.MaxInt64} {
		original := flv.TagRecord{ID: math.MaxUint64, Offset: offset, DTS: dts, RawTypeFlags: 18, DataSize: 3, AvailableDataSize: 3}
		view := mapTag(original)
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var generic map[string]any
		if err := json.Unmarshal(encoded, &generic); err != nil {
			t.Fatal(err)
		}
		if generic["id"] != "18446744073709551615" || generic["offset"] != "9007199254740993" || generic["dts"] != strconv.FormatInt(dts, 10) || generic["payloadSpan"].(map[string]any)["byteOffset"] != "9007199254741004" {
			t.Fatalf("lossy JSON transport: %s", encoded)
		}
		var roundTrip Tag
		if err := json.Unmarshal(encoded, &roundTrip); err != nil || !reflect.DeepEqual(view, roundTrip) {
			t.Fatalf("tag round trip: %s %v", encoded, err)
		}
	}
	maximum, zero := uint64(math.MaxUint64), uint64(0)
	diagnostic := mapDiagnostic(flv.Diagnostic{Offset: maximum, Expected: &maximum, Actual: &zero, Severity: flv.SeverityError})
	if diagnostic.Offset != "18446744073709551615" || *diagnostic.Expected != diagnostic.Offset || *diagnostic.Actual != "0" {
		t.Fatal("diagnostic precision lost")
	}
	maximum = 123
	if *diagnostic.Expected != "18446744073709551615" || optionalDecimal(nil) != nil {
		t.Fatal("diagnostic value aliases core storage")
	}
	if span := mapSpan(cursor.Span{ByteOffset: math.MaxUint64, BitOffset: 7, BitLength: math.MaxUint32}); span.ByteOffset != "18446744073709551615" || span.BitOffset != 7 || span.BitLength != math.MaxUint32 {
		t.Fatalf("span: %+v", span)
	}
	scan := newFakeScan(t, false)
	scan.snapshot.Progress.SourceSize = math.MaxUint64
	scan.snapshot.Progress.BytesScanned = offset
	scan.snapshot.Progress.TagCount = math.MaxUint64
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	view := app.Bindings().GetSession(id)
	if view.Session.Progress.SourceSize != "18446744073709551615" || view.Session.Progress.BytesScanned != "9007199254740993" || view.Session.Progress.TagCount != "18446744073709551615" {
		t.Fatalf("progress precision lost: %+v", view.Session.Progress)
	}
}

func TestDecimalStartGrammar(t *testing.T) {
	for _, value := range []string{"0", "1", "9007199254740993", "18446744073709551615"} {
		parsed, ok := parseStart(value)
		if !ok || decimal(parsed) != value {
			t.Fatalf("valid start rejected: %q", value)
		}
	}
	for _, value := range []string{"", "00", "01", "+1", "-1", "-0", " 1", "1 ", "1\n", "1.0", "1e3", "0x10", "１２", "١", "18446744073709551616", strings.Repeat("9", 1000), string([]byte{255})} {
		if _, ok := parseStart(value); ok {
			t.Fatalf("invalid start accepted: %q", value)
		}
	}
}

func TestResponseJSONContracts(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{SessionResponse{APIVersion: APIVersion, Session: &SessionSnapshot{SessionID: "scan-1", State: "opening"}}, `{"apiVersion":1,"session":{"sessionId":"scan-1","state":"opening","terminal":false,"closed":false,"progress":null,"scan":null,"failure":null},"error":null}`},
		{failedSession(CodeInvalidArgument), `{"apiVersion":1,"session":null,"error":{"code":"invalid_argument","message":"Invalid request."}}`},
		{TagPageResponse{APIVersion: APIVersion, Page: &TagPage{SessionID: "scan-1", Bounds: mapBounds(0, 1, 0, 0), Tags: []Tag{}}}, `{"apiVersion":1,"page":{"sessionId":"scan-1","bounds":{"start":"0","limit":1,"total":"0","next":"0"},"tags":[]},"error":null}`},
		{DiagnosticPageResponse{APIVersion: APIVersion, Page: &DiagnosticPage{SessionID: "scan-1", Bounds: mapBounds(0, 1, 0, 0), Diagnostics: []Diagnostic{}}}, `{"apiVersion":1,"page":{"sessionId":"scan-1","bounds":{"start":"0","limit":1,"total":"0","next":"0"},"diagnostics":[]},"error":null}`},
	} {
		encoded, err := json.Marshal(tc.value)
		if err != nil || string(encoded) != tc.want {
			t.Fatalf("contract mismatch: %s / %v", encoded, err)
		}
		roundTrip := reflect.New(reflect.TypeOf(tc.value))
		if err := json.Unmarshal(encoded, roundTrip.Interface()); err != nil || !reflect.DeepEqual(tc.value, roundTrip.Elem().Interface()) {
			t.Fatalf("response round trip: %v", err)
		}
	}
}

func TestDTOsExposeOnlyConcreteSafeFields(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Struct:
			for field := range typ.Fields() {
				if !field.IsExported() || field.Tag.Get("json") == "" {
					t.Errorf("unmapped DTO field: %s.%s", typ, field.Name)
				}
				visit(field.Type)
			}
		case reflect.Pointer, reflect.Slice:
			visit(typ.Elem())
		case reflect.String, reflect.Bool, reflect.Uint8, reflect.Uint32:
		default:
			t.Errorf("unsupported or potentially lossy DTO type: %s", typ)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[OpenRequest](), reflect.TypeFor[PageRequest](), reflect.TypeFor[SessionResponse](), reflect.TypeFor[TagPageResponse](), reflect.TypeFor[DiagnosticPageResponse](), reflect.TypeFor[FilteredTagRequest](), reflect.TypeFor[FilteredTagPageResponse]()} {
		visit(typ)
	}
}

func TestPageBoundsAndFaultMapping(t *testing.T) {
	scan := newFakeScan(t, false)
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	checkFault(t, b.GetTags(PageRequest{id, "18446744073709551615", 1}).Error, CodeInvalidArgument)
	checkFault(t, b.GetDiagnostics(PageRequest{id, "18446744073709551615", 1}).Error, CodeInvalidArgument)
	empty := b.GetDiagnostics(PageRequest{id, "1", 4096})
	if empty.Error != nil || empty.Page.Diagnostics == nil || len(empty.Page.Diagnostics) != 0 || empty.Page.Bounds.Next != "1" {
		t.Fatal("diagnostic EOF page is not empty")
	}
	checkFault(t, pageFailure(session.ErrClosed), CodeSessionClosed)
	checkFault(t, pageFailure(session.ErrNotReady), CodeNotReady)
}

func FuzzPageStart(f *testing.F) {
	for _, seed := range []string{"", "0", "1", "01", "-1", "1e3", "9007199254740993", "18446744073709551615", "18446744073709551616", "１２"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		parsed, valid := parseStart(value)
		// ParseUint alone permits a leading '+', so canonicality is checked
		// independently by formatting the parsed value back to decimal.
		reference, err := strconv.ParseUint(value, 10, 64)
		want := err == nil && strconv.FormatUint(reference, 10) == value
		if valid != want || valid && parsed != reference {
			t.Fatalf("decimal grammar mismatch for %q", value)
		}
	})
}

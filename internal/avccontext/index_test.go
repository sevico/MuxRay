package avccontext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func observeFixture(t *testing.T, data []byte, tags flv.TagIndex) *CandidateIndex {
	t.Helper()
	x := &CandidateIndex{}
	for _, tag := range tags.All() {
		if err := x.Observe(t.Context(), bytes.NewReader(data), tag); err != nil {
			t.Fatal(err)
		}
	}
	return x
}

func TestCandidateIndexStrictPredecessorsAndHardBound(t *testing.T) {
	packets := make([]packet, 130)
	for i := range packets {
		packets[i] = packet{18, []byte{5}}
	}
	for _, id := range []int{0, 63, 64, 65, 129} {
		packets[id] = config(4)
	}
	data, tags := fixture(t, packets...)
	x := observeFixture(t, data, tags)
	for _, tc := range []struct{ before, want uint64 }{{1, 0}, {63, 0}, {64, 63}, {65, 64}, {66, 65}, {129, 65}, {130, 129}} {
		id, ok, err := x.previous(t.Context(), tc.before)
		if err != nil || !ok || id != tc.want {
			t.Fatalf("before %d: %d %t %v", tc.before, id, ok, err)
		}
	}
	if _, ok, err := x.previous(t.Context(), 0); err != nil || ok {
		t.Fatalf("no predecessor: %t %v", ok, err)
	}
	if _, _, err := x.previous(t.Context(), 131); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unobserved suffix: %v", err)
	}
	if len(x.words)*8 != 125000 || cap(x.words) != len(x.words) {
		t.Fatalf("memory bound: len=%d cap=%d", len(x.words), cap(x.words))
	}
	// Exercise the final legal bit without allocating a million Tag records.
	x.covered = flv.HardMaxTags - 1
	tag, _ := tags.Get(0)
	tag.ID = x.covered
	if err := x.Observe(t.Context(), bytes.NewReader(data), tag); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := x.previous(t.Context(), flv.HardMaxTags); err != nil || !ok || id != flv.HardMaxTags-1 {
		t.Fatalf("final bit: %d %t %v", id, ok, err)
	}
	tag.ID++
	if err := x.Observe(t.Context(), bytes.NewReader(data), tag); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("limit: %v", err)
	}
}

func TestCandidateIndexProbeEligibilityAndNoWidthCache(t *testing.T) {
	data, tags := fixture(t,
		packet{18, []byte{0x17, 0}}, packet{0x29, []byte{0x17, 0}},
		packet{9, []byte{0x57, 0}}, packet{9, []byte{0x12, 0}},
		packet{9, []byte{0x17}}, nalu(), config(2), nalu())
	x := observeFixture(t, data, tags)
	if id, ok, err := x.previous(t.Context(), 8); err != nil || !ok || id != 6 {
		t.Fatalf("eligible candidate: %d %t %v", id, ok, err)
	}
	if _, ok, err := x.previous(t.Context(), 6); err != nil || ok {
		t.Fatalf("ineligible prefix: %t %v", ok, err)
	}
	// The index does not retain a width; candidate bytes are re-read each time.
	// A production document rejects source mutation before this low-level call.
	configTag, _ := tags.Get(6)
	data[configTag.PayloadSpan().ByteOffset+9] = 0xfc
	r, err := ResolveIndexed(t.Context(), bytes.NewReader(data), selected(tags), lookup(tags), x, flv.AVCVideoOptions{}, Limits{})
	if err != nil || r.State != StateFound || r.NALLengthSize != 1 || r.SourceTagID != 6 || r.ScannedTags != 1 || r.LookupMethod != "candidate_index" {
		t.Fatalf("re-read: %+v %v", r, err)
	}
}

func TestCandidateIndexFailedProbeDoesNotCoverOrInventAbsence(t *testing.T) {
	data, tags := fixture(t, config(4), nalu())
	tag, _ := tags.Get(0)
	for _, readErr := range []error{io.ErrUnexpectedEOF, errors.New("disk error")} {
		x := &CandidateIndex{}
		err := x.Observe(t.Context(), readerFunc(func([]byte, int64) (int, error) { return 0, readErr }), tag)
		if !errors.Is(err, readErr) || x.covered != 0 || x.words != nil {
			t.Fatalf("failed probe: %+v %v", x, err)
		}
		if _, _, err := x.previous(t.Context(), 1); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unknown prefix accepted: %v", err)
		}
	}
	x := &CandidateIndex{}
	ctx, cancel := context.WithCancel(t.Context())
	r := readerFunc(func(p []byte, off int64) (int, error) {
		n, err := bytes.NewReader(data).ReadAt(p, off)
		cancel()
		return n, err
	})
	if err := x.Observe(ctx, r, tag); !errors.Is(err, context.Canceled) || x.covered != 0 || x.words != nil {
		t.Fatalf("canceled probe published: %+v %v", x, err)
	}
	if _, _, err := x.previous(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled memory search: %v", err)
	}
	if err := x.Observe(t.Context(), bytes.NewReader(data), selected(tags)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("gap admitted: %v", err)
	}
}

func TestIndexedResolutionMatchesLegacyAndRetainsBudgets(t *testing.T) {
	bad := packet{9, []byte{0x17, 0, 0, 0, 0, 1}}
	data, tags := fixture(t, config(1), packet{18, []byte{5}}, bad, nalu())
	x := observeFixture(t, data, tags)
	legacy, err := Resolve(t.Context(), bytes.NewReader(data), selected(tags), lookup(tags), flv.AVCVideoOptions{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	indexed, err := ResolveIndexed(t.Context(), bytes.NewReader(data), selected(tags), lookup(tags), x, flv.AVCVideoOptions{}, Limits{})
	if err != nil || indexed.State != StateFound || indexed.SourceTagID != legacy.SourceTagID || indexed.NALLengthSize != legacy.NALLengthSize || indexed.SourceLengthSpan != legacy.SourceLengthSpan || indexed.Candidates != 2 || indexed.ScannedTags != 2 {
		t.Fatalf("equivalence: %+v %+v %v", legacy, indexed, err)
	}
	for _, limits := range []Limits{{MaxCandidates: 1}, {MaxBytes: indexed.BytesRead - 1}} {
		r, err := ResolveIndexed(t.Context(), bytes.NewReader(data), selected(tags), lookup(tags), x, flv.AVCVideoOptions{}, limits)
		if err != nil || r.State != StateBudgetExhausted || r.NALLengthSize != 0 {
			t.Fatalf("budget: %+v %v", r, err)
		}
	}
	if _, err := ResolveIndexed(t.Context(), bytes.NewReader(data), selected(tags), lookup(tags), &CandidateIndex{}, flv.AVCVideoOptions{}, Limits{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("uncovered resolution: %v", err)
	}
}

func TestFramingEventsRetainBarriersWithoutChangingLegacyCandidates(t *testing.T) {
	data, tags := fixture(t, config(4), nalu(), packet{9, []byte{0x17, 2, 0, 0, 0}}, nalu(), packet{9, []byte{0x12, 1}}, nalu(), packet{0x29, []byte{0x17, 0}}, nalu(), packet{9, []byte{0x17}}, nalu())
	x := observeFixture(t, data, tags)
	for _, tc := range []struct{ before, want uint64 }{{2, 0}, {4, 2}, {6, 4}, {8, 6}, {10, 8}} {
		id, ok, err := x.PreviousFramingEvent(t.Context(), tc.before)
		if err != nil || !ok || id != tc.want {
			t.Fatalf("event before%d: %d %t %v", tc.before, id, ok, err)
		}
		id, ok, err = x.previous(t.Context(), tc.before)
		if err != nil || !ok || id != 0 {
			t.Fatalf("legacy changed: %d %t %v", id, ok, err)
		}
	}
	if len(x.events)*8 != 125000 {
		t.Fatalf("event memory %d", len(x.events)*8)
	}
}

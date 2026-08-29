package flv

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/cursor"
)

const (
	CodeScriptNameMissing   = "AMF0_SCRIPT_NAME_MISSING"
	CodeScriptNameNotString = "AMF0_SCRIPT_NAME_NOT_STRING"
	CodeScriptValueMissing  = "AMF0_SCRIPT_VALUE_MISSING"
)

var (
	ErrScriptDataNilSource         = errors.New("flv: script data source is nil")
	ErrScriptDataWrongTagType      = errors.New("flv: tag is not script data")
	ErrScriptDataFiltered          = errors.New("flv: filtered script data is unsupported")
	ErrScriptDataPayloadIncomplete = errors.New("flv: script data payload is incomplete")
)

// ScriptData is the on-demand AMF0 detail for one complete, unfiltered FLV
// ScriptData tag. All spans and diagnostic offsets remain absolute positions
// in the original source.
type ScriptData struct {
	TagID uint64
	Span  cursor.Span
	AMF   amf0.Result

	EventNameNodeID amf0.NodeID
	ValueNodeID     amf0.NodeID
	EventName       string

	Diagnostics []amf0.Diagnostic
	Complete    bool
}

// ParseScriptData parses one indexed ScriptData payload without changing the
// container scan result. Malformed AMF0 is represented in the returned detail;
// invalid adapter inputs and source read failures are returned as Go errors.
func ParseScriptData(source io.ReaderAt, tag TagRecord, options amf0.Options) (ScriptData, error) {
	detail := ScriptData{
		TagID:           tag.ID,
		Span:            tag.PayloadSpan(),
		EventNameNodeID: amf0.NoNode,
		ValueNodeID:     amf0.NoNode,
	}

	if source == nil {
		return detail, ErrScriptDataNilSource
	}
	if tag.Type() != TagTypeScript {
		return detail, fmt.Errorf("%w: got %s", ErrScriptDataWrongTagType, tag.Type())
	}
	if tag.Filter() {
		return detail, ErrScriptDataFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return detail, ErrScriptDataPayloadIncomplete
	}
	normalized, err := options.Normalized()
	if err != nil {
		return detail, err
	}

	result, err := amf0.ParseSequence(source, detail.Span, normalized)
	detail.AMF = result
	if err != nil {
		return detail, err
	}

	detail.Diagnostics = append([]amf0.Diagnostic(nil), result.Diagnostics...)
	payloadEnd := detail.Span.ByteOffset + uint64(tag.DataSize)
	semanticError := false

	if len(result.Roots) == 0 {
		semanticError = true
		detail.Diagnostics = appendScriptDiagnostic(detail.Diagnostics, missingScriptDiagnostic(
			CodeScriptNameMissing,
			"script data event name is missing",
			payloadEnd,
		), normalized.MaxDiagnostics)
	} else {
		detail.EventNameNodeID = result.Roots[0]
		if node, ok := amfNode(result, detail.EventNameNodeID); ok {
			if node.Type != amf0.TypeString {
				detail.Diagnostics = appendScriptDiagnostic(detail.Diagnostics, amf0.Diagnostic{
					Code:      CodeScriptNameNotString,
					Severity:  amf0.SeverityWarning,
					Offset:    node.MarkerSpan.ByteOffset,
					BitOffset: node.MarkerSpan.BitOffset,
					Message:   "script data event name should be an AMF0 string",
				}, normalized.MaxDiagnostics)
			} else if node.Complete && !node.StringOmitted {
				detail.EventName = node.String
			}
		}
	}

	if len(result.Roots) < 2 {
		semanticError = true
		detail.Diagnostics = appendScriptDiagnostic(detail.Diagnostics, missingScriptDiagnostic(
			CodeScriptValueMissing,
			"script data event value is missing",
			payloadEnd,
		), normalized.MaxDiagnostics)
	} else {
		detail.ValueNodeID = result.Roots[1]
	}

	detail.Complete = !result.HasErrors() && !semanticError
	return detail, nil
}

func amfNode(result amf0.Result, id amf0.NodeID) (amf0.Node, bool) {
	index := uint64(id)
	if index >= uint64(len(result.Nodes)) {
		return amf0.Node{}, false
	}
	return result.Nodes[index], true
}

func missingScriptDiagnostic(code, message string, payloadEnd uint64) amf0.Diagnostic {
	return amf0.Diagnostic{
		Code:           code,
		Severity:       amf0.SeverityError,
		Offset:         payloadEnd,
		Message:        message,
		RecoveryOffset: scriptOffset(payloadEnd),
	}
}

func scriptOffset(value uint64) *uint64 { return &value }

func appendScriptDiagnostic(items []amf0.Diagnostic, diagnostic amf0.Diagnostic, limit uint32) []amf0.Diagnostic {
	if uint32(len(items)) < limit {
		return append(items, diagnostic)
	}
	if limit == 0 || len(items) == 0 {
		return items
	}
	markerIndex := len(items) - 1
	markerPresent := items[markerIndex].Code == amf0.CodeDiagnosticLimitReached
	marker := items[markerIndex]
	if !markerPresent {
		marker = amf0.Diagnostic{
			Code:      amf0.CodeDiagnosticLimitReached,
			Severity:  amf0.SeverityError,
			Offset:    diagnostic.Offset,
			BitOffset: diagnostic.BitOffset,
			Message:   "diagnostic limit reached; additional diagnostics were omitted",
		}
	}

	searchEnd := len(items)
	if markerPresent {
		searchEnd = markerIndex
	}
	fatal := amf0.Diagnostic{}
	hasFatal := false
	for _, existing := range items[:searchEnd] {
		if existing.RecoveryOffset != nil {
			fatal = existing
			hasFatal = true
			break
		}
	}
	if !hasFatal && diagnostic.RecoveryOffset != nil {
		fatal = diagnostic
		hasFatal = true
	}
	if hasFatal {
		retainedBeforeMarker := false
		for _, existing := range items[:markerIndex] {
			if existing.RecoveryOffset != nil {
				retainedBeforeMarker = true
				break
			}
		}
		if !retainedBeforeMarker && markerIndex > 0 {
			items[markerIndex-1] = fatal
		}
		marker.Offset = fatal.Offset
		marker.BitOffset = fatal.BitOffset
		marker.RecoveryOffset = fatal.RecoveryOffset
	}
	items[markerIndex] = marker
	return items
}

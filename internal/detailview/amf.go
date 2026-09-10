package detailview

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
)

func floatValue(bits uint64) string {
	return fmt.Sprintf("%s (IEEE-754 0x%016x)", strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64), bits)
}

func (b *builder) amf(ctx context.Context, parsed flv.ScriptData) error {
	parents := make(map[amf0.NodeID]string, len(parsed.AMF.Nodes))
	for _, n := range parsed.AMF.Nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := "value [" + decimal(uint64(n.ID)) + "]"
		if n.ID == parsed.EventNameNodeID {
			name = "eventName"
		} else if n.ID == parsed.ValueNodeID {
			name = "eventValue"
		}
		var property *amf0.Property
		if n.PropertyID != amf0.NoProperty && int(n.PropertyID) < len(parsed.AMF.Properties) {
			property = &parsed.AMF.Properties[n.PropertyID]
			name = property.Name
			if property.NameOmitted {
				name = "[property name not retained; see diagnostics]"
				b.detail.Truncated = true
			}
		}
		value := "AMF0 " + n.Type.String()
		if !n.Complete {
			value += " · incomplete"
		}
		id := b.add(parents[n.ParentID], name, n.Type.String(), value, span(n.Span))
		if id == "" {
			break
		}
		parents[n.ID] = id
		if property != nil {
			b.encoded(id, "propertyNameLength", "uint16", decimal(uint64(property.NameLength)), property.NameLengthSpan)
			text := property.Name
			if property.NameOmitted {
				text = "[not retained; see diagnostics]"
			}
			b.add(id, "propertyName", "utf8", text, span(property.NameSpan))
		}
		b.encoded(id, "typeMarker", "uint8", fmt.Sprintf("0x%02x (%s)", uint8(n.Type), n.Type), n.MarkerSpan)
		switch n.Type {
		case amf0.TypeNumber:
			if n.NumberSpan.BitLength == 64 {
				b.encoded(id, "value", "float64", floatValue(n.NumberBits), n.NumberSpan)
			}
		case amf0.TypeBoolean:
			if n.BooleanSpan.BitLength == 8 {
				b.encoded(id, "value", "boolean", fmt.Sprintf("%t (raw %d)", n.BooleanValue, n.BooleanRaw), n.BooleanSpan)
			}
		case amf0.TypeString, amf0.TypeLongString, amf0.TypeXMLDocument:
			if n.StringLengthSpan.BitLength == 16 || n.StringLengthSpan.BitLength == 32 {
				b.encoded(id, "byteLength", "uint32", decimal(uint64(n.StringLength)), n.StringLengthSpan)
				value := n.String
				if n.StringOmitted {
					value = "[not retained; see diagnostics]"
					b.detail.Truncated = true
				}
				if uint64(n.StringSpan.BitLength) == uint64(n.StringLength)*8 {
					b.add(id, "value", "utf8", value, span(n.StringSpan))
				}
			}
		case amf0.TypeReference:
			if n.ReferenceSpan.BitLength == 16 {
				b.encoded(id, "referenceIndex", "uint16", fmt.Sprintf("%d (valid=%t, target node=%d)", n.Reference, n.ReferenceValid, n.ReferencedNodeID), n.ReferenceSpan)
			}
		case amf0.TypeDate:
			if n.DateMillisecondsSpan.BitLength == 64 {
				b.encoded(id, "milliseconds", "float64", floatValue(n.DateTimeBits), n.DateMillisecondsSpan)
			}
			if n.DateTimezoneSpan.BitLength == 16 {
				b.encoded(id, "timezoneMinutes", "int16", strconv.FormatInt(int64(n.TimeZoneMinutes), 10), n.DateTimezoneSpan)
			}
		case amf0.TypeTypedObject:
			if n.ClassNameLengthSpan.BitLength == 16 {
				b.encoded(id, "classNameLength", "uint16", decimal(uint64(n.ClassNameLength)), n.ClassNameLengthSpan)
				value := n.ClassName
				if n.ClassNameOmitted {
					value = "[not retained; see diagnostics]"
					b.detail.Truncated = true
				}
				if n.ClassNameSpan.BitLength == uint32(n.ClassNameLength)*8 {
					b.add(id, "className", "utf8", value, span(n.ClassNameSpan))
				}
			}
		}
		if n.DeclaredCountPresent {
			b.encoded(id, "declaredCount", "uint32", decimal(uint64(n.DeclaredCount)), n.CountSpan)
		}
		if n.Type == amf0.TypeObject || n.Type == amf0.TypeECMAArray || n.Type == amf0.TypeStrictArray || n.Type == amf0.TypeTypedObject {
			b.add(id, "retainedChildren", "uint32", decimal(uint64(n.ChildCount)), nil)
		}
		if n.HasTerminator {
			b.encoded(id, "objectEnd", "marker", "00 00 09", n.TerminatorSpan)
		}
	}
	return nil
}

package amf0

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/cursor"
)

// ParseSequence parses every top-level AMF0 value in payload. All returned
// spans use absolute source offsets. Malformed input is represented by a
// partial Result and diagnostics; invalid arguments and ReaderAt failures are
// returned as Go errors.
func ParseSequence(source io.ReaderAt, payload cursor.Span, options Options) (Result, error) {
	result := Result{Span: payload}
	if source == nil {
		return result, errors.New("amf0: nil source")
	}
	if err := payload.Validate(); err != nil {
		return result, fmt.Errorf("amf0: invalid payload span: %w", err)
	}
	if !payload.IsByteAligned() {
		return result, fmt.Errorf("amf0: payload span must be byte-aligned")
	}
	start, end, err := payload.ByteRange()
	if err != nil {
		return result, fmt.Errorf("amf0: payload byte range: %w", err)
	}
	payloadBytes := end - start
	if payloadBytes > HardMaxPayloadBytes {
		return result, fmt.Errorf("amf0: payload length %d exceeds hard limit %d", payloadBytes, HardMaxPayloadBytes)
	}
	options, err = options.Normalized()
	if err != nil {
		return result, err
	}
	arenaCapacity := initialArenaCapacity(payloadBytes, options.MaxNodes)
	rootCapacity := arenaCapacity
	if rootCapacity > 64 {
		rootCapacity = 64
	}
	result.Roots = make([]NodeID, 0, rootCapacity)
	result.Nodes = make([]Node, 0, arenaCapacity)
	result.Properties = make([]Property, 0, arenaCapacity)

	reader := cursor.NewByteReader(source, end)
	if err := reader.Seek(start); err != nil {
		return result, fmt.Errorf("amf0: seek to payload: %w", err)
	}
	p := parser{
		reader:      reader,
		start:       start,
		end:         end,
		options:     options,
		result:      result,
		diagnostics: newDiagnosticCollector(options.MaxDiagnostics),
		lastChild:   make([]NodeID, 0, arenaCapacity),
	}
	p.parseSequence()

	// A fatal AMF0 condition cannot be resynchronized safely inside an
	// arbitrary value. BytesConsumed remains the actual parse cursor;
	// diagnostics identify the trusted outer payload boundary at which the FLV
	// caller can recover without asking the AMF parser to inspect skipped bytes.
	p.result.BytesConsumed = p.reader.Offset() - p.start
	p.result.Complete = p.err == nil && !p.fatal && p.reader.Offset() == p.end
	p.result.Diagnostics = p.diagnostics.items
	if p.err != nil {
		return p.result, p.err
	}
	return p.result, nil
}

func initialArenaCapacity(payloadBytes uint64, maxNodes uint32) int {
	if payloadBytes == 0 || maxNodes == 0 {
		return 0
	}
	// Metadata properties commonly consume at least a two-byte name length,
	// several name bytes, a marker, and a scalar payload. This estimate avoids
	// repeated relocation for normal ScriptData without eagerly reserving the
	// worst-case one-node-per-byte arena for marker-only sequences.
	estimate := payloadBytes/16 + 1
	const maximumEagerNodes = 4_096
	if estimate > maximumEagerNodes {
		estimate = maximumEagerNodes
	}
	if estimate > uint64(maxNodes) {
		estimate = uint64(maxNodes)
	}
	return int(estimate)
}

type parser struct {
	reader      *cursor.ByteReader
	start       uint64
	end         uint64
	options     Options
	result      Result
	diagnostics *diagnosticCollector
	lastChild   []NodeID
	references  []NodeID
	scratch     [8]byte
	fatal       bool
	err         error
}

type storedString struct {
	value   string
	omitted bool
	span    cursor.Span
}

func (p *parser) parseSequence() {
	for p.reader.Remaining() > 0 && !p.stopped() {
		p.parseValue(NoNode, NoProperty, 1)
	}
}

func (p *parser) parseValue(parent NodeID, propertyID uint32, depth uint32) NodeID {
	if p.stopped() {
		return NoNode
	}
	markerOffset := p.reader.Offset()
	if !p.ensure(1, CodeMarkerTruncated, "AMF0 type marker is truncated") {
		return NoNode
	}
	marker, ok := p.readU8("type marker")
	if !ok {
		return NoNode
	}
	return p.parseMarkedValue(parent, propertyID, depth, markerOffset, Type(marker))
}

func (p *parser) parseMarkedValue(parent NodeID, propertyID uint32, depth uint32, markerOffset uint64, marker Type) NodeID {
	if p.stopped() {
		return NoNode
	}
	if depth > p.options.MaxDepth {
		p.fail(Diagnostic{
			Code: CodeDepthLimitExceeded, Severity: SeverityError, Offset: markerOffset,
			Message:  "AMF0 nesting depth exceeds the configured limit",
			Expected: number(uint64(p.options.MaxDepth)), Actual: number(uint64(depth)),
		})
		return NoNode
	}
	if uint32(len(p.result.Nodes)) >= p.options.MaxNodes {
		p.fail(Diagnostic{
			Code: CodeNodeLimitExceeded, Severity: SeverityError, Offset: markerOffset,
			Message:  "AMF0 node count exceeds the configured limit",
			Expected: number(uint64(p.options.MaxNodes)), Actual: number(uint64(len(p.result.Nodes)) + 1),
		})
		return NoNode
	}

	id := NodeID(len(p.result.Nodes))
	node := Node{
		ID: id, ParentID: parent, FirstChild: NoNode, NextSibling: NoNode,
		PropertyID: propertyID, Type: marker,
		MarkerSpan:       cursor.Span{ByteOffset: markerOffset, BitLength: 8},
		ReferencedNodeID: NoNode,
	}
	p.appendNode(node)
	p.linkNode(id, parent)

	if referenceable(marker) {
		p.references = append(p.references, id)
	}

	switch marker {
	case TypeNumber:
		p.parseNumber(id)
	case TypeBoolean:
		p.parseBoolean(id)
	case TypeString:
		p.parseNodeString(id, 2)
	case TypeObject:
		p.parseProperties(id, depth, nil)
	case TypeNull, TypeUndefined, TypeUnsupported:
		// Marker-only values.
	case TypeReference:
		p.parseReference(id)
	case TypeECMAArray:
		p.parseECMAArray(id, depth)
	case TypeObjectEnd:
		p.fail(Diagnostic{
			Code: CodeObjectEndUnexpected, Severity: SeverityError, Offset: markerOffset,
			Message: "AMF0 object-end marker appears outside an object terminator",
			Actual:  number(uint64(marker)),
		})
	case TypeStrictArray:
		p.parseStrictArray(id, depth)
	case TypeDate:
		p.parseDate(id)
	case TypeLongString:
		p.parseNodeString(id, 4)
	case TypeXMLDocument:
		p.parseNodeString(id, 4)
	case TypeTypedObject:
		p.parseTypedObject(id, depth)
	case TypeMovieClip, TypeRecordSet, TypeAVMPlus:
		p.fail(Diagnostic{
			Code: CodeTypeUnsupported, Severity: SeverityError, Offset: markerOffset,
			Message: "AMF0 marker has no supported bounded payload grammar",
			Actual:  number(uint64(marker)),
		})
	default:
		p.fail(Diagnostic{
			Code: CodeMarkerUnknown, Severity: SeverityError, Offset: markerOffset,
			Message: "unknown AMF0 type marker",
			Actual:  number(uint64(marker)),
		})
	}

	p.result.Nodes[id].Span = byteSpan(markerOffset, p.reader.Offset())
	p.result.Nodes[id].Complete = !p.stopped()
	return id
}

func (p *parser) parseNumber(id NodeID) {
	start := p.reader.Offset()
	if !p.ensure(8, CodeNumberTruncated, "AMF0 number payload is truncated") {
		p.result.Nodes[id].NumberSpan = byteSpan(start, p.reader.Offset())
		return
	}
	bits, ok := p.readU64("number")
	if !ok {
		return
	}
	p.result.Nodes[id].NumberBits = bits
	p.result.Nodes[id].NumberSpan = byteSpan(start, p.reader.Offset())
}

func (p *parser) parseBoolean(id NodeID) {
	start := p.reader.Offset()
	if !p.ensure(1, CodeBooleanTruncated, "AMF0 boolean payload is truncated") {
		p.result.Nodes[id].BooleanSpan = byteSpan(start, p.reader.Offset())
		return
	}
	raw, ok := p.readU8("boolean")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.BooleanRaw = raw
	node.BooleanValue = raw != 0
	node.BooleanSpan = byteSpan(start, p.reader.Offset())
	if raw > 1 {
		p.warn(Diagnostic{
			Code: CodeBooleanNonCanonical, Severity: SeverityWarning, Offset: start,
			Message:  "AMF0 boolean uses a non-canonical byte value",
			Expected: number(1), Actual: number(uint64(raw)),
		})
	}
}

func (p *parser) parseNodeString(id NodeID, lengthBytes uint64) {
	lengthStart := p.reader.Offset()
	if !p.ensure(lengthBytes, CodeStringLengthTruncated, "AMF0 string length is truncated") {
		p.result.Nodes[id].StringLengthSpan = byteSpan(lengthStart, p.reader.Offset())
		return
	}
	var length uint32
	var ok bool
	if lengthBytes == 2 {
		var short uint16
		short, ok = p.readU16("string length")
		length = uint32(short)
	} else {
		length, ok = p.readU32("string length")
	}
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.StringLength = length
	node.StringLengthSpan = byteSpan(lengthStart, p.reader.Offset())
	stored := p.readStoredString(length, CodeStringDataTruncated, "AMF0 string data is truncated", "string")
	node = &p.result.Nodes[id]
	node.String = stored.value
	node.StringOmitted = stored.omitted
	node.StringSpan = stored.span
}

func (p *parser) parseReference(id NodeID) {
	start := p.reader.Offset()
	if !p.ensure(2, CodeReferenceTruncated, "AMF0 reference index is truncated") {
		p.result.Nodes[id].ReferenceSpan = byteSpan(start, p.reader.Offset())
		return
	}
	index, ok := p.readU16("reference index")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.Reference = index
	node.ReferenceSpan = byteSpan(start, p.reader.Offset())
	if int(index) < len(p.references) {
		node.ReferenceValid = true
		node.ReferencedNodeID = p.references[index]
		return
	}
	p.warn(Diagnostic{
		Code: CodeReferenceOutOfRange, Severity: SeverityWarning, Offset: start,
		Message:  "AMF0 reference index does not identify a preceding referenceable value",
		Expected: number(uint64(len(p.references))), Actual: number(uint64(index)),
	})
}

func (p *parser) parseECMAArray(id NodeID, depth uint32) {
	start := p.reader.Offset()
	if !p.ensure(4, CodeArrayCountTruncated, "AMF0 ECMA Array count is truncated") {
		p.result.Nodes[id].CountSpan = byteSpan(start, p.reader.Offset())
		return
	}
	count, ok := p.readU32("ECMA Array count")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.DeclaredCountPresent = true
	node.DeclaredCount = count
	node.CountSpan = byteSpan(start, p.reader.Offset())
	p.parseProperties(id, depth, &count)
}

func (p *parser) parseStrictArray(id NodeID, depth uint32) {
	start := p.reader.Offset()
	if !p.ensure(4, CodeArrayCountTruncated, "AMF0 Strict Array count is truncated") {
		p.result.Nodes[id].CountSpan = byteSpan(start, p.reader.Offset())
		return
	}
	count, ok := p.readU32("Strict Array count")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.DeclaredCountPresent = true
	node.DeclaredCount = count
	node.CountSpan = byteSpan(start, p.reader.Offset())
	if count > p.options.MaxContainerEntries {
		p.fail(Diagnostic{
			Code: CodeContainerEntryLimitExceeded, Severity: SeverityError, Offset: start,
			Message:  "AMF0 container entry count exceeds the configured limit",
			Expected: number(uint64(p.options.MaxContainerEntries)), Actual: number(uint64(count)),
		})
		return
	}
	for index := uint32(0); index < count && !p.stopped(); index++ {
		child := p.parseValue(id, NoProperty, depth+1)
		if child != NoNode {
			p.result.Nodes[id].ActualCount++
		}
	}
}

func (p *parser) parseDate(id NodeID) {
	millisecondsStart := p.reader.Offset()
	if !p.ensure(8, CodeDateMillisecondsTruncated, "AMF0 date milliseconds are truncated") {
		p.result.Nodes[id].DateMillisecondsSpan = byteSpan(millisecondsStart, p.reader.Offset())
		return
	}
	bits, ok := p.readU64("date milliseconds")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.DateTimeBits = bits
	node.DateMillisecondsSpan = byteSpan(millisecondsStart, p.reader.Offset())

	timezoneStart := p.reader.Offset()
	if !p.ensure(2, CodeDateTimezoneTruncated, "AMF0 date timezone is truncated") {
		p.result.Nodes[id].DateTimezoneSpan = byteSpan(timezoneStart, p.reader.Offset())
		return
	}
	timezone, ok := p.readU16("date timezone")
	if !ok {
		return
	}
	node = &p.result.Nodes[id]
	node.TimeZoneMinutes = int16(timezone)
	node.DateTimezoneSpan = byteSpan(timezoneStart, p.reader.Offset())
	if node.TimeZoneMinutes != 0 {
		p.warn(Diagnostic{
			Code: CodeDateTimezoneNonZero, Severity: SeverityWarning, Offset: timezoneStart,
			Message:  "AMF0 date timezone should be zero",
			Expected: number(0), Actual: number(uint64(timezone)),
		})
	}
}

func (p *parser) parseTypedObject(id NodeID, depth uint32) {
	lengthStart := p.reader.Offset()
	if !p.ensure(2, CodeTypedClassLengthTruncated, "AMF0 typed-object class-name length is truncated") {
		p.result.Nodes[id].ClassNameLengthSpan = byteSpan(lengthStart, p.reader.Offset())
		return
	}
	length, ok := p.readU16("typed-object class-name length")
	if !ok {
		return
	}
	node := &p.result.Nodes[id]
	node.ClassNameLength = length
	node.ClassNameLengthSpan = byteSpan(lengthStart, p.reader.Offset())
	stored := p.readStoredString(uint32(length), CodeTypedClassDataTruncated, "AMF0 typed-object class name is truncated", "class name")
	node = &p.result.Nodes[id]
	node.ClassName = stored.value
	node.ClassNameOmitted = stored.omitted
	node.ClassNameSpan = stored.span
	if !p.stopped() {
		p.parseProperties(id, depth, nil)
	}
}

func (p *parser) parseProperties(id NodeID, depth uint32, declaredCount *uint32) {
	actual := uint32(0)
	for !p.stopped() {
		if p.reader.Remaining() == 0 {
			p.fail(Diagnostic{
				Code: CodeObjectEndMissing, Severity: SeverityError, Offset: p.reader.Offset(),
				Message:  "AMF0 object property list is missing its object-end marker",
				Expected: number(3), Actual: number(0),
			})
			return
		}
		if actual >= p.options.MaxContainerEntries {
			p.parseTerminatorAtEntryLimit(id, actual, declaredCount)
			return
		}

		propertyStart := p.reader.Offset()
		lengthStart := p.reader.Offset()
		if !p.ensure(2, CodeObjectKeyLengthTruncated, "AMF0 object property-name length is truncated") {
			return
		}
		nameLength, ok := p.readU16("object property-name length")
		if !ok {
			return
		}
		lengthSpan := byteSpan(lengthStart, p.reader.Offset())
		stored := p.readStoredString(uint32(nameLength), CodeObjectKeyDataTruncated, "AMF0 object property name is truncated", "property name")
		if p.stopped() {
			p.appendPartialProperty(id, propertyStart, lengthSpan, nameLength, stored)
			return
		}

		markerOffset := p.reader.Offset()
		if !p.ensure(1, CodeMarkerTruncated, "AMF0 object property value marker is truncated") {
			p.appendPartialProperty(id, propertyStart, lengthSpan, nameLength, stored)
			return
		}
		marker, ok := p.readU8("object property value marker")
		if !ok {
			p.appendPartialProperty(id, propertyStart, lengthSpan, nameLength, stored)
			return
		}
		if nameLength == 0 && Type(marker) == TypeObjectEnd {
			node := &p.result.Nodes[id]
			node.HasTerminator = true
			node.TerminatorSpan = byteSpan(propertyStart, p.reader.Offset())
			node.ActualCount = actual
			p.checkDeclaredCount(id, declaredCount, actual)
			return
		}

		propertyID := uint32(len(p.result.Properties))
		property := Property{
			ID: propertyID, ParentID: id, ValueID: NoNode,
			Span:           byteSpan(propertyStart, p.reader.Offset()),
			NameLengthSpan: lengthSpan, NameSpan: stored.span,
			NameLength: nameLength, Name: stored.value, NameOmitted: stored.omitted,
		}
		p.appendProperty(property)
		actual++
		p.result.Nodes[id].ActualCount = actual
		valueID := p.parseMarkedValue(id, propertyID, depth+1, markerOffset, Type(marker))
		property = p.result.Properties[propertyID]
		property.ValueID = valueID
		property.Span = byteSpan(propertyStart, p.reader.Offset())
		p.result.Properties[propertyID] = property
	}
}

func (p *parser) parseTerminatorAtEntryLimit(id NodeID, actual uint32, declaredCount *uint32) {
	propertyStart := p.reader.Offset()
	if !p.ensure(2, CodeObjectKeyLengthTruncated, "AMF0 object property-name length is truncated") {
		return
	}
	nameLength, ok := p.readU16("object property-name length")
	if !ok {
		return
	}
	if nameLength == 0 {
		if !p.ensure(1, CodeMarkerTruncated, "AMF0 object property value marker is truncated") {
			return
		}
		marker, ok := p.readU8("object property value marker")
		if !ok {
			return
		}
		if Type(marker) == TypeObjectEnd {
			node := &p.result.Nodes[id]
			node.HasTerminator = true
			node.TerminatorSpan = byteSpan(propertyStart, p.reader.Offset())
			node.ActualCount = actual
			p.checkDeclaredCount(id, declaredCount, actual)
			return
		}
	}
	p.fail(Diagnostic{
		Code: CodeContainerEntryLimitExceeded, Severity: SeverityError, Offset: propertyStart,
		Message:  "AMF0 container entry count exceeds the configured limit",
		Expected: number(uint64(p.options.MaxContainerEntries)), Actual: number(uint64(actual) + 1),
	})
}

func (p *parser) appendPartialProperty(id NodeID, start uint64, lengthSpan cursor.Span, length uint16, stored storedString) {
	propertyID := uint32(len(p.result.Properties))
	p.appendProperty(Property{
		ID: propertyID, ParentID: id, ValueID: NoNode,
		Span: byteSpan(start, p.reader.Offset()), NameLengthSpan: lengthSpan,
		NameSpan: stored.span, NameLength: length, Name: stored.value, NameOmitted: stored.omitted,
	})
}

func (p *parser) appendNode(node Node) {
	if len(p.result.Nodes) == cap(p.result.Nodes) {
		next := nextArenaCapacity(cap(p.result.Nodes), int(p.options.MaxNodes))
		grown := make([]Node, len(p.result.Nodes), next)
		copy(grown, p.result.Nodes)
		p.result.Nodes = grown
	}
	if len(p.lastChild) == cap(p.lastChild) {
		next := nextArenaCapacity(cap(p.lastChild), int(p.options.MaxNodes))
		grown := make([]NodeID, len(p.lastChild), next)
		copy(grown, p.lastChild)
		p.lastChild = grown
	}
	p.result.Nodes = append(p.result.Nodes, node)
	p.lastChild = append(p.lastChild, NoNode)
}

func (p *parser) appendProperty(property Property) {
	if len(p.result.Properties) == cap(p.result.Properties) {
		next := nextArenaCapacity(cap(p.result.Properties), int(p.options.MaxNodes))
		grown := make([]Property, len(p.result.Properties), next)
		copy(grown, p.result.Properties)
		p.result.Properties = grown
	}
	p.result.Properties = append(p.result.Properties, property)
}

func nextArenaCapacity(current, hardLimit int) int {
	if current >= hardLimit {
		return hardLimit
	}
	next := current * 2
	if next < 1 {
		next = 1
	}
	if next > hardLimit {
		next = hardLimit
	}
	return next
}

func (p *parser) checkDeclaredCount(id NodeID, declared *uint32, actual uint32) {
	if declared == nil || *declared == actual {
		return
	}
	p.warn(Diagnostic{
		Code: CodeECMAArrayCountMismatch, Severity: SeverityWarning,
		Offset:   p.result.Nodes[id].CountSpan.ByteOffset,
		Message:  "AMF0 ECMA Array declared count does not match its property count",
		Expected: number(uint64(*declared)), Actual: number(uint64(actual)),
	})
}

func (p *parser) readStoredString(length uint32, truncatedCode, truncatedMessage, role string) storedString {
	start := p.reader.Offset()
	available := p.reader.Remaining()
	if available < uint64(length) {
		if err := p.reader.Skip(available); err != nil {
			p.err = fmt.Errorf("amf0: consume truncated %s at byte %d: %w", role, start, err)
			return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
		}
		p.fail(Diagnostic{
			Code: truncatedCode, Severity: SeverityError, Offset: p.reader.Offset(),
			Message:  truncatedMessage,
			Expected: number(uint64(length)), Actual: number(available),
		})
		return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
	}
	if length > p.options.MaxStringBytes {
		if err := p.reader.Skip(uint64(length)); err != nil {
			p.err = fmt.Errorf("amf0: skip oversized %s at byte %d: %w", role, start, err)
			return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
		}
		p.warn(Diagnostic{
			Code: CodeStringLimitExceeded, Severity: SeverityWarning, Offset: start,
			Message:  "AMF0 string exceeds the configured per-string storage limit and was omitted",
			Expected: number(uint64(p.options.MaxStringBytes)), Actual: number(uint64(length)),
		})
		return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
	}
	remainingBudget := p.options.MaxTotalStringBytes - p.result.StringBytes
	if uint64(length) > remainingBudget {
		if err := p.reader.Skip(uint64(length)); err != nil {
			p.err = fmt.Errorf("amf0: skip unbudgeted %s at byte %d: %w", role, start, err)
			return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
		}
		p.warn(Diagnostic{
			Code: CodeStringBudgetExceeded, Severity: SeverityWarning, Offset: start,
			Message:  "AMF0 cumulative string storage budget was exhausted and the string was omitted",
			Expected: number(remainingBudget), Actual: number(uint64(length)),
		})
		return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
	}
	data, err := p.reader.ReadBytes(uint64(length))
	if err != nil {
		p.err = fmt.Errorf("amf0: read %s at byte %d: %w", role, start, err)
		return storedString{omitted: true, span: byteSpan(start, p.reader.Offset())}
	}
	p.result.StringBytes += uint64(length)
	value := string(data)
	if !utf8.ValidString(value) {
		invalidAt := firstInvalidUTF8Byte(value)
		p.warn(Diagnostic{
			Code: CodeUTF8Invalid, Severity: SeverityWarning, Offset: start + uint64(invalidAt),
			Message: "AMF0 string contains invalid UTF-8 and was preserved byte-for-byte in the Go string",
		})
	}
	return storedString{value: value, span: byteSpan(start, p.reader.Offset())}
}

func firstInvalidUTF8Byte(value string) int {
	for offset := 0; offset < len(value); {
		runeValue, size := utf8.DecodeRuneInString(value[offset:])
		if runeValue == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return 0
}

func (p *parser) linkNode(id, parent NodeID) {
	if parent == NoNode {
		p.result.Roots = append(p.result.Roots, id)
		return
	}
	parentNode := &p.result.Nodes[parent]
	if parentNode.FirstChild == NoNode {
		parentNode.FirstChild = id
	} else {
		p.result.Nodes[p.lastChild[parent]].NextSibling = id
	}
	p.lastChild[parent] = id
	parentNode.ChildCount++
}

func referenceable(marker Type) bool {
	switch marker {
	case TypeObject, TypeECMAArray, TypeStrictArray, TypeDate, TypeXMLDocument, TypeTypedObject:
		return true
	default:
		return false
	}
}

func (p *parser) ensure(length uint64, code, message string) bool {
	if p.reader.Remaining() >= length {
		return true
	}
	available := p.reader.Remaining()
	if err := p.reader.Skip(available); err != nil {
		p.err = fmt.Errorf("amf0: consume truncated field at byte %d: %w", p.reader.Offset(), err)
		return false
	}
	p.fail(Diagnostic{
		Code: code, Severity: SeverityError, Offset: p.reader.Offset(), Message: message,
		Expected: number(length), Actual: number(available),
	})
	return false
}

func (p *parser) readU8(label string) (uint8, bool) {
	if !p.readInto(p.scratch[:1], label) {
		return 0, false
	}
	return p.scratch[0], true
}

func (p *parser) readU16(label string) (uint16, bool) {
	if !p.readInto(p.scratch[:2], label) {
		return 0, false
	}
	return binary.BigEndian.Uint16(p.scratch[:2]), true
}

func (p *parser) readU32(label string) (uint32, bool) {
	if !p.readInto(p.scratch[:4], label) {
		return 0, false
	}
	return binary.BigEndian.Uint32(p.scratch[:4]), true
}

func (p *parser) readU64(label string) (uint64, bool) {
	if !p.readInto(p.scratch[:8], label) {
		return 0, false
	}
	return binary.BigEndian.Uint64(p.scratch[:8]), true
}

func (p *parser) readInto(destination []byte, label string) bool {
	start := p.reader.Offset()
	if err := p.reader.ReadInto(destination); err != nil {
		p.err = fmt.Errorf("amf0: read %s at byte %d: %w", label, start, err)
		return false
	}
	return true
}

func (p *parser) warn(diagnostic Diagnostic) {
	p.diagnostics.add(diagnostic)
}

func (p *parser) fail(diagnostic Diagnostic) {
	recovery := p.end
	diagnostic.RecoveryOffset = &recovery
	p.diagnostics.add(diagnostic)
	p.fatal = true
}

func (p *parser) stopped() bool { return p.fatal || p.err != nil }

func byteSpan(start, end uint64) cursor.Span {
	return cursor.Span{ByteOffset: start, BitLength: uint32((end - start) * 8)}
}

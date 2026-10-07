#!/usr/bin/env python3
"""Validate public JSON Schemas and their committed contract examples."""

from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parent.parent
CONTRACTS = (
    ("docs/schemas/flv-mp3-tag-detail-v3.schema.json", "testdata/golden/flv-mp3-side-info-v3.json"),
    ("docs/schemas/flv-mp3-tag-detail-v3.schema.json", "testdata/golden/flv-mp3-side-info-truncated-v3.json"),
    ("docs/schemas/flv-mp3-tag-detail-v3.schema.json", "testdata/golden/flv-mp3-side-info-before-tag-v3.json"),
    ("docs/schemas/flv-avc-tag-detail-v3.schema.json", "testdata/golden/flv-avc-detail-v3.json"),
    ("docs/schemas/flv-avc-tag-detail-v3.schema.json", "testdata/golden/flv-avc-detail-nalu-v3.json"),
    ("docs/schemas/flv-avc-tag-detail-v3.schema.json", "testdata/golden/flv-avc-detail-missing-v3.json"),
    ("docs/schemas/flv-avc-tag-detail-v2.schema.json", "testdata/golden/flv-avc-detail-v2.json"),
    ("docs/schemas/flv-avc-tag-detail-v2.schema.json", "testdata/golden/flv-avc-detail-truncated-v2.json"),
    ("docs/schemas/flv-mp3-tag-detail-v2.schema.json", "testdata/golden/flv-mp3-detail-v2.json"),
    ("docs/schemas/flv-mp3-tag-detail-v2.schema.json", "testdata/golden/flv-mp3-detail-truncated-v2.json"),
    ("docs/schemas/desktop-diagnostic-snapshot-v1.schema.json", "testdata/golden/desktop-diagnostic-snapshot-minimal-v1.json"),
    ("docs/schemas/desktop-diagnostic-snapshot-v1.schema.json", "testdata/golden/desktop-diagnostic-snapshot-full-v1.json"),
    ("docs/schemas/flv-tag-page-v1.schema.json", "testdata/golden/flv-tag-page-v1.json"),
    ("docs/schemas/flv-tag-page-v1.schema.json", "testdata/golden/flv-tag-page-empty-v1.json"),
    ("docs/schemas/flv-tag-page-v1.schema.json", "testdata/golden/flv-tag-page-partial-v1.json"),
    (
        "docs/schemas/flv-scan-report-v1.schema.json",
        "testdata/golden/flv-basic-v1.json",
    ),
    (
        "docs/schemas/flv-tag-detail-v1.schema.json",
        "testdata/golden/flv-amf0-detail-v1.json",
    ),
    (
        "docs/schemas/flv-tag-detail-v1.schema.json",
        "testdata/golden/flv-amf0-detail-malformed-v1.json",
    ),
    (
        "docs/schemas/flv-aac-tag-detail-v1.schema.json",
        "testdata/golden/flv-aac-detail-v1.json",
    ),
    (
        "docs/schemas/flv-aac-tag-detail-v1.schema.json",
        "testdata/golden/flv-aac-detail-raw-v1.json",
    ),
    (
        "docs/schemas/flv-aac-tag-detail-v1.schema.json",
        "testdata/golden/flv-aac-detail-malformed-v1.json",
    ),
    (
        "docs/schemas/flv-avc-tag-detail-v1.schema.json",
        "testdata/golden/flv-avc-detail-v1.json",
    ),
    (
        "docs/schemas/flv-avc-tag-detail-v1.schema.json",
        "testdata/golden/flv-avc-detail-nalu-v1.json",
    ),
    (
        "docs/schemas/flv-avc-tag-detail-v1.schema.json",
        "testdata/golden/flv-avc-detail-malformed-v1.json",
    ),
    (
        "docs/schemas/flv-mp3-tag-detail-v1.schema.json",
        "testdata/golden/flv-mp3-detail-v1.json",
    ),
    (
        "docs/schemas/flv-mp3-tag-detail-v1.schema.json",
        "testdata/golden/flv-mp3-detail-free-v1.json",
    ),
    (
        "docs/schemas/flv-mp3-tag-detail-v1.schema.json",
        "testdata/golden/flv-mp3-detail-malformed-v1.json",
    ),
    (
        "docs/schemas/flv-mp3-tag-detail-v1.schema.json",
        "testdata/golden/flv-mp3-detail-truncated-v1.json",
    ),
    (
        "docs/schemas/flv-media-summary-v1.schema.json",
        "testdata/golden/flv-media-summary-v1.json",
    ),
    (
        "docs/schemas/flv-media-summary-v2.schema.json",
        "testdata/golden/flv-media-summary-v2.json",
    ),
    (
        "docs/schemas/flv-media-summary-v2.schema.json",
        "testdata/golden/flv-media-summary-mp3-free-v2.json",
    ),
    (
        "docs/schemas/flv-media-summary-v2.schema.json",
        "testdata/golden/flv-media-summary-mp3-malformed-v2.json",
    ),
    (
        "testdata/manifests/sample-manifest-v1.schema.json",
        "testdata/manifests/example-minimal-header-only.sample.json",
    ),
)


def load_json(relative_path: str) -> object:
    with (ROOT / relative_path).open(encoding="utf-8") as source:
        return json.load(source)


def validation_errors(validator: Draft202012Validator, instance: object) -> list:
    return sorted(
        validator.iter_errors(instance),
        key=lambda error: tuple(str(part) for part in error.absolute_path),
    )


def require_valid(validator: Draft202012Validator, instance: object, label: str) -> None:
    errors = validation_errors(validator, instance)
    if errors:
        first = errors[0]
        location = "/".join(str(part) for part in first.absolute_path) or "<root>"
        raise SystemExit(f"{label}:{location}: {first.message}")


def require_invalid(validator: Draft202012Validator, instance: object, label: str) -> None:
    if not validation_errors(validator, instance):
        raise SystemExit(f"{label}: invalid contract mutation was accepted")


def validate_desktop_snapshot_edge_cases(validator: Draft202012Validator) -> None:
    full = load_json("testdata/golden/desktop-diagnostic-snapshot-full-v1.json")
    minimal = load_json("testdata/golden/desktop-diagnostic-snapshot-minimal-v1.json")
    mutations = (
        ("wrong snapshot version", ["schemaVersion"], 2),
        ("CLI identity is not desktop snapshot", ["documentType"], "flv-media-summary"),
        ("absolute source path is not a file name", ["fileName"], "C:\\private\\media.flv"),
        ("timestamp must be UTC", ["createdAt"], "2026-09-29T08:00:00+08:00"),
        ("lossy numeric offset", ["selectedTag", "offset"], 9007199254740993),
        ("noncanonical offset", ["selectedTag", "offset"], "01"),
        ("uint64 offset overflow", ["selectedTag", "offset"], "18446744073709551616"),
        ("uint32 DTS overflow", ["trend", "points", 0, "dts"], "4294967296"),
        ("source window point bound", ["trend", "points"], full["trend"]["points"][:1] * 513),
        ("context visit bound", ["trend", "contextScanned"], 4097),
        ("unknown context must not invent a delta", ["trend", "points", 0, "deltaMs"], "0"),
        ("positive gap threshold", ["selection", "trend", "gapThreshold"], "0"),
        ("gap threshold overflow", ["selection", "trend", "gapThreshold"], "4294967296"),
        ("bounded retained Hex", ["hex", "window", "hex"], "00" * 4097),
        ("even Hex bytes", ["hex", "window", "hex"], "abc"),
        ("local observation is not corruption diagnostic", ["trendIssues", 0, "kind"], "parser_error"),
        ("ready-only detail", ["detail", "state"], "pending"),
        ("summary page limit", ["summaryPage", "page", "bounds", "limit"], 101),
    )
    for label, path, value in mutations:
        changed = deepcopy(full)
        parent = changed
        for key in path[:-1]:
            parent = parent[key]
        parent[path[-1]] = value
        require_invalid(validator, changed, label)
    omitted = deepcopy(minimal)
    omitted["omitted"] = []
    require_invalid(validator, omitted, "every missing section has an omission reason")
    misleading = deepcopy(full)
    misleading["omitted"] = [{"section": "detail", "reason": "not_requested"}]
    require_invalid(validator, misleading, "present section cannot also be omitted")
    extra = deepcopy(full)
    extra["sourcePath"] = "C:\\private\\media.flv"
    require_invalid(validator, extra, "closed snapshot envelope")
    for name in ("minimal", "full"):
        path = ROOT / f"testdata/golden/desktop-diagnostic-snapshot-{name}-v1.json"
        if path.stat().st_size > 2 << 20:
            raise SystemExit(f"{path}: snapshot exceeds 2 MiB")


def validate_media_summary_edge_cases(validator: Draft202012Validator) -> None:
    summary = load_json("testdata/golden/flv-media-summary-v1.json")

    unsupported = deepcopy(summary)
    unsupported["source"]["sizeBytes"] = 232
    unsupported["scan"]["bytesScanned"] = 232
    unsupported["scan"]["indexedTagCount"] = 9
    unsupported["summary"]["complete"] = False
    unsupported["summary"]["tags"]["total"] = 9
    unsupported["summary"]["tags"]["audio"] = 4
    unsupported["summary"]["tags"]["dataSizeBytes"] = 84
    unsupported["summary"]["audio"]["tagCount"] = 4
    unsupported["summary"]["audio"]["indexedDataSizeBytes"] = 13
    unsupported["summary"]["audio"]["unclassifiedTagCount"] = 1
    del unsupported["summary"]["avSync"]
    unsupported["analysisDiagnostics"] = [
        {
            "code": "MEDIA_AUDIO_CODEC_UNSUPPORTED",
            "severity": "warning",
            "tagId": 8,
            "offset": 226,
            "bitOffset": 0,
            "message": "synthetic unsupported MP3 tag",
            "actual": 2,
        }
    ]
    require_valid(validator, unsupported, "incomplete AAC/AVC-only summary with unsupported audio")

    wrong_identity = deepcopy(summary)
    wrong_identity["documentType"] = "flv-summary"
    require_invalid(validator, wrong_identity, "media summary wrong document identity")

    extra_field = deepcopy(summary)
    extra_field["summary"]["audio"]["mystery"] = 1
    require_invalid(validator, extra_field, "media summary additional field")

    negative_count = deepcopy(summary)
    negative_count["summary"]["tags"]["total"] = -1
    require_invalid(validator, negative_count, "media summary negative count")

    bad_fingerprint = deepcopy(summary)
    bad_fingerprint["summary"]["aacConfigurations"]["events"][0][
        "fingerprintSha256"
    ] = "not-a-sha256"
    require_invalid(validator, bad_fingerprint, "media summary invalid fingerprint")

    bad_bitrate_basis = deepcopy(summary)
    bad_bitrate_basis["summary"]["video"]["bitrate"]["basis"] = "timestampRange"
    require_invalid(validator, bad_bitrate_basis, "media summary ambiguous bitrate basis")

    incomplete_scan = deepcopy(summary)
    incomplete_scan["scan"]["complete"] = False
    require_invalid(validator, incomplete_scan, "complete media summary with incomplete scan")

    classified_false = deepcopy(summary)
    classified_false["summary"]["audio"]["unclassifiedTagCount"] = 1
    require_invalid(validator, classified_false, "complete media summary with unclassified tag")

    skipped_configuration = deepcopy(summary)
    skipped_configuration["summary"]["avcConfigurations"]["skippedByBudgetCount"] = 1
    require_invalid(validator, skipped_configuration, "complete media summary with skipped configuration")

    incomplete_configuration = deepcopy(summary)
    incomplete_configuration["summary"]["aacConfigurations"]["events"][0]["complete"] = False
    require_invalid(validator, incomplete_configuration, "complete media summary with incomplete configuration")

    analysis_error = deepcopy(summary)
    analysis_error["analysisDiagnostics"] = [
        {
            "code": "MEDIA_VIDEO_HEADER_TRUNCATED",
            "severity": "error",
            "tagId": 1,
            "offset": 20,
            "bitOffset": 0,
            "message": "synthetic error",
            "recoveryOffset": 20,
        }
    ]
    require_invalid(validator, analysis_error, "complete media summary with analysis error")

    scan_error = deepcopy(summary)
    scan_error["scanDiagnostics"] = [
        {
            "code": "FLV_DIAGNOSTIC_LIMIT_REACHED",
            "severity": "error",
            "offset": 20,
            "bitOffset": 0,
            "message": "synthetic scan error",
        }
    ]
    require_invalid(validator, scan_error, "complete media summary with scan error")

    negative_audio_dts = deepcopy(summary)
    negative_audio_dts["summary"]["audio"]["timing"]["firstMs"] = -1
    negative_audio_dts["summary"]["audio"]["timing"]["minMs"] = -1
    require_invalid(validator, negative_audio_dts, "negative raw audio DTS")

    for field in ("rangeMs", "largestForwardGapMs"):
        dts_delta_out_of_range = deepcopy(summary)
        dts_delta_out_of_range["summary"]["audio"]["timing"][field] = 4294967296
        require_invalid(
            validator,
            dts_delta_out_of_range,
            f"raw DTS {field} outside uint32 range",
        )

    for value in (-8388609, 4303355903):
        pts_out_of_range = deepcopy(summary)
        pts_out_of_range["summary"]["videoPts"]["firstMs"] = value
        require_invalid(
            validator,
            pts_out_of_range,
            f"video PTS outside uint32 DTS plus signedInt24 range: {value}",
        )

        keyframe_pts_out_of_range = deepcopy(summary)
        keyframe_pts_out_of_range["summary"]["keyframes"]["entries"][0][
            "pts"
        ] = value
        require_invalid(
            validator,
            keyframe_pts_out_of_range,
            f"keyframe PTS outside uint32 DTS plus signedInt24 range: {value}",
        )

    for field in ("rangeMs", "largestForwardGapMs"):
        pts_delta_out_of_range = deepcopy(summary)
        pts_delta_out_of_range["summary"]["videoPts"][field] = 4311744511
        require_invalid(
            validator,
            pts_delta_out_of_range,
            f"video PTS {field} outside exact PTS domain span",
        )

    av_sync_in_incomplete_summary = deepcopy(summary)
    av_sync_in_incomplete_summary["summary"]["complete"] = False
    require_invalid(
        validator,
        av_sync_in_incomplete_summary,
        "A/V relationship in incomplete media summary",
    )

    for track in ("audio", "video"):
        av_sync_without_timing = deepcopy(summary)
        del av_sync_without_timing["summary"][track]["timing"]
        require_invalid(
            validator,
            av_sync_without_timing,
            f"A/V relationship without {track} timing",
        )

    av_sync_without_video_pts = deepcopy(summary)
    del av_sync_without_video_pts["summary"]["videoPts"]
    require_invalid(
        validator,
        av_sync_without_video_pts,
        "A/V relationship without video PTS",
    )

    for track in ("audio", "video"):
        av_sync_across_regression = deepcopy(summary)
        av_sync_across_regression["summary"][track]["timing"][
            "regressionCount"
        ] = 1
        require_invalid(
            validator,
            av_sync_across_regression,
            f"A/V relationship across {track} DTS regression",
        )

    disjoint_with_overlap = deepcopy(summary)
    disjoint_with_overlap["summary"]["avSync"]["rangesOverlap"] = False
    disjoint_with_overlap["summary"]["avSync"]["overlapMs"] = 1
    require_invalid(
        validator,
        disjoint_with_overlap,
        "disjoint A/V ranges with non-zero overlap",
    )

    for field in ("startExtremaSkewMs", "endExtremaSkewMs"):
        for value in (-4303355904, 4303355903):
            av_sync_skew_out_of_range = deepcopy(summary)
            av_sync_skew_out_of_range["summary"]["avSync"][field] = value
            require_invalid(
                validator,
                av_sync_skew_out_of_range,
                f"A/V {field} outside exact PTS-minus-DTS range: {value}",
            )

    av_sync_overlap_out_of_range = deepcopy(summary)
    av_sync_overlap_out_of_range["summary"]["avSync"]["overlapMs"] = 4294967296
    require_invalid(
        validator,
        av_sync_overlap_out_of_range,
        "A/V overlap outside raw audio DTS range",
    )

    for field in ("audioObjectType", "samplingFrequencyHz", "channelConfiguration"):
        incomplete_complete_aac = deepcopy(summary)
        del incomplete_complete_aac["summary"]["aacConfigurations"]["events"][0][
            field
        ]
        require_invalid(
            validator,
            incomplete_complete_aac,
            f"complete AAC configuration without {field}",
        )

    for field in ("profile", "lengthSize"):
        incomplete_complete_avc = deepcopy(summary)
        del incomplete_complete_avc["summary"]["avcConfigurations"]["events"][0][
            field
        ]
        require_invalid(
            validator,
            incomplete_complete_avc,
            f"complete AVC configuration without {field}",
        )

    for codec in ("aacConfigurations", "avcConfigurations"):
        changed_incomplete_configuration = deepcopy(summary)
        changed_incomplete_configuration["summary"]["complete"] = False
        del changed_incomplete_configuration["summary"]["avSync"]
        event = changed_incomplete_configuration["summary"][codec]["events"][0]
        event["complete"] = False
        event["changedFromPrevious"] = True
        require_invalid(
            validator,
            changed_incomplete_configuration,
            f"incomplete {codec} event with a comparison result",
        )

    non_avc_keyframe = deepcopy(summary)
    non_avc_keyframe["summary"]["keyframes"]["entries"][0]["codecId"] = 2
    require_invalid(validator, non_avc_keyframe, "non-AVC media-summary keyframe")

    keyframe_without_pts = deepcopy(summary)
    del keyframe_without_pts["summary"]["keyframes"]["entries"][0]["pts"]
    require_invalid(validator, keyframe_without_pts, "media-summary keyframe without PTS")

    oversized_keyframe_payload = deepcopy(summary)
    oversized_keyframe_payload["summary"]["keyframes"]["entries"][0][
        "dataSizeBytes"
    ] = 16777216
    require_invalid(
        validator,
        oversized_keyframe_payload,
        "media-summary keyframe DataSize outside unsigned 24-bit range",
    )

    for value in (0, 4294967296):
        gop_duration_out_of_range = deepcopy(summary)
        gop_duration_out_of_range["summary"]["keyframes"]["entries"][0][
            "gopDurationMs"
        ] = value
        require_invalid(
            validator,
            gop_duration_out_of_range,
            f"media-summary GOP duration outside positive uint32 range: {value}",
        )

    signed_24_overflow = deepcopy(summary)
    signed_24_overflow["summary"]["compositionTime"]["maxMs"] = 8388608
    require_invalid(validator, signed_24_overflow, "composition time outside signed 24-bit range")


def validate_media_summary_v2_edge_cases(validator: Draft202012Validator) -> None:
    indexed = load_json("testdata/golden/flv-media-summary-v2.json")
    free = load_json("testdata/golden/flv-media-summary-mp3-free-v2.json")
    malformed = load_json("testdata/golden/flv-media-summary-mp3-malformed-v2.json")
    v1 = Draft202012Validator(load_json("docs/schemas/flv-media-summary-v1.schema.json"))
    require_invalid(v1, indexed, "v2 cannot masquerade as v1")
    require_invalid(validator, load_json("testdata/golden/flv-media-summary-v1.json"), "v1 is not v2")

    wrong_version = deepcopy(indexed)
    wrong_version["schemaVersion"] = 1
    require_invalid(validator, wrong_version, "v2 wrong schema version")
    for field in ("mp3", "audioCodecs"):
        missing = deepcopy(indexed)
        del missing["summary"][field]
        require_invalid(validator, missing, f"v2 missing {field}")

    for field in ("unknownTagCount", "otherTagCount"):
        incomplete = deepcopy(indexed)
        incomplete["summary"]["audioCodecs"][field] = 1
        require_invalid(validator, incomplete, f"complete v2 with {field}")
    bad_count = deepcopy(indexed)
    bad_count["summary"]["audioCodecs"]["switchCount"] = -1
    require_invalid(validator, bad_count, "negative codec switch count")

    codec_switch_skew = deepcopy(free)
    codec_switch_skew["summary"]["audioCodecs"]["switchCount"] = 1
    require_invalid(validator, codec_switch_skew, "A/V skew across a codec switch")

    # Retaining a prefix does not invalidate exact aggregate counters.
    retained = deepcopy(indexed)
    retained["summary"]["mp3"]["headers"]["events"] = retained["summary"]["mp3"]["headers"]["events"][:1]
    retained["summary"]["mp3"]["headers"]["truncated"] = True
    require_valid(validator, retained, "complete v2 with truncated retained events")

    for field in ("parameters", "bitrateMode", "bitrateKbps"):
        missing = deepcopy(indexed)
        del missing["summary"]["mp3"]["headers"]["events"][0][field]
        require_invalid(validator, missing, f"complete indexed MP3 missing {field}")
    invented_free_bitrate = deepcopy(free)
    invented_free_bitrate["summary"]["mp3"]["headers"]["events"][0]["bitrateKbps"] = 128
    require_invalid(validator, invented_free_bitrate, "free format invented bitrate")

    parameter_mutations = (
        ("version", {"value": 1, "name": "reserved"}),
        ("version", {"value": 3, "name": "mpeg2"}),
        ("samplingFrequencyHz", 22050),
        ("samplesPerFrame", 576),
        ("channelMode", {"value": 1, "name": "stereo"}),
        ("channels", 1),
        ("unrecognized", 0),
    )
    for field, value in parameter_mutations:
        mutated = deepcopy(indexed)
        mutated["summary"]["mp3"]["headers"]["events"][0]["parameters"][field] = value
        require_invalid(validator, mutated, f"MP3 parameter contradiction: {field}")
    for value in (0, 8, 129, 321):
        mutated = deepcopy(indexed)
        mutated["summary"]["mp3"]["headers"]["events"][0]["bitrateKbps"] = value
        require_invalid(validator, mutated, f"MPEG-1 indexed bitrate outside table: {value}")
    for field in ("frameLengthBytes", "decodedDurationMs", "vbr"):
        mutated = deepcopy(indexed)
        mutated["summary"]["mp3"]["headers"]["events"][0][field] = 1
        require_invalid(validator, mutated, f"unpromised MP3 observation: {field}")

    for field in ("parametersChangedFromPrevious", "bitrateChangedFromPrevious"):
        mutated = deepcopy(indexed)
        del mutated["summary"]["mp3"]["headers"]["events"][1][field]
        require_invalid(validator, mutated, f"unpaired MP3 comparison: {field}")
    malformed_facts = deepcopy(malformed)
    malformed_facts["summary"]["mp3"]["headers"]["events"][1]["parameters"] = indexed["summary"]["mp3"]["headers"]["events"][0]["parameters"]
    require_invalid(validator, malformed_facts, "incomplete MP3 event with confident parameters")
    malformed_complete = deepcopy(retained)
    malformed_complete["summary"]["mp3"]["headers"]["malformedCount"] = 1
    require_invalid(validator, malformed_complete, "complete summary hides malformed unretained headers")
    malformed_event = deepcopy(indexed)
    malformed_event["summary"]["mp3"]["headers"]["events"][0] = malformed["summary"]["mp3"]["headers"]["events"][1]
    require_invalid(validator, malformed_event, "complete summary with malformed event")

    wrong_mode = deepcopy(free)
    wrong_mode["summary"]["mp3"]["headers"]["events"][0]["bitrateMode"] = "vbr"
    require_invalid(validator, wrong_mode, "first headers cannot claim VBR")
    for field, value in (("code", "MP3_UNKNOWN"), ("severity", "warning")):
        mutated = deepcopy(malformed)
        mutated["analysisDiagnostics"][0][field] = value
        require_invalid(validator, mutated, f"MP3 diagnostic wrong {field}")
    missing_recovery = deepcopy(malformed)
    del missing_recovery["analysisDiagnostics"][0]["recoveryOffset"]
    require_invalid(validator, missing_recovery, "MP3 error missing recovery")
    warning_recovery = deepcopy(free)
    warning_recovery["analysisDiagnostics"][0]["recoveryOffset"] = 29
    require_invalid(validator, warning_recovery, "MP3 warning with fabricated recovery")


def validate_detail_edge_cases(validator: Draft202012Validator) -> None:
    base = load_json("testdata/golden/flv-amf0-detail-v1.json")

    for bits, value_class, include_value, value in (
        ("8000000000000000", "finite", True, -0.0),
        ("7ff8000000000001", "nan", False, None),
        ("7ff0000000000000", "positiveInfinity", False, None),
        ("fff0000000000000", "negativeInfinity", False, None),
    ):
        edge = deepcopy(base)
        number = edge["detail"]["nodes"][2]["number"]
        number["bits"] = bits
        number["class"] = value_class
        number.pop("value", None)
        if include_value:
            number["value"] = value
        require_valid(validator, edge, f"synthetic IEEE-754 edge {value_class}")

    replacement = deepcopy(base)
    replacement["detail"]["eventName"] = "\ufffd"
    replacement["detail"]["nodes"][0]["string"]["value"] = "\ufffd"
    require_valid(validator, replacement, "synthetic invalid-UTF-8 JSON replacement")

    wrong_name = deepcopy(base)
    wrong_name["detail"]["nodes"][0]["typeName"] = "number"
    require_invalid(validator, wrong_name, "mismatched AMF0 type name")

    wrong_subtype = deepcopy(base)
    wrong_subtype["detail"]["nodes"][3]["number"] = deepcopy(
        wrong_subtype["detail"]["nodes"][2]["number"]
    )
    require_invalid(validator, wrong_subtype, "mismatched AMF0 subtype field")

    missing_complete_field = deepcopy(base)
    missing_complete_field["detail"]["nodes"][0]["complete"] = True
    del missing_complete_field["detail"]["nodes"][0]["string"]
    require_invalid(validator, missing_complete_field, "complete AMF0 node without syntax")


def validate_aac_detail_edge_cases(validator: Draft202012Validator) -> None:
    sequence = load_json("testdata/golden/flv-aac-detail-v1.json")
    raw = load_json("testdata/golden/flv-aac-detail-raw-v1.json")
    malformed = load_json("testdata/golden/flv-aac-detail-malformed-v1.json")

    wrong_identity = deepcopy(sequence)
    wrong_identity["documentType"] = "flv-tag-detail"
    require_invalid(validator, wrong_identity, "AAC detail with AMF0 document identity")

    wrong_tag = deepcopy(sequence)
    wrong_tag["tag"]["typeCode"] = 18
    wrong_tag["tag"]["typeName"] = "script"
    require_invalid(validator, wrong_tag, "AAC detail attached to ScriptData tag")

    sequence_without_config = deepcopy(sequence)
    del sequence_without_config["detail"]["audioSpecificConfig"]
    require_invalid(validator, sequence_without_config, "AAC sequence header without ASC")

    sequence_with_raw_span = deepcopy(sequence)
    sequence_with_raw_span["detail"]["rawDataSpan"] = deepcopy(
        sequence_with_raw_span["detail"]["dataSpan"]
    )
    require_invalid(validator, sequence_with_raw_span, "AAC sequence header with raw span")

    raw_with_config = deepcopy(raw)
    raw_with_config["detail"]["audioSpecificConfig"] = deepcopy(
        sequence["detail"]["audioSpecificConfig"]
    )
    require_invalid(validator, raw_with_config, "raw AAC packet with ASC")

    malformed_claims_complete = deepcopy(malformed)
    malformed_claims_complete["detail"]["complete"] = True
    require_invalid(validator, malformed_claims_complete, "malformed AAC marked complete")

    warning_with_recovery = deepcopy(raw)
    warning_with_recovery["diagnostics"] = [
        {
            "code": "AAC_FLV_SOUND_RATE_UNEXPECTED",
            "severity": "warning",
            "offset": 24,
            "bitOffset": 4,
            "message": "synthetic warning",
            "expected": 3,
            "actual": 2,
            "recoveryOffset": 28,
        }
    ]
    require_invalid(validator, warning_with_recovery, "AAC warning with recovery offset")

    warning = deepcopy(raw)
    warning["diagnostics"] = [
        {
            "code": "AAC_FLV_SOUND_TYPE_UNEXPECTED",
            "severity": "warning",
            "offset": 24,
            "bitOffset": 7,
            "message": "synthetic warning",
            "expected": 1,
            "actual": 0,
        }
    ]
    require_valid(validator, warning, "complete AAC with framing warning")

    header_missing = deepcopy(raw)
    header_missing["detail"] = {
        "kind": "aacAudio",
        "span": deepcopy(raw["detail"]["span"]),
        "bitsConsumed": 0,
        "complete": False,
    }
    header_missing["diagnostics"] = [
        {
            "code": "AAC_FLV_AUDIO_HEADER_TRUNCATED",
            "severity": "error",
            "offset": 24,
            "bitOffset": 0,
            "message": "synthetic missing audio header",
            "expected": 1,
            "actual": 0,
            "recoveryOffset": 24,
        }
    ]
    require_valid(validator, header_missing, "empty complete Audio Tag payload")

    packet_type_missing = deepcopy(raw)
    packet_type_missing["detail"].pop("packetType")
    packet_type_missing["detail"].pop("dataSpan")
    packet_type_missing["detail"].pop("rawDataSpan")
    packet_type_missing["detail"]["bitsConsumed"] = 8
    packet_type_missing["detail"]["complete"] = False
    packet_type_missing["diagnostics"] = [
        {
            "code": "AAC_FLV_PACKET_TYPE_TRUNCATED",
            "severity": "error",
            "offset": 25,
            "bitOffset": 0,
            "message": "synthetic missing packet type",
            "expected": 2,
            "actual": 1,
            "recoveryOffset": 25,
        }
    ]
    require_valid(validator, packet_type_missing, "AAC packet-type truncation")

    unknown_packet_type = deepcopy(raw)
    unknown_packet_type["detail"]["packetType"]["value"] = 2
    unknown_packet_type["detail"]["packetType"]["name"] = "unknown"
    unknown_packet_type["detail"].pop("rawDataSpan")
    unknown_packet_type["detail"]["bitsConsumed"] = 16
    unknown_packet_type["detail"]["complete"] = False
    unknown_packet_type["diagnostics"] = [
        {
            "code": "AAC_FLV_PACKET_TYPE_UNKNOWN",
            "severity": "error",
            "offset": 25,
            "bitOffset": 0,
            "message": "synthetic unknown packet type",
            "actual": 2,
            "recoveryOffset": 28,
        }
    ]
    require_valid(validator, unknown_packet_type, "unknown AAC packet type")


def validate_mp3_detail_edge_cases(validator: Draft202012Validator) -> None:
    indexed = load_json("testdata/golden/flv-mp3-detail-v1.json")
    free = load_json("testdata/golden/flv-mp3-detail-free-v1.json")
    malformed = load_json("testdata/golden/flv-mp3-detail-malformed-v1.json")
    truncated = load_json("testdata/golden/flv-mp3-detail-truncated-v1.json")

    require_valid(validator, indexed, "indexed MP3 first-frame header")
    require_valid(validator, free, "free-format MP3 first-frame header")
    require_valid(validator, malformed, "malformed MP3 first-frame header")
    require_valid(validator, truncated, "truncated MP3 first-frame header")

    wrong_identity = deepcopy(indexed)
    wrong_identity["documentType"] = "flv-aac-tag-detail"
    require_invalid(validator, wrong_identity, "MP3 detail with AAC document identity")

    wrong_tag = deepcopy(indexed)
    wrong_tag["tag"]["typeCode"] = 9
    wrong_tag["tag"]["typeName"] = "video"
    require_invalid(validator, wrong_tag, "MP3 detail attached to video tag")

    filtered_tag = deepcopy(indexed)
    filtered_tag["tag"]["filter"] = True
    require_invalid(validator, filtered_tag, "MP3 detail attached to filtered tag")

    incomplete_tag = deepcopy(indexed)
    incomplete_tag["tag"]["payloadComplete"] = False
    require_invalid(validator, incomplete_tag, "MP3 detail attached to incomplete tag")

    wrong_sound_format = deepcopy(indexed)
    wrong_sound_format["detail"]["audioTagHeader"]["soundFormat"]["value"] = 10
    wrong_sound_format["detail"]["audioTagHeader"]["soundFormat"]["name"] = "AAC"
    require_invalid(validator, wrong_sound_format, "MP3 detail with non-MP3 SoundFormat")

    missing_bitrate = deepcopy(indexed)
    del missing_bitrate["detail"]["firstFrameHeader"]["bitrateKbps"]
    require_invalid(validator, missing_bitrate, "indexed MP3 without derived bitrate")

    missing_frame_length = deepcopy(indexed)
    del missing_frame_length["detail"]["firstFrameHeader"]["frameLengthBytes"]
    require_invalid(validator, missing_frame_length, "indexed MP3 without frame length")

    wrong_indexed_bitrate = deepcopy(indexed)
    wrong_indexed_bitrate["detail"]["firstFrameHeader"]["bitrateKbps"] = 129
    require_invalid(validator, wrong_indexed_bitrate, "indexed MP3 with wrong bitrate")

    wrong_sampling_frequency = deepcopy(indexed)
    wrong_sampling_frequency["detail"]["firstFrameHeader"][
        "samplingFrequencyHz"
    ] = 48000
    require_invalid(
        validator,
        wrong_sampling_frequency,
        "MP3 header with wrong derived sampling frequency",
    )

    mismatched_channel_count = deepcopy(indexed)
    mismatched_channel_count["detail"]["firstFrameHeader"]["channels"] = 1
    require_invalid(
        validator,
        mismatched_channel_count,
        "stereo MP3 channel mode with mono channel count",
    )

    mismatched_crc_flag = deepcopy(indexed)
    mismatched_crc_flag["detail"]["firstFrameHeader"]["crcProtected"] = True
    require_invalid(
        validator,
        mismatched_crc_flag,
        "unprotected MP3 header marked CRC protected",
    )

    indexed_claims_free = deepcopy(indexed)
    indexed_claims_free["detail"]["firstFrameHeader"]["bitrateMode"] = "free"
    require_invalid(validator, indexed_claims_free, "indexed MP3 marked free-format")

    free_with_bitrate = deepcopy(free)
    free_with_bitrate["detail"]["firstFrameHeader"]["bitrateKbps"] = 128
    require_invalid(validator, free_with_bitrate, "free-format MP3 with bitrate")

    free_with_frame_length = deepcopy(free)
    free_with_frame_length["detail"]["firstFrameHeader"]["frameLengthBytes"] = 417
    require_invalid(validator, free_with_frame_length, "free-format MP3 with frame length")

    free_without_warning = deepcopy(free)
    free_without_warning["diagnostics"] = []
    require_invalid(validator, free_without_warning, "free-format MP3 without warning")

    malformed_claims_complete = deepcopy(malformed)
    malformed_claims_complete["detail"]["complete"] = True
    malformed_claims_complete["detail"]["firstFrameHeader"]["complete"] = True
    require_invalid(validator, malformed_claims_complete, "malformed MP3 marked complete")

    mismatched_header_complete = deepcopy(indexed)
    mismatched_header_complete["detail"]["firstFrameHeader"]["complete"] = False
    require_invalid(
        validator,
        mismatched_header_complete,
        "complete MP3 detail with incomplete first header",
    )

    incomplete_without_error = deepcopy(truncated)
    incomplete_without_error["diagnostics"] = []
    require_invalid(validator, incomplete_without_error, "incomplete MP3 without error")

    error_without_recovery = deepcopy(malformed)
    del error_without_recovery["diagnostics"][0]["recoveryOffset"]
    require_invalid(validator, error_without_recovery, "MP3 error without recovery")

    warning_with_recovery = deepcopy(free)
    warning_with_recovery["diagnostics"][0]["recoveryOffset"] = 30
    require_invalid(validator, warning_with_recovery, "MP3 warning with recovery offset")

    complete_with_error = deepcopy(indexed)
    complete_with_error["diagnostics"] = deepcopy(malformed["diagnostics"])
    require_invalid(validator, complete_with_error, "complete MP3 with error")

    extra_field = deepcopy(indexed)
    extra_field["detail"]["firstFrameHeader"]["mystery"] = 1
    require_invalid(validator, extra_field, "MP3 detail additional field")


def validate_avc_detail_edge_cases(validator: Draft202012Validator) -> None:
    sequence = load_json("testdata/golden/flv-avc-detail-v1.json")
    nalu = load_json("testdata/golden/flv-avc-detail-nalu-v1.json")
    malformed = load_json("testdata/golden/flv-avc-detail-malformed-v1.json")

    def partial_sps(*fields: str):
        instance = deepcopy(sequence)
        detail = instance["detail"]
        configuration = detail["avcDecoderConfigurationRecord"]
        parameter_set = configuration["sequenceParameterSets"][0]
        source_sps = parameter_set["sequenceParameterSet"]
        retained = {
            name: deepcopy(source_sps[name])
            for name in ("rbsp", *fields, "syntaxBits", "remainingRbspBits", "complete")
        }
        retained["complete"] = False
        parameter_set["sequenceParameterSet"] = retained
        parameter_set["summaryComplete"] = False
        configuration["complete"] = False
        detail["complete"] = False
        instance["diagnostics"] = [
            {
                "code": "AVC_RBSP_SYNTAX_TRUNCATED",
                "severity": "error",
                "offset": 76,
                "bitOffset": 0,
                "message": "synthetic truncated SPS syntax",
                "recoveryOffset": 76,
            }
        ]
        return instance

    require_valid(
        validator,
        partial_sps("profileIdc"),
        "SPS truncated after a High profile_idc",
    )
    high_profile_prefix = partial_sps(
        "profileIdc",
        "constraintSetFlags",
        "reservedZero2Bits",
        "levelIdc",
        "sequenceParameterSetId",
        "highProfile",
    )
    high_profile_prefix["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["highProfile"] = {}
    require_valid(
        validator,
        high_profile_prefix,
        "SPS truncated inside its High-profile prefix",
    )
    chroma_only = partial_sps(
        "profileIdc",
        "constraintSetFlags",
        "reservedZero2Bits",
        "levelIdc",
        "sequenceParameterSetId",
        "highProfile",
    )
    chroma_only["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["highProfile"] = {
        "chromaFormatIdc": deepcopy(
            sequence["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["highProfile"]["chromaFormatIdc"]
        )
    }
    chroma_only["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["highProfile"]["chromaFormatIdc"]["value"] = 3
    require_valid(
        validator,
        chroma_only,
        "SPS truncated after chroma_format_idc 3",
    )
    require_valid(
        validator,
        partial_sps(
            "profileIdc",
            "constraintSetFlags",
            "reservedZero2Bits",
            "levelIdc",
            "sequenceParameterSetId",
            "highProfile",
            "log2MaxFrameNumMinus4",
            "picOrderCntType",
        ),
        "SPS truncated after pic_order_cnt_type 0",
    )
    interlaced_prefix = partial_sps(
        "profileIdc",
        "constraintSetFlags",
        "reservedZero2Bits",
        "levelIdc",
        "sequenceParameterSetId",
        "highProfile",
        "log2MaxFrameNumMinus4",
        "picOrderCntType",
        "log2MaxPicOrderCntLsbMinus4",
        "maxNumRefFrames",
        "gapsInFrameNumValueAllowedFlag",
        "picWidthInMbsMinus1",
        "picHeightInMapUnitsMinus1",
        "frameMbsOnlyFlag",
    )
    interlaced_prefix["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["frameMbsOnlyFlag"]["value"] = False
    require_valid(
        validator,
        interlaced_prefix,
        "SPS truncated after frame_mbs_only_flag false",
    )
    partial_crop = partial_sps(
        "profileIdc",
        "constraintSetFlags",
        "reservedZero2Bits",
        "levelIdc",
        "sequenceParameterSetId",
        "highProfile",
        "log2MaxFrameNumMinus4",
        "picOrderCntType",
        "log2MaxPicOrderCntLsbMinus4",
        "maxNumRefFrames",
        "gapsInFrameNumValueAllowedFlag",
        "picWidthInMbsMinus1",
        "picHeightInMapUnitsMinus1",
        "frameMbsOnlyFlag",
        "direct8x8InferenceFlag",
        "frameCroppingFlag",
        "frameCropLeftOffset",
    )
    partial_crop["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["frameCroppingFlag"]["value"] = True
    require_valid(
        validator,
        partial_crop,
        "SPS truncated within frame-crop offsets",
    )

    wrong_identity = deepcopy(sequence)
    wrong_identity["documentType"] = "flv-aac-tag-detail"
    require_invalid(validator, wrong_identity, "AVC detail with AAC document identity")

    wrong_tag = deepcopy(sequence)
    wrong_tag["tag"]["typeCode"] = 8
    wrong_tag["tag"]["typeName"] = "audio"
    require_invalid(validator, wrong_tag, "AVC detail attached to audio tag")

    sequence_without_config = deepcopy(sequence)
    del sequence_without_config["detail"]["avcDecoderConfigurationRecord"]
    require_invalid(validator, sequence_without_config, "AVC sequence header without avcC")

    sequence_with_nalus = deepcopy(sequence)
    sequence_with_nalus["detail"]["nalUnitSequence"] = deepcopy(
        nalu["detail"]["nalUnitSequence"]
    )
    require_invalid(validator, sequence_with_nalus, "AVC sequence header with NALU sequence")

    nalu_with_config = deepcopy(nalu)
    nalu_with_config["detail"]["avcDecoderConfigurationRecord"] = deepcopy(
        sequence["detail"]["avcDecoderConfigurationRecord"]
    )
    require_invalid(validator, nalu_with_config, "AVC NALU packet with avcC")

    nalu_without_sequence = deepcopy(nalu)
    del nalu_without_sequence["detail"]["nalUnitSequence"]
    require_invalid(validator, nalu_without_sequence, "complete AVC NALU without boundaries")

    wrong_nal_type_name = deepcopy(nalu)
    wrong_nal_type_name["detail"]["nalUnitSequence"]["nalUnits"][0]["header"]["nalUnitType"]["name"] = "sei"
    require_invalid(validator, wrong_nal_type_name, "mismatched AVC NAL type name")

    invalid_length_size = deepcopy(nalu)
    invalid_length_size["detail"]["nalUnitSequence"]["lengthSize"] = 3
    require_invalid(validator, invalid_length_size, "reserved AVC NAL length width")

    missing_source_mapping = deepcopy(sequence)
    missing_source_mapping["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["profileIdc"]["span"]["sourceSpans"] = []
    require_invalid(validator, missing_source_mapping, "AVC RBSP field without source mapping")

    malformed_claims_complete = deepcopy(malformed)
    malformed_claims_complete["detail"]["complete"] = True
    require_invalid(validator, malformed_claims_complete, "malformed avcC marked complete")

    reserved_length_size = deepcopy(sequence)
    reserved_config = reserved_length_size["detail"]["avcDecoderConfigurationRecord"]
    reserved_config["lengthSizeMinusOne"]["value"] = 2
    reserved_config.pop("lengthSize")
    reserved_config["complete"] = False
    reserved_length_size["detail"]["complete"] = False
    reserved_length_size["diagnostics"] = [
        {
            "code": "AVC_LENGTH_SIZE_RESERVED",
            "severity": "error",
            "offset": 33,
            "bitOffset": 6,
            "message": "synthetic reserved lengthSizeMinusOne",
            "expected": 3,
            "actual": 2,
            "recoveryOffset": 76,
        }
    ]
    require_valid(validator, reserved_length_size, "reserved avcC NAL length width")

    reserved_with_derived_size = deepcopy(reserved_length_size)
    reserved_with_derived_size["detail"]["avcDecoderConfigurationRecord"]["lengthSize"] = 4
    require_invalid(validator, reserved_with_derived_size, "reserved avcC width with derived length size")

    complete_with_reserved_length_size = deepcopy(sequence)
    complete_with_reserved_length_size["detail"]["avcDecoderConfigurationRecord"][
        "lengthSizeMinusOne"
    ]["value"] = 2
    complete_with_reserved_length_size["detail"]["avcDecoderConfigurationRecord"].pop(
        "lengthSize"
    )
    require_invalid(
        validator,
        complete_with_reserved_length_size,
        "complete avcC with reserved NAL length width",
    )

    framing_complete_wrong_version = deepcopy(sequence)
    framing_complete_wrong_version["detail"]["avcDecoderConfigurationRecord"][
        "configurationVersion"
    ]["value"] = 2
    require_invalid(
        validator,
        framing_complete_wrong_version,
        "framing-complete avcC with configurationVersion other than 1",
    )

    for field in ("bitDepthLumaMinus8", "bitDepthChromaMinus8"):
        excessive_bit_depth = deepcopy(sequence)
        excessive_bit_depth["detail"]["avcDecoderConfigurationRecord"][
            "highProfileExtension"
        ][field]["value"] = 5
        require_invalid(
            validator,
            excessive_bit_depth,
            f"complete avcC with {field} above 4",
        )

    baseline_with_high_extension = deepcopy(sequence)
    baseline_with_high_extension["detail"]["avcDecoderConfigurationRecord"][
        "avcProfileIndication"
    ]["value"] = 66
    require_invalid(
        validator,
        baseline_with_high_extension,
        "Baseline avcC with a high-profile extension",
    )

    high_without_extension = deepcopy(sequence)
    del high_without_extension["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]
    require_invalid(
        validator,
        high_without_extension,
        "framing-complete High avcC without its extension",
    )

    high_extension_missing_field = deepcopy(sequence)
    del high_extension_missing_field["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]["bitDepthChromaMinus8"]
    require_invalid(
        validator,
        high_extension_missing_field,
        "framing-complete High avcC with an incomplete extension",
    )

    partial_high_extension = deepcopy(sequence)
    partial_config = partial_high_extension["detail"][
        "avcDecoderConfigurationRecord"
    ]
    partial_config["framingComplete"] = False
    partial_config["complete"] = False
    partial_high_extension["detail"]["complete"] = False
    partial_extension = partial_config["highProfileExtension"]
    for field in (
        "bitDepthLumaReserved",
        "bitDepthLumaMinus8",
        "bitDepthChromaReserved",
        "bitDepthChromaMinus8",
        "numSequenceParameterSetExt",
    ):
        del partial_extension[field]
    partial_high_extension["diagnostics"] = [
        {
            "code": "AVC_HIGH_PROFILE_EXTENSION_TRUNCATED",
            "severity": "error",
            "offset": 73,
            "bitOffset": 0,
            "message": "synthetic truncated high-profile extension",
            "expected": 1,
            "actual": 0,
            "recoveryOffset": 76,
        }
    ]
    require_valid(
        validator,
        partial_high_extension,
        "partially decoded High avcC extension",
    )

    empty_high_extension = deepcopy(partial_high_extension)
    empty_extension = empty_high_extension["detail"][
        "avcDecoderConfigurationRecord"
    ]["highProfileExtension"]
    del empty_extension["chromaFormatReserved"]
    del empty_extension["chromaFormat"]
    require_valid(
        validator,
        empty_high_extension,
        "High avcC extension truncated before its first byte",
    )

    full_extension = sequence["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]
    partial_luma_extension = deepcopy(partial_high_extension)
    partial_luma = partial_luma_extension["detail"][
        "avcDecoderConfigurationRecord"
    ]["highProfileExtension"]
    for field in ("bitDepthLumaReserved", "bitDepthLumaMinus8"):
        partial_luma[field] = deepcopy(full_extension[field])
    require_valid(
        validator,
        partial_luma_extension,
        "High avcC extension truncated after its luma-depth byte",
    )

    partial_chroma_depth_extension = deepcopy(partial_luma_extension)
    partial_chroma_depth = partial_chroma_depth_extension["detail"][
        "avcDecoderConfigurationRecord"
    ]["highProfileExtension"]
    for field in ("bitDepthChromaReserved", "bitDepthChromaMinus8"):
        partial_chroma_depth[field] = deepcopy(full_extension[field])
    require_valid(
        validator,
        partial_chroma_depth_extension,
        "High avcC extension truncated after its chroma-depth byte",
    )

    unpaired_chroma_format = deepcopy(partial_high_extension)
    del unpaired_chroma_format["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]["chromaFormat"]
    require_invalid(
        validator,
        unpaired_chroma_format,
        "High avcC extension with an unpaired chroma-format field",
    )

    luma_without_chroma = deepcopy(partial_luma_extension)
    luma_without_chroma_fields = luma_without_chroma["detail"][
        "avcDecoderConfigurationRecord"
    ]["highProfileExtension"]
    del luma_without_chroma_fields["chromaFormatReserved"]
    del luma_without_chroma_fields["chromaFormat"]
    require_invalid(
        validator,
        luma_without_chroma,
        "High avcC luma-depth fields without their syntax prefix",
    )

    chroma_depth_without_luma = deepcopy(partial_chroma_depth_extension)
    chroma_depth_without_luma_fields = chroma_depth_without_luma["detail"][
        "avcDecoderConfigurationRecord"
    ]["highProfileExtension"]
    del chroma_depth_without_luma_fields["bitDepthLumaReserved"]
    del chroma_depth_without_luma_fields["bitDepthLumaMinus8"]
    require_invalid(
        validator,
        chroma_depth_without_luma,
        "High avcC chroma-depth fields without their syntax prefix",
    )

    count_without_depth_fields = deepcopy(partial_high_extension)
    count_without_depth_fields["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]["numSequenceParameterSetExt"] = deepcopy(
        full_extension["numSequenceParameterSetExt"]
    )
    require_invalid(
        validator,
        count_without_depth_fields,
        "High avcC SPS-extension count without preceding depth fields",
    )

    entries_without_count = deepcopy(partial_chroma_depth_extension)
    entries_without_count["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]["sequenceParameterSetExts"] = [
        deepcopy(
            sequence["detail"]["avcDecoderConfigurationRecord"][
                "sequenceParameterSets"
            ][0]
        )
    ]
    require_invalid(
        validator,
        entries_without_count,
        "High avcC SPS-extension entry without its count",
    )

    zero_count_with_entries = deepcopy(sequence)
    zero_count_with_entries["detail"]["avcDecoderConfigurationRecord"][
        "highProfileExtension"
    ]["sequenceParameterSetExts"] = [
        deepcopy(
            sequence["detail"]["avcDecoderConfigurationRecord"][
                "sequenceParameterSets"
            ][0]
        )
    ]
    require_invalid(
        validator,
        zero_count_with_entries,
        "High avcC zero SPS-extension count with an entry",
    )

    extension_without_profile = deepcopy(partial_high_extension)
    del extension_without_profile["detail"]["avcDecoderConfigurationRecord"][
        "avcProfileIndication"
    ]
    require_invalid(
        validator,
        extension_without_profile,
        "high-profile extension without AVCProfileIndication",
    )

    warning_with_recovery = deepcopy(sequence)
    warning_with_recovery["diagnostics"] = [
        {
            "code": "AVC_FLV_FRAME_TYPE_UNEXPECTED",
            "severity": "warning",
            "offset": 24,
            "bitOffset": 0,
            "message": "synthetic warning",
            "actual": 3,
            "recoveryOffset": 76,
        }
    ]
    require_invalid(validator, warning_with_recovery, "AVC warning with recovery offset")

    warning = deepcopy(sequence)
    warning["detail"]["videoTagHeader"]["frameType"]["value"] = 3
    warning["detail"]["videoTagHeader"]["frameType"]["name"] = "disposableInterFrame"
    warning["diagnostics"] = [
        {
            "code": "AVC_FLV_FRAME_TYPE_UNEXPECTED",
            "severity": "warning",
            "offset": 24,
            "bitOffset": 0,
            "message": "synthetic warning",
            "actual": 3,
        }
    ]
    require_valid(validator, warning, "complete AVC with framing warning")

    no_context = deepcopy(nalu)
    del no_context["detail"]["nalUnitSequence"]
    no_context["detail"]["bitsConsumed"] = 40
    no_context["detail"]["complete"] = False
    no_context["diagnostics"] = [
        {
            "code": "AVC_NAL_LENGTH_SIZE_UNAVAILABLE",
            "severity": "error",
            "offset": 29,
            "bitOffset": 0,
            "message": "synthetic unavailable NAL length size",
            "recoveryOffset": 35,
        }
    ]
    require_valid(validator, no_context, "AVC NALU without stream context")

    header_missing = deepcopy(nalu)
    header_missing["detail"] = {
        "kind": "avcVideo",
        "span": deepcopy(nalu["detail"]["span"]),
        "bitsConsumed": 0,
        "complete": False,
    }
    header_missing["diagnostics"] = [
        {
            "code": "AVC_FLV_VIDEO_HEADER_TRUNCATED",
            "severity": "error",
            "offset": 24,
            "bitOffset": 0,
            "message": "synthetic missing video header",
            "expected": 1,
            "actual": 0,
            "recoveryOffset": 24,
        }
    ]
    require_valid(validator, header_missing, "empty AVC Video Tag payload")

    composition_missing = deepcopy(nalu)
    composition_missing["detail"].pop("compositionTime")
    composition_missing["detail"].pop("dataSpan")
    composition_missing["detail"].pop("nalUnitSequence")
    composition_missing["detail"]["bitsConsumed"] = 16
    composition_missing["detail"]["complete"] = False
    composition_missing["diagnostics"] = [
        {
            "code": "AVC_FLV_COMPOSITION_TIME_TRUNCATED",
            "severity": "error",
            "offset": 26,
            "bitOffset": 0,
            "message": "synthetic truncated composition time",
            "expected": 5,
            "actual": 2,
            "recoveryOffset": 26,
        }
    ]
    require_valid(validator, composition_missing, "AVC composition-time truncation")

    unknown_packet = deepcopy(nalu)
    unknown_packet["detail"]["packetType"]["value"] = 3
    unknown_packet["detail"]["packetType"]["name"] = "unknown"
    unknown_packet["detail"].pop("nalUnitSequence")
    unknown_packet["detail"]["bitsConsumed"] = 40
    unknown_packet["detail"]["complete"] = False
    unknown_packet["diagnostics"] = [
        {
            "code": "AVC_FLV_PACKET_TYPE_UNKNOWN",
            "severity": "error",
            "offset": 25,
            "bitOffset": 0,
            "message": "synthetic unknown packet type",
            "actual": 3,
            "recoveryOffset": 35,
        }
    ]
    require_valid(validator, unknown_packet, "unknown AVC packet type")

    end_of_sequence = deepcopy(nalu)
    end_of_sequence["detail"]["packetType"]["value"] = 2
    end_of_sequence["detail"]["packetType"]["name"] = "endOfSequence"
    end_of_sequence["detail"].pop("nalUnitSequence")
    end_of_sequence["detail"]["bitsConsumed"] = 40
    end_of_sequence["detail"]["endOfSequenceSpan"] = deepcopy(
        end_of_sequence["detail"]["dataSpan"]
    )
    end_of_sequence["detail"]["endOfSequenceSpan"]["bitLength"] = 0
    end_of_sequence["detail"]["dataSpan"]["bitLength"] = 0
    end_of_sequence["diagnostics"] = []
    require_valid(validator, end_of_sequence, "AVC end-of-sequence packet")

    eos_with_nalus = deepcopy(end_of_sequence)
    eos_with_nalus["detail"]["nalUnitSequence"] = deepcopy(
        nalu["detail"]["nalUnitSequence"]
    )
    require_invalid(validator, eos_with_nalus, "AVC end-of-sequence with NAL units")


def validate_tag_page_edge_cases(validator: Draft202012Validator) -> None:
    page = load_json("testdata/golden/flv-tag-page-v1.json")
    mutations = [
        (("schemaVersion",), 2),
        (("documentType",), "flv-scan-report"),
        (("page", "limit"), 0),
        (("page", "limit"), 4097),
        (("page", "start"), -1),
        (("page", "next"), 2**64),
        (("page", "total"), 1.5),
        (("page", "unexpected"), True),
        (("source", "path"), "private.flv"),
        (("tags",), None),
        (("tags",), page["tags"] * 4097),
        (("tags", 0, "headerSpan", "bitLength"), 87),
        (("tags", 0, "id"), -1),
    ]
    for path, value in mutations:
        changed = deepcopy(page)
        target = changed
        for key in path[:-1]:
            target = target[key]
        target[path[-1]] = value
        require_invalid(validator, changed, f"tag page mutation {path}")
    for key in page:
        missing = deepcopy(page)
        del missing[key]
        require_invalid(validator, missing, f"tag page missing {key}")


def validate_codec_detail_v2_edge_cases() -> None:
    for codec in ("avc", "mp3"):
        current = Draft202012Validator(load_json(f"docs/schemas/flv-{codec}-tag-detail-v2.schema.json"))
        legacy = Draft202012Validator(load_json(f"docs/schemas/flv-{codec}-tag-detail-v1.schema.json"))
        complete = load_json(f"testdata/golden/flv-{codec}-detail-v2.json")
        old = load_json(f"testdata/golden/flv-{codec}-detail-v1.json")
        require_invalid(current, old, f"{codec} v1 cannot enter v2 contract")
        require_invalid(legacy, complete, f"{codec} v2 cannot enter v1 contract")
        changed = deepcopy(complete)
        changed["schemaVersion"] = 1
        require_invalid(legacy, changed, f"{codec} deep fields cannot masquerade as v1")
        changed = deepcopy(complete)
        changed["detail"]["unpromisedField"] = True
        require_invalid(current, changed, f"{codec} v2 closed detail shape")

    avc = Draft202012Validator(load_json("docs/schemas/flv-avc-tag-detail-v2.schema.json"))
    sample = load_json("testdata/golden/flv-avc-detail-v2.json")
    def vui(document):
        return document["detail"]["avcDecoderConfigurationRecord"]["sequenceParameterSets"][0]["sequenceParameterSet"]["vui"]
    for field, value in (("trailingBitsValid", False), ("remainingRbspBits", 1), ("present", False)):
        changed = deepcopy(sample)
        vui(changed)[field] = value
        require_invalid(avc, changed, f"complete VUI contradiction: {field}")
    changed = deepcopy(sample)
    del vui(changed)["timeScale"]
    require_invalid(avc, changed, "complete timing VUI missing timeScale")
    changed = deepcopy(sample)
    vui(changed)["timingInfoPresentFlag"]["value"] = False
    require_invalid(avc, changed, "timing data without its presence flag")
    changed = deepcopy(sample)
    vui(changed)["numUnitsInTick"]["span"]["sourceSpans"][0]["bitOffset"] = 8
    require_invalid(avc, changed, "VUI mapped source bit outside byte")

    mp3 = Draft202012Validator(load_json("docs/schemas/flv-mp3-tag-detail-v2.schema.json"))
    sample = load_json("testdata/golden/flv-mp3-detail-v2.json")
    for field, value in (("maxFrames", 1025), ("bytesCovered", 1048577), ("stopReason", "guessed")):
        changed = deepcopy(sample)
        changed["detail"]["frames"][field] = value
        require_invalid(mp3, changed, f"MP3 frame inspection invalid {field}")
    changed = deepcopy(sample)
    changed["detail"]["frames"]["uninspectedSpan"]["bitLength"] = 8
    require_invalid(mp3, changed, "complete frames with uninspected remainder")
    changed = deepcopy(sample)
    changed["detail"]["frames"]["items"][0]["crc"]["status"] = "match"
    require_invalid(mp3, changed, "CRC match without stored/computed values")
    changed = deepcopy(sample)
    changed["detail"]["frames"]["items"][0]["crc"]["stored"] = 0
    require_invalid(mp3, changed, "unprotected frame with invented CRC value")
    changed = deepcopy(sample)
    changed["detail"]["frames"]["vbr"]["kind"] = "VBRI"
    require_invalid(mp3, changed, "Xing fields cannot masquerade as VBRI")
    changed = deepcopy(sample)
    del changed["detail"]["frames"]["vbr"]["flags"]
    require_invalid(mp3, changed, "complete Xing missing flags")


def validate_avc_v3_edge_cases() -> None:
    validator = Draft202012Validator(load_json("docs/schemas/flv-avc-tag-detail-v3.schema.json"))
    config = load_json("testdata/golden/flv-avc-detail-v3.json")
    nalu = load_json("testdata/golden/flv-avc-detail-nalu-v3.json")
    for version in (1, 2):
        old = Draft202012Validator(load_json(f"docs/schemas/flv-avc-tag-detail-v{version}.schema.json"))
        changed = deepcopy(nalu)
        changed['schemaVersion'] = version
        require_invalid(old, changed, f"AVC v3 fields cannot enter v{version}")
    for field in ('sliceHeaderPrefix',):
        changed = deepcopy(nalu)
        del changed['detail']['nalUnitSequence']['nalUnits'][0][field]
        require_invalid(validator, changed, 'complete slice without prefix')
    for field in ('frameNum', 'referencedPpsSpan', 'idrPicId'):
        changed = deepcopy(nalu)
        del changed['detail']['nalUnitSequence']['nalUnits'][0]['sliceHeaderPrefix'][field]
        require_invalid(validator, changed, 'complete slice without ' + field)
    for field in ('prefix',):
        changed = deepcopy(config)
        del changed['detail']['avcDecoderConfigurationRecord']['pictureParameterSets'][0]['pictureParameterSet'][field]
        require_invalid(validator, changed, 'complete PPS without prefix')
    changed = deepcopy(config)
    del changed['detail']['avcDecoderConfigurationRecord']['pictureParameterSets'][0]['pictureParameterSet']['prefix']['weightedBipredIdc']
    require_invalid(validator, changed, 'complete PPS prefix without prediction flag')
    for path, value in (
        (['detail', 'parameterContext', 'state'], 'guessed'),
        (['detail', 'parameterContext', 'ppsCount'], 257),
        (['detail', 'nalUnitSequence', 'nalUnits', 0, 'sliceHeaderPrefix', 'bytesRead'], 257),
        (['detail', 'nalUnitSequence', 'nalUnits', 0, 'sliceHeaderPrefix', 'unexpected'], True),
    ):
        changed = deepcopy(nalu)
        target = changed
        for part in path[:-1]:
            target = target[part]
        target[path[-1]] = value
        require_invalid(validator, changed, 'v3 bounds/closed shape: ' + str(path))


def validate_mp3_v3_edge_cases() -> None:
    validator = Draft202012Validator(load_json("docs/schemas/flv-mp3-tag-detail-v3.schema.json"))
    valid = load_json("testdata/golden/flv-mp3-side-info-v3.json")
    base = ["detail", "frames", "items", 0]
    mutations = (
        ([], "schemaVersion", 2),
        (base + ["sideInfo"], "bitsConsumed", 257),
        (base + ["sideInfo"], "declaredBytes", 31),
        (base + ["sideInfo"], "granules", []),
        (base + ["sideInfo"], "scfsiChannels", []),
        (base + ["sideInfo"], "extra", 1),
        (base + ["dependency"], "state", "decodable"),
        (base + ["dependency"], "knownPreviousBytes", -1),
        (base + ["dependency"], "missingPreviousBytes", 1),
        (base + ["sideInfo", "granules", 0, "channels", 0], "tableSelect", []),
        (base + ["sideInfo", "granules", 0, "channels", 0], "blockType", {"value": 2, "span": {"byteOffset": 0, "bitOffset": 0, "bitLength": 2}}),
    )
    for path, key, value in mutations:
        changed = deepcopy(valid)
        target = changed
        for part in path:
            target = target[part]
        target[key] = value
        require_invalid(validator, changed, "MP3 v3 closed shape/layout: " + str(path + [key]))
    for path, key in (
        (base, "sideInfo"), (base, "dependency"),
        (base + ["sideInfo"], "mainDataBegin"),
        (base + ["sideInfo", "granules", 0, "channels", 0], "preflag"),
        (base + ["sideInfo", "granules", 0, "channels", 0], "region0Count"),
    ):
        changed = deepcopy(valid)
        target = changed
        for part in path:
            target = target[part]
        del target[key]
        require_invalid(validator, changed, "MP3 v3 complete presence: " + str(path + [key]))
    changed = load_json("testdata/golden/flv-mp3-side-info-before-tag-v3.json")
    del changed["detail"]["frames"]["items"][0]["dependency"]["missingPreviousBytes"]
    require_invalid(validator, changed, "MP3 v3 missing Tag-external byte count")
    # A reported source value can be invalid syntax: schemas must retain it.
    changed = deepcopy(valid)
    side = changed["detail"]["frames"]["items"][0]["sideInfo"]
    side["complete"] = False
    side["granules"][0]["channels"][0]["bigValues"]["value"] = 511
    changed["detail"]["frames"]["items"][0]["dependency"]["state"] = "unknown"
    require_valid(validator, changed, "MP3 v3 preserves invalid decoded side-info values")
    # Encoded widths remain bounded even for malformed/partial syntax. Only
    # the semantic big_values/block_type restrictions depend on completeness.
    side_base = base + ["sideInfo"]
    channel_base = side_base + ["granules", 0, "channels", 0]
    for path, value in (
        (side_base + ["mainDataBegin", "value"], 512),
        (side_base + ["privateBits", "value"], 8),
        (channel_base + ["part23Length", "value"], 4096),
        (channel_base + ["bigValues", "value"], 512),
        (channel_base + ["globalGain", "value"], 256),
        (channel_base + ["scalefacCompress", "value"], 16),
        (channel_base + ["region0Count", "value"], 16),
        (channel_base + ["region1Count", "value"], 8),
        (channel_base + ["tableSelect", 0, "value"], 32),
        (channel_base + ["part23Length", "span", "bitLength"], 11),
        (side_base + ["scfsiChannels", 0, "flags", 0, "span", "bitLength"], 2),
    ):
        for complete in (False, True):
            changed = deepcopy(valid)
            changed["detail"]["frames"]["items"][0]["sideInfo"]["complete"] = complete
            target = changed
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            require_invalid(validator, changed, "MP3 v3 encoded width: " + str(path))
    changed = deepcopy(valid)
    changed["detail"]["frames"]["items"][0]["sideInfo"]["granules"][0]["channels"][0]["bigValues"]["value"] = 289
    require_invalid(validator, changed, "MP3 v3 complete big_values must not exceed 288")
    changed["detail"]["frames"]["items"][0]["sideInfo"]["complete"] = False
    require_valid(validator, changed, "MP3 v3 malformed big_values remains reportable")
    changed = deepcopy(valid)
    switched = changed["detail"]["frames"]["items"][1]["sideInfo"]
    switched["granules"][0]["channels"][0]["blockType"]["value"] = 0
    require_invalid(validator, changed, "MP3 v3 complete switched block_type cannot be zero")
    switched["complete"] = False
    require_valid(validator, changed, "MP3 v3 malformed switched block_type remains reportable")
    for field, maximum in (("blockType", 3), ("subblockGain", 7)):
        changed = deepcopy(valid)
        side = changed["detail"]["frames"]["items"][1]["sideInfo"]
        side["complete"] = False
        target = side["granules"][0]["channels"][0][field]
        if isinstance(target, list):
            target = target[0]
        target["value"] = maximum + 1
        require_invalid(validator, changed, "MP3 v3 switched field width: " + field)
    # Version-dependent widths apply even when malformed syntax is retained.
    for version, rate, name in ((2, 22050, "mpeg2"), (0, 11025, "mpeg2.5")):
        lsf = deepcopy(valid)
        frame = lsf["detail"]["frames"]["items"][0]
        header, side = frame["header"], frame["sideInfo"]
        header["version"].update(value=version, name=name)
        header.update(samplesPerFrame=576, samplingFrequencyHz=rate, bitrateKbps=80)
        side.update(declaredBytes=17, bitsConsumed=136, scfsiChannels=[])
        side["span"]["bitLength"] = 136
        side["mainDataBegin"]["span"]["bitLength"] = 8
        side["privateBits"]["value"] = 3
        side["privateBits"]["span"]["bitLength"] = 2
        side["granules"] = side["granules"][:1]
        for channel in side["granules"][0]["channels"]:
            del channel["preflag"]
            channel["scalefacCompress"]["span"]["bitLength"] = 9
        require_valid(validator, lsf, "MP3 v3 LSF field widths: " + name)
        for path, value in (
            (side_base + ["mainDataBegin", "value"], 256),
            (side_base + ["privateBits", "value"], 4),
            (channel_base + ["scalefacCompress", "value"], 512),
        ):
            changed = deepcopy(lsf)
            changed["detail"]["frames"]["items"][0]["sideInfo"]["complete"] = False
            target = changed
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            require_invalid(validator, changed, "MP3 v3 LSF encoded width: " + str(path))
    changed = load_json("testdata/golden/flv-mp3-side-info-before-tag-v3.json")
    dependency = changed["detail"]["frames"]["items"][0]["dependency"]
    dependency["state"] = "unknown"
    dependency["part23Bits"] = 16380
    require_valid(validator, changed, "MP3 v3 unknown dependency retains computable Tag-external gap")
    changed = deepcopy(valid)
    frame = changed["detail"]["frames"]["items"][0]
    del frame["sideInfo"]
    frame["boundaryComplete"] = False
    dependency = frame["dependency"]
    dependency["state"] = "unknown"
    del dependency["part23Bits"], dependency["availableBits"]
    require_valid(validator, changed, "MP3 v3 unread side information remains unknown")
    for state in ("none", "within_tag", "before_tag"):
        dependency["state"] = state
        require_invalid(validator, changed, "MP3 v3 cannot know dependency without side information")
    dependency["state"] = "unknown"
    dependency["part23Bits"] = 0
    require_invalid(validator, changed, "MP3 v3 cannot invent side information fields")
    for version in (1, 2):
        old = Draft202012Validator(load_json(f"docs/schemas/flv-mp3-tag-detail-v{version}.schema.json"))
        require_invalid(old, valid, f"MP3 v{version} rejects v3")


def main() -> None:
    manifest_schema = load_json("testdata/manifests/sample-manifest-v1.schema.json")
    Draft202012Validator.check_schema(manifest_schema)
    manifest_validator = Draft202012Validator(manifest_schema)
    seen_ids, seen_hashes = set(), set()
    for manifest_path in sorted((ROOT / "testdata/manifests").rglob("*.sample.json")):
        if manifest_path.name.startswith("example-"):
            continue
        relative = manifest_path.relative_to(ROOT).as_posix()
        manifest = load_json(relative)
        require_valid(manifest_validator, manifest, relative)
        if manifest_path.name != manifest["id"] + ".sample.json":
            raise SystemExit(f"{relative}: filename does not match sample ID")
        if manifest["id"] in seen_ids or manifest["file"]["sha256"] in seen_hashes:
            raise SystemExit(f"{relative}: duplicate corpus ID or SHA-256")
        seen_ids.add(manifest["id"])
        seen_hashes.add(manifest["file"]["sha256"])
    print(f"validated {len(seen_ids)} corpus manifest identities (media presence checked separately)")
    validate_codec_detail_v2_edge_cases()
    validate_avc_v3_edge_cases()
    validate_mp3_v3_edge_cases()
    print("validated MP3 v3 layouts, partial values, source fields, dependency states and version isolation")
    print("validated AVC v3 prefix presence, version isolation, closed fields and budgets")
    print("validated AVC/MP3 detail v2 version isolation, presence, bounds, CRC and VBR shapes")
    tag_page_validator = None
    detail_validator = None
    aac_detail_validator = None
    avc_detail_validator = None
    mp3_detail_validator = None
    media_summary_validator = None
    media_summary_v2_validator = None
    desktop_snapshot_validator = None
    for schema_path, instance_path in CONTRACTS:
        schema = load_json(schema_path)
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema)
        require_valid(validator, load_json(instance_path), instance_path)
        print(f"validated {instance_path} against {schema_path}")
        if schema_path == "docs/schemas/flv-tag-page-v1.schema.json":
            tag_page_validator = validator
        if schema_path == "docs/schemas/flv-tag-detail-v1.schema.json":
            detail_validator = validator
        if schema_path == "docs/schemas/flv-aac-tag-detail-v1.schema.json":
            aac_detail_validator = validator
        if schema_path == "docs/schemas/flv-avc-tag-detail-v1.schema.json":
            avc_detail_validator = validator
        if schema_path == "docs/schemas/flv-mp3-tag-detail-v1.schema.json":
            mp3_detail_validator = validator
        if schema_path == "docs/schemas/flv-media-summary-v1.schema.json":
            media_summary_validator = validator
        if schema_path == "docs/schemas/flv-media-summary-v2.schema.json":
            media_summary_v2_validator = validator
        if schema_path == "docs/schemas/desktop-diagnostic-snapshot-v1.schema.json":
            desktop_snapshot_validator = validator

    if tag_page_validator is None:
        raise SystemExit("tag page schema was not validated")
    validate_tag_page_edge_cases(tag_page_validator)
    print("validated tag-page identity, limits, closed fields, spans, and negative cases")
    if detail_validator is None:
        raise SystemExit("detail schema was not validated")
    validate_detail_edge_cases(detail_validator)
    print("validated tag-detail IEEE-754, UTF-8 replacement, and negative contract cases")
    if aac_detail_validator is None:
        raise SystemExit("AAC detail schema was not validated")
    validate_aac_detail_edge_cases(aac_detail_validator)
    print("validated AAC packet variants, recovery semantics, and negative contract cases")
    if avc_detail_validator is None:
        raise SystemExit("AVC detail schema was not validated")
    validate_avc_detail_edge_cases(avc_detail_validator)
    print("validated AVC packet variants, RBSP mappings, recovery semantics, and negative contract cases")
    if mp3_detail_validator is None:
        raise SystemExit("MP3 detail schema was not validated")
    validate_mp3_detail_edge_cases(mp3_detail_validator)
    print("validated MP3 identity, tag, completeness, bitrate modes, malformed, and recovery contract cases")
    if media_summary_validator is None:
        raise SystemExit("media summary schema was not validated")
    validate_media_summary_edge_cases(media_summary_validator)
    print("validated media-summary identity, bounds, completeness, and negative contract cases")
    if media_summary_v2_validator is None:
        raise SystemExit("media summary v2 schema was not validated")
    validate_media_summary_v2_edge_cases(media_summary_v2_validator)
    print("validated media-summary v2 version isolation, MP3 parameters, bitrate modes, retention, and recovery cases")
    if desktop_snapshot_validator is None:
        raise SystemExit("desktop snapshot schema was not validated")
    validate_desktop_snapshot_edge_cases(desktop_snapshot_validator)
    print("validated desktop snapshot identity, exact coordinates, bounded sections, omissions, and local observations")


if __name__ == "__main__":
    main()

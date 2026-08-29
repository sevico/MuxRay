#!/usr/bin/env python3
"""Validate public JSON Schemas and their committed contract examples."""

from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parent.parent
CONTRACTS = (
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


def main() -> None:
    detail_validator = None
    aac_detail_validator = None
    for schema_path, instance_path in CONTRACTS:
        schema = load_json(schema_path)
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema)
        require_valid(validator, load_json(instance_path), instance_path)
        print(f"validated {instance_path} against {schema_path}")
        if schema_path == "docs/schemas/flv-tag-detail-v1.schema.json":
            detail_validator = validator
        if schema_path == "docs/schemas/flv-aac-tag-detail-v1.schema.json":
            aac_detail_validator = validator

    if detail_validator is None:
        raise SystemExit("detail schema was not validated")
    validate_detail_edge_cases(detail_validator)
    print("validated tag-detail IEEE-754, UTF-8 replacement, and negative contract cases")
    if aac_detail_validator is None:
        raise SystemExit("AAC detail schema was not validated")
    validate_aac_detail_edge_cases(aac_detail_validator)
    print("validated AAC packet variants, recovery semantics, and negative contract cases")


if __name__ == "__main__":
    main()

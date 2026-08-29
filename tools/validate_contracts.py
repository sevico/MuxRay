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


def main() -> None:
    detail_validator = None
    for schema_path, instance_path in CONTRACTS:
        schema = load_json(schema_path)
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema)
        require_valid(validator, load_json(instance_path), instance_path)
        print(f"validated {instance_path} against {schema_path}")
        if schema_path == "docs/schemas/flv-tag-detail-v1.schema.json":
            detail_validator = validator

    if detail_validator is None:
        raise SystemExit("detail schema was not validated")
    validate_detail_edge_cases(detail_validator)
    print("validated tag-detail IEEE-754, UTF-8 replacement, and negative contract cases")


if __name__ == "__main__":
    main()

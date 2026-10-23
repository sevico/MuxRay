"""Collect bounded, offline production-component license evidence; does not edit the repository."""
from pathlib import Path
import hashlib
import json
import re
import argparse

if not __debug__:
    raise RuntimeError("License audit requires assertions; do not use Python -O or PYTHONOPTIMIZE")

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--audit-dir", type=Path, required=True)
args = parser.parse_args()

OUT = args.audit_dir.resolve()
CONFIG = json.loads((OUT / "audit-environment.json").read_text(encoding="utf-8-sig"))
REPO = Path(CONFIG["repoRoot"])
GOROOT = Path(CONFIG["goRoot"])
GO_VERSION = CONFIG["goVersion"]
MODCACHE = Path(CONFIG["modCache"])
FRONTEND = REPO / "desktop/frontend"
LICENSES = OUT / "licenses"
LICENSES.mkdir(exist_ok=False)

def digest(data):
    return hashlib.sha256(data).hexdigest()

def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))

def relative_origin(path):
    path = Path(path)
    for root, prefix in [(GOROOT, "GOROOT"), (MODCACHE, "GOMODCACHE"), (REPO, "REPO"), (OUT, "AUDIT")]:
        try:
            return prefix + "/" + path.relative_to(root).as_posix()
        except ValueError:
            pass
    raise ValueError(f"Unclassified source {path}")

records = []
local_sources = []

def component(key, name, version, license_id, basis, files, source_url=None, notes=None):
    entry = {"id": key, "name": name, "version": version, "licenseDeclaredOrObserved": license_id,
             "inclusionBasis": basis, "sourceURL": source_url, "noticeSources": [], "notes": notes or []}
    folder = LICENSES / key
    folder.mkdir(exist_ok=True)
    for index, item in enumerate(files):
        path, excerpt = item if isinstance(item, tuple) else (item, None)
        path = Path(path)
        raw = path.read_bytes()
        text = raw.decode("utf-8-sig")
        if excerpt:
            start, end = excerpt
            data = ("\n".join(text.splitlines()[start - 1:end]) + "\n").encode("utf-8")
            filename = f"{index + 1:02d}-{path.name}-lines-{start}-{end}.txt"
        else:
            data = raw
            filename = f"{index + 1:02d}-{path.name}"
        dest = folder / filename
        dest.write_bytes(data)
        entry["noticeSources"].append({"sourceFile": relative_origin(path), "sourceSHA256": digest(raw),
                                       "lineRange": list(excerpt) if excerpt else None,
                                       "copiedFile": dest.relative_to(OUT).as_posix(),
                                       "copiedSHA256": digest(data), "bytes": len(data)})
        local_sources.append({"component": key, "sourceFile": str(path), "sha256": digest(raw)})
    records.append(entry)
    return entry

production = read_json(OUT / "go-production-modules.json")
modules = [m for m in production["modules"] if m["Path"] not in {"github.com/sevico/muxray", "github.com/sevico/muxray/desktop"}]
binary = Path(CONFIG["binaryPath"])
binary_bytes = binary.read_bytes()
binary_metadata = (OUT / "go-production-binary-metadata.txt").read_text(encoding="utf-8-sig")
assert binary_metadata.splitlines()[0].endswith(GO_VERSION), "Toolchain configuration does not match binary build info"
linked = {match.group(1): match.group(2) for match in re.finditer(r"^\s*dep\s+(\S+)\s+(\S+)", binary_metadata, re.M)}
assert {m["Path"]: m["Version"] for m in modules} == {k: v for k, v in linked.items() if k not in {"github.com/sevico/muxray", "github.com/sevico/muxray/desktop"}}

for mod in sorted(modules, key=lambda m: m["Path"]):
    root = Path(mod["Dir"])
    files = sorted(f for f in root.iterdir() if f.is_file() and re.match(r"(?i)^(license|notice|copying|patents)([._-].*)?$", f.name))
    assert any(f.name.lower().startswith("license") for f in files), mod["Path"]
    main_text = next(f.read_text(encoding="utf-8-sig") for f in files if f.name.lower().startswith("license"))
    if "Unlicense OR MIT" in main_text:
        license_id = "Unlicense OR MIT"
    elif "Apache License" in main_text:
        license_id = "Apache-2.0"
    elif "Permission is hereby granted" in main_text:
        license_id = "MIT"
    elif "Neither the name" in main_text:
        license_id = "BSD-3-Clause"
    elif "Redistribution and use in source and binary forms" in main_text:
        license_id = "BSD-2-Clause"
    else:
        raise ValueError("Review license classification for " + mod["Path"])
    pkg_names = [p["importPath"] for p in production["packages"] if p["importPath"] == mod["Path"] or p["importPath"].startswith(mod["Path"] + "/")]
    component("go-" + re.sub(r"[^a-zA-Z0-9_.-]", "_", mod["Path"]), mod["Path"], mod["Version"], license_id,
              {"kind": "production-go-module", "binaryBuildInfo": True, "packages": pkg_names}, files,
              "https://" + mod["Path"], ["Module-level notice coverage; Go linker may remove unused functions."])

module_index = {m["Path"]: m for m in modules}
wails_module = module_index["github.com/wailsapp/wails/v2"]
webview_module = module_index["github.com/wailsapp/go-webview2"]
wails = Path(wails_module["Dir"])
webview = Path(webview_module["Dir"])
assert wails_module["Version"] == "v2.14.0" and webview_module["Version"] == "v1.0.22", "Review vendored notice policy after upgrading Wails/WebView2"
assert GO_VERSION == "go1.27.2", "Review standard-library source notices after upgrading the Go toolchain"
children = [
    ("wails-winc", "Wails vendored winc", "MIT", wails / "internal/frontend/desktop/windows/winc/LICENSE", "Windows window/control integration"),
    ("wails-file-dialog", "Wails vendored go-common-file-dialog", "MIT", wails / "internal/go-common-file-dialog/LICENSE", "Default Windows native file dialog"),
    ("wails-typescriptify", "Wails vendored typescriptify", "Apache-2.0", wails / "internal/typescriptify/LICENSE.txt", "Present in production Go package graph"),
    ("go-webview2-loader", "Go WebView2 loader / OpenWebView2Loader port", "ISC", webview / "webviewloader/LICENSE", "Default !native_webview2loader Go implementation"),
]
for key, name, lic, path, basis in children:
    component(key, name, wails_module["Version"] if key.startswith("wails-") else webview_module["Version"], lic, {"kind": "vendored-go-subcomponent", "reason": basis}, [path])

def block_comment(path):
    lines = Path(path).read_text(encoding="utf-8-sig").splitlines()
    start = next(i for i, s in enumerate(lines) if s.strip() == "/*")
    end = next(i for i in range(start + 1, len(lines)) if lines[i].strip() == "*/")
    return start + 2, end

for key, name, path, origin in [
    ("wails-ringqueue", "ringqueue by Erik Dubbelboer, vendored in Wails", wails / "pkg/assetserver/ringqueue.go", "https://github.com/erikdubbelboer/ringqueue"),
    ("go-webview2-guid", "go-ole adapted GUID code in go-webview2", webview / "pkg/edge/guid.go", "https://github.com/go-ole/go-ole"),
]:
    component(key, name, "vendored in pinned module", "MIT", {"kind": "source-header-notice"}, [(path, block_comment(path))], origin)

runtime_root = wails / "internal/frontend/runtime"
runtime_package = read_json(runtime_root / "package.json")
component("wails-js-runtime", "Wails embedded JavaScript desktop runtime", "Wails " + wails_module["Version"] + " / package " + runtime_package["version"], "MIT at module root; ISC in runtime package metadata",
          {"kind": "go-embedded-javascript", "assets": ["internal/frontend/runtime/ipc.js", "internal/frontend/runtime/runtime_prod_desktop.js"]},
          [wails / "LICENSE", (runtime_root / "desktop/main.js", (1, 10))],
          "https://github.com/wailsapp/wails", ["runtime/package.json declares ISC; no dedicated full ISC license file is included in that runtime directory. Root MIT text and source copyright are preserved; metadata discrepancy remains for publisher review."])

lock = read_json(FRONTEND / "package-lock.json")
bundle_modules = read_json(OUT / "frontend-bundle-modules.json")
frontend_assets = []
for chunk in bundle_modules:
    asset = FRONTEND / "dist" / chunk["fileName"]
    payload = asset.read_bytes()
    offset = binary_bytes.find(payload)
    assert offset >= 0, "Audited frontend chunk is not embedded in the audited EXE"
    frontend_assets.append({"file": "frontend/dist/" + chunk["fileName"], "sha256": digest(payload), "bytes": len(payload), "fullPayloadOffsetInAuditedEXE": offset})
rendered = [m for chunk in bundle_modules for m in chunk["modules"] if m["renderedLength"] > 0]
packages = sorted(set(re.search(r"/node_modules/((?:@[^/]+/)?[^/]+)", m["id"]).group(1) for m in rendered if "/node_modules/" in m["id"]))
# New rendered packages require a deliberate notice policy review.
assert packages == ["react", "react-dom", "scheduler"], "Review newly rendered frontend dependencies: " + repr(packages)
for name in packages:
    root = FRONTEND / "node_modules" / name
    package = read_json(root / "package.json")
    entry = lock["packages"]["node_modules/" + name]
    assert package["version"] == entry["version"]
    component("npm-" + name, name, package["version"], package["license"],
              {"kind": "rendered-production-frontend-module", "modules": [relative_origin(Path(m["id"])) for m in rendered if not m["id"].startswith("\0") and f"/node_modules/{name}/" in m["id"]]},
              [root / "LICENSE"], entry.get("resolved"))

vite = FRONTEND / "node_modules/vite"
vite_package = read_json(vite / "package.json")
vite_lines = (vite / "LICENSE.md").read_text().splitlines()
end = next(i for i, line in enumerate(vite_lines) if line.startswith("# Licenses of bundled dependencies"))
assert any(m["id"] == "\0vite/modulepreload-polyfill.js" for m in rendered)
component("vite-modulepreload", "Vite injected modulepreload polyfill", vite_package["version"], "MIT",
          {"kind": "injected-production-javascript", "module": "vite/modulepreload-polyfill.js"}, [(vite / "LICENSE.md", (1, end))],
          "https://github.com/vitejs/vite", ["Only the injected helper is attributed. The Vite CLI, server, compiler and unrelated bundled build dependencies are not shipped."])

component("go-runtime", "Go runtime and standard library", GO_VERSION, "BSD-3-Clause",
          {"kind": "linked-toolchain-runtime", "standardPackages": [p["importPath"] for p in production["packages"] if p["standard"] and not p["importPath"].startswith("vendor/")]},
          [GOROOT / "LICENSE"], "https://go.dev/", ["Source-header third-party notices are preserved separately below."])
vendor_versions = dict(re.findall(r"^# (golang.org/x/\S+) (\S+)", (GOROOT / "src/vendor/modules.txt").read_text(), re.M))
for name in ["crypto", "net", "sys", "text"]:
    path = "golang.org/x/" + name
    relevant = [p["importPath"] for p in production["packages"] if p["importPath"].startswith("vendor/" + path + "/")]
    if relevant:
        root = GOROOT / "src/vendor" / path
        component("go-stdlib-vendor-" + name, "Go standard-library vendored " + path, vendor_versions[path], "BSD-3-Clause",
                  {"kind": "toolchain-vendored-package", "packages": relevant}, [root / "LICENSE", root / "PATENTS"], "https://" + path)

# Preserve complete in-source permissions for selected standard-library files.
for name, key, label in [("acosh.go", "go-math-sun-1993", "Sun fdlibm notice (1993)"), ("exp.go", "go-math-sun-2004", "Sun fdlibm notice (2004)"), ("atan.go", "go-math-cephes", "Cephes / Stephen L. Moshier notice")]:
    path = GOROOT / "src/math" / name
    lines = path.read_text().splitlines()
    if "cephes" in key:
        start = next(i for i, line in enumerate(lines) if "// Cephes Math Library Release" in line)
        end = next(i for i in range(start, len(lines)) if "moshier@" in lines[i])
    else:
        start = next(i for i, line in enumerate(lines) if "Copyright (C)" in line)
        end = next(i for i in range(start, len(lines)) if "is preserved." in lines[i])
    component(key, label, "as distributed with " + GO_VERSION, "source-specific permissive notice", {"kind": "standard-library-source-notice"}, [(path, (start + 1, end + 1))])
for name, key in [("nistec/p256_ordinv.go", "go-fiat-nistec"), ("edwards25519/scalar.go", "go-fiat-edwards25519")]:
    path = GOROOT / "src/crypto/internal/fips140" / name
    lines = path.read_text().splitlines()
    start = next(i for i, line in enumerate(lines) if "Copyright" in line and "fiat-crypto" in line)
    end = next(i for i in range(start, len(lines)) if "SOFTWARE, EVEN IF ADVISED" in lines[i])
    component(key, "Fiat Cryptography notice in Go " + name, "as distributed with " + GO_VERSION, "BSD-1-Clause-like original text",
              {"kind": "standard-library-source-notice"}, [(path, (start + 1, end + 1))], "https://github.com/mit-plv/fiat-crypto")

# Unicode table attribution policy was reviewed for this exact dependency.
assert module_index["github.com/rivo/uniseg"]["Version"] == "v0.4.7", "Review Unicode generated-data version after upgrading uniseg"
unicode_path = OUT / "Unicode-LICENSE.txt"
missing = []
if unicode_path.exists():
    component("unicode-data", "Unicode data used by uniseg generated tables", "Unicode 15.0.0 data; supplied Unicode license text", "Unicode license (see full supplied text)",
              {"kind": "generated-data-notice", "sourceHeaders": [relative_origin(Path(module_index["github.com/rivo/uniseg"]["Dir"]) / name) for name in ["graphemeproperties.go", "wordproperties.go", "sentenceproperties.go", "lineproperties.go", "emojipresentation.go", "eastasianwidth.go"]]},
              [unicode_path], "https://www.unicode.org/license.txt")
else:
    raise ValueError("Missing reviewed Unicode license text")

native = []
for label, path in [("Microsoft WebView2 native loader x64 (not selected)", webview / "webviewloader/x64/WebView2Loader.dll"),
                    ("Microsoft WebView2 bootstrapper (not used by error strategy)", wails / "internal/webview2runtime/MicrosoftEdgeWebview2Setup.exe")]:
    payload = path.read_bytes()
    native.append({"name": label, "sourceFile": relative_origin(path), "bytes": len(payload), "sha256": digest(payload), "fullPayloadOffsetInAuditedEXE": binary_bytes.find(payload)})
assert all(r["fullPayloadOffsetInAuditedEXE"] == -1 for r in native)

findings = [{"component": "Wails embedded JS runtime", "kind": "license-metadata-discrepancy", "details": records[next(i for i, r in enumerate(records) if r['id'] == 'wails-js-runtime')]["notes"][0]}]
manifest = {"schemaVersion": 1, "scope": "Windows amd64 default production executable and embedded frontend; license evidence inventory, not a legal clearance opinion",
            "auditStatus": CONFIG["state"],
            "auditedBinary": {"file": "MuxRay.exe", "sha256": digest(binary_bytes), "bytes": len(binary_bytes), "goVersion": GO_VERSION, "buildTags": ["desktop", "wv2runtime.error", "production"]},
            "inputs": [{"file": relative_origin(REPO / f), "sha256": digest((REPO / f).read_bytes())} for f in ["go.mod", "desktop/go.mod", "desktop/go.sum", "desktop/frontend/package.json", "desktop/frontend/package-lock.json"]],
            "components": records, "frontendAssets": frontend_assets, "nativeExclusions": native,
            "excludedNotBundled": ["Project-owned MuxRay modules (covered by project LICENSE separately)", "Microsoft WebView2 Runtime (preinstalled prerequisite, not redistributed)", "Microsoft WebView2 native loader DLL (native_webview2loader tag absent)", "Microsoft WebView2 bootstrapper (wv2runtime.error strategy; complete payload absent)", "loose-envify and js-tokens (installed dependency tree but absent from rendered frontend module graph)", "TypeScript compiler/types, Vite CLI/dev-server, esbuild, Rollup and other build/test tooling (not redistributed as packages)", "Wails development Svelte/IPC overlay (production assets selected)"],
            "missingLicenseTexts": missing, "findingsForPublisherReview": findings,
            "finalBuildRecheck": "This inventory is bound to auditedBinary.sha256; regenerate it for every different executable."}
(OUT / "license-inventory.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
(OUT / "local-source-files.json").write_text(json.dumps(local_sources, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

notice = ["THIRD-PARTY NOTICES FOR MUXRAY WINDOWS DESKTOP", "", "This file preserves notices for third-party components selected for the Windows amd64 default production executable and its embedded frontend. MuxRay's own license is supplied separately.", "", "Microsoft WebView2 Runtime is a separately installed prerequisite; this package does not redistribute that runtime, its native loader DLL, or its bootstrapper. The default Go WebView2 loader is attributed below.", "", "Component and version selection is documented in license-inventory.json. These notices are an evidence-based attribution inventory, not a legal clearance or warranty.", ""]
for r in records:
    notice += ["=" * 78, r["name"] + " — " + r["version"], "Observed license: " + r["licenseDeclaredOrObserved"]]
    if r["sourceURL"]:
        notice.append("Source: " + r["sourceURL"])
    for note in r["notes"]:
        notice.append("Note: " + note)
    for f in r["noticeSources"]:
        notice += ["", "Notice from: " + f["sourceFile"], (LICENSES.parent / f["copiedFile"]).read_text(encoding="utf-8-sig").rstrip(), ""]
if missing:
    notice += ["=" * 78, "UNRESOLVED NOTICE COLLECTION", json.dumps(missing, ensure_ascii=False, indent=2)]
(OUT / "THIRD-PARTY-NOTICES.txt").write_text("\n".join(notice) + "\n", encoding="utf-8")
print(json.dumps({"components": len(records), "thirdPartyGoModules": len(modules), "frontendPackages": packages, "missingTexts": missing, "findingsForPublisherReview": findings, "noticeSHA256": digest((OUT / "THIRD-PARTY-NOTICES.txt").read_bytes())}, ensure_ascii=False, indent=2))

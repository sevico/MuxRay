"""Read-only, identity-fenced Windows MuxRay handle-type sampling.

snapshot writes one new JSON file. record writes a new directory containing
manifest.json, samples.jsonl and summary.json, until natural target exit or a
bounded duration. No UIA, process mutation, object-name query or GC request.
"""

import argparse
from collections import defaultdict
import ctypes as C
from ctypes import wintypes as W
import json
import math
import os
from pathlib import Path
import time
import uuid

import resource_probe as probe

MAX_LOG_BYTES = 256 << 20
LIMITATIONS = [
    "Only the exact target process is sampled; descendants require a separate resource probe.",
    "Handle, thread, GDI, USER and memory queries are not atomic; counts may differ during work.",
    "Object types are resolved using at most four temporary local duplicates per type per sample; no source handle is closed.",
    "No object names, allocation stacks, file contents or unrelated process handle tables are queried.",
    "GDI and USER objects are separate from kernel handles; sampled counts can miss short-lived objects.",
    "A complete bounded recording does not prove a leak or establish orderly source-file closure.",
]


class HandleEntry(C.Structure):
    _fields_ = [("value", C.c_size_t), ("handleCount", C.c_size_t), ("pointerCount", C.c_size_t),
                ("grantedAccess", W.ULONG), ("typeIndex", W.ULONG),
                ("attributes", W.ULONG), ("reserved", W.ULONG)]


class UnicodeString(C.Structure):
    _fields_ = [("length", W.USHORT), ("maximum", W.USHORT), ("buffer", C.c_void_p)]


class WindowsAPI(probe.WindowsAPI):
    def __init__(self):
        super().__init__()
        self.user = C.WinDLL("user32", use_last_error=True)
        self.user.GetGuiResources.argtypes, self.user.GetGuiResources.restype = [W.HANDLE, W.DWORD], W.DWORD
        self.nt = C.WinDLL("ntdll")
        self.nt.NtQueryInformationProcess.argtypes = [W.HANDLE, W.ULONG, C.c_void_p, W.ULONG, C.POINTER(W.ULONG)]
        self.nt.NtQueryInformationProcess.restype = W.LONG
        self.nt.NtQueryObject.argtypes = [W.HANDLE, W.ULONG, C.c_void_p, W.ULONG, C.POINTER(W.ULONG)]
        self.nt.NtQueryObject.restype = W.LONG
        self.kernel.DuplicateHandle.argtypes = [W.HANDLE, W.HANDLE, W.HANDLE, C.POINTER(W.HANDLE), W.DWORD, W.BOOL, W.DWORD]
        self.kernel.DuplicateHandle.restype = W.BOOL
        self.kernel.GetCurrentProcess.argtypes, self.kernel.GetCurrentProcess.restype = [], W.HANDLE

    def open(self, pid):
        if not 0 < pid <= 0xffffffff:
            raise ValueError("PID must fit a positive Windows DWORD")
        # Query, limited query, VM read (PSAPI), duplicate, synchronize only.
        handle = self.kernel.OpenProcess(0x400 | 0x1000 | 0x10 | 0x40 | 0x100000, False, pid)
        if not handle:
            raise self.error("OpenProcess for target handle snapshot")
        return handle

    def threads(self, pid):
        snapshot = self.kernel.CreateToolhelp32Snapshot(2, 0)
        if snapshot == C.c_void_p(-1).value:
            raise self.error("CreateToolhelp32Snapshot")
        try:
            entry = probe.PROCESSENTRY32W()
            entry.dwSize = C.sizeof(entry)
            okay = self.kernel.Process32FirstW(snapshot, C.byref(entry))
            while okay:
                if entry.th32ProcessID == pid:
                    return entry.cntThreads
                okay = self.kernel.Process32NextW(snapshot, C.byref(entry))
            if C.get_last_error() != 18:
                raise self.error("Process32 enumeration")
            raise ValueError("Target missing from process thread snapshot")
        finally:
            self.close(snapshot)

    def gui(self, handle):
        result = {}
        for name, flag in (("gdiObjects", 0), ("userObjects", 1)):
            C.set_last_error(0)
            count = self.user.GetGuiResources(handle, flag)
            if count == 0 and C.get_last_error():
                raise self.error("GetGuiResources " + name)
            result[name] = count
        return result

    def handles(self, handle):
        size, returned = 65536, W.ULONG()
        for _ in range(10):
            buffer = C.create_string_buffer(size)
            status = self.nt.NtQueryInformationProcess(handle, 51, buffer, size, C.byref(returned))
            if status == 0:
                break
            if status & 0xffffffff not in (0xc0000004, 0xc0000023):
                raise OSError(f"ProcessHandleInformation unavailable: NTSTATUS 0x{status & 0xffffffff:08x}")
            size = max(size * 2, returned.value + 4096)
            if size > 32 << 20:
                raise ValueError("Target handle snapshot exceeds 32 MiB bound")
        else:
            raise ValueError("Target handle snapshot changed too often")
        header_size = C.sizeof(C.c_size_t) * 2
        count = C.c_size_t.from_buffer(buffer).value
        if count > 262144 or header_size + count * C.sizeof(HandleEntry) > len(buffer):
            raise ValueError("Invalid or excessive target handle count")
        entries = (HandleEntry * count).from_buffer(buffer, header_size)
        grouped = defaultdict(list)
        for item in entries:
            grouped[item.typeIndex].append(item.value)
        rows = []
        for type_index, values in sorted(grouped.items()):
            name, failures = None, []
            for value in values[:4]:
                local = W.HANDLE()
                # DUPLICATE_SAME_ACCESS only; never DUPLICATE_CLOSE_SOURCE.
                if not self.kernel.DuplicateHandle(handle, value, self.kernel.GetCurrentProcess(), C.byref(local), 0, False, 2):
                    failures.append(f"DuplicateHandle failed: {C.get_last_error()}")
                    continue
                try:
                    data, used = C.create_string_buffer(4096), W.ULONG()
                    status = self.nt.NtQueryObject(local, 2, data, len(data), C.byref(used))
                    if status != 0:
                        failures.append(f"ObjectTypeInformation NTSTATUS 0x{status & 0xffffffff:08x}")
                        continue
                    value_text = UnicodeString.from_buffer(data)
                    start, end = C.addressof(data), C.addressof(data) + len(data)
                    if not value_text.buffer or not 0 < value_text.length <= 1024 or value_text.length % 2 or not (start <= value_text.buffer <= end - value_text.length):
                        raise ValueError("Invalid object type string range")
                    name = C.wstring_at(value_text.buffer, value_text.length // 2)
                    break
                finally:
                    self.close(local)
            rows.append({"typeIndex": type_index, "typeName": name, "count": len(values), "typeQueryFailures": failures})
        return {"handleTypes": rows, "snapshotHandleCount": count,
                "typeCountsSum": sum(row["count"] for row in rows),
                "handleValuesByTypeIndex": {str(k): values for k, values in grouped.items()}}


def check_identity(actual, expected):
    if actual["pid"] != expected["pid"] or actual["created"] != expected["created"] or not probe.same_image(actual["exe"], expected["exe"]):
        raise ValueError("Target PID/creation/executable identity mismatch")


def collect(api, handle, expected):
    result = {"status": "failed", "errors": [], "threadCount": None, "guiResources": None,
              "processMetrics": None, "handleTypes": None, "snapshotHandleCount": None}
    # This is the retained handle validated at attachment, never a PID reopen.
    # Windows may stop exposing the image path once that process has exited.
    life = api.lifecycle(handle)
    if life["exited"]:
        return {**result, "status": "exited", "lifecycle": life}
    try:
        actual = api.identity(handle, expected["pid"])
    except OSError as exc:
        # Exit can race the lifecycle check and image query. Only a confirmed
        # exit of the retained handle permits this terminal partial sample.
        life = api.lifecycle(handle)
        if life["exited"]:
            return {**result, "status": "exited_during_snapshot", "lifecycle": life,
                    "identityQueryError": f"{type(exc).__name__}: {exc}"}
        raise
    # An observed mismatch is always fatal, even if exit follows the query.
    check_identity(actual, expected)
    for field, operation in (("processMetrics", lambda: api.metrics(handle)),
                             ("threadCount", lambda: api.threads(expected["pid"])),
                             ("guiResources", lambda: api.gui(handle)),
                             ("handleData", lambda: api.handles(handle))):
        try:
            value = operation()
            if field == "handleData":
                result.update(value)
            else:
                result[field] = value
        except (OSError, ValueError) as exc:
            result["errors"].append(f"{field}: {type(exc).__name__}: {exc}")
    life = api.lifecycle(handle)
    result["lifecycle"] = life
    if life["exited"]:
        result["status"] = "exited_during_snapshot"
        return result
    metrics = result["processMetrics"]
    if not metrics or any(metrics.get(key) is None for key in ("handles", "privateBytes", "workingSetBytes")):
        result["errors"].append("Missing process metric(s)")
    if metrics and metrics.get("errors"):
        result["errors"].extend(metrics["errors"])
    if result["threadCount"] is None or result["guiResources"] is None:
        result["errors"].append("Missing thread or GUI metric(s)")
    if result["handleTypes"] is None:
        result["errors"].append("Missing handle types")
    else:
        for row in result["handleTypes"]:
            if not row["typeName"]:
                result["errors"].append(f"Unresolved object type index {row['typeIndex']} ({row['count']} handles)")
        if result["typeCountsSum"] != result["snapshotHandleCount"]:
            result["errors"].append("Handle type counts do not sum to the snapshot count")
    result["status"] = "passed" if not result["errors"] else "failed"
    return result


def record(args, api, clock=time.monotonic, sleep=time.sleep, utc=probe.utc_now):
    args.run_dir.mkdir(parents=True, exist_ok=False)
    started, start = utc(), clock()
    expected = {"pid": args.pid, "created": args.created, "exe": str(args.exe)}
    manifest = {"schemaVersion": 1, "runId": uuid.uuid4().hex, "startedUTC": started,
                "target": expected, "intervalSeconds": args.interval_seconds, "durationSeconds": args.duration_seconds,
                "maxAllowedGapSeconds": max(1.0, args.interval_seconds * 2), "inspectorPID": os.getpid(),
                "scriptSHA256": probe.sha256(Path(__file__)), "resourceProbeSHA256": probe.sha256(Path(probe.__file__)),
                "exeSHA256": probe.sha256(args.exe), "limitations": LIMITATIONS}
    probe.write_json(args.run_dir / "manifest.json", manifest)
    handle, count, active, failed, max_gap, max_duration, previous, error = None, 0, 0, 0, 0.0, 0.0, None, None
    first_sample, last_sample, max_lag, log_bytes = None, None, 0.0, 0
    stop, exit_info = "error", None
    try:
        handle, identity = probe.attach(api, args.pid, args.created, str(args.exe))
        check_identity(identity, expected)
        due = start
        with (args.run_dir / "samples.jsonl").open("xb") as stream:
            while True:
                now = clock()
                if now < due:
                    sleep(due - now)
                sample_start, observed = clock(), utc()
                elapsed = sample_start - start
                lag = max(0.0, sample_start - due)
                row = collect(api, handle, expected)
                completed = clock()
                duration = completed - sample_start
                max_duration = max(max_duration, duration)
                row.update(index=count, observedUTC=observed, completedUTC=utc(), elapsedSeconds=elapsed,
                           completedElapsedSeconds=completed - start, sampleDurationSeconds=duration, sampleLagSeconds=lag)
                encoded = probe.json_bytes(row)
                if log_bytes + len(encoded) > MAX_LOG_BYTES:
                    raise ValueError("Handle sample log exceeds 256 MiB bound")
                stream.write(encoded)
                stream.flush()
                if previous is not None:
                    max_gap = max(max_gap, sample_start - previous)
                previous = sample_start
                first_sample = elapsed if first_sample is None else first_sample
                last_sample = elapsed
                max_lag = max(max_lag, lag)
                log_bytes += len(encoded)
                count += 1
                if row["status"] in ("exited", "exited_during_snapshot"):
                    stop, exit_info = "target_exit", row["lifecycle"]
                    break
                active += 1
                failed += row["status"] != "passed"
                if completed - start >= args.duration_seconds:
                    stop = "duration_reached"
                    break
                # Keep a fixed cadence; do not issue catch-up bursts after delay.
                due = min(start + args.duration_seconds, max(due + args.interval_seconds, sample_start + args.interval_seconds, completed))
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"
    finally:
        if handle is not None:
            api.close(handle)
    finished, elapsed = utc(), clock() - start
    gap_limit = manifest["maxAllowedGapSeconds"]
    reasons = []
    if error:
        reasons.append(error)
    if active == 0:
        reasons.append("No completed live samples")
    if failed:
        reasons.append(f"{failed} live sample(s) have missing data or query errors")
    if max_gap > gap_limit or (first_sample is not None and first_sample > gap_limit) or max_duration > gap_limit:
        reasons.append("Sampling gap, initial attachment delay or sample duration exceeds allowed bound")
    summary = {"schemaVersion": 1, "runId": manifest["runId"], "target": expected,
               "startedUTC": started, "finishedUTC": finished, "elapsedSeconds": elapsed, "stopReason": stop,
               "complete": not reasons, "incompleteReasons": reasons, "error": error,
               "samples": count, "liveSamples": active, "failedLiveSamples": failed,
               "firstSampleElapsedSeconds": first_sample, "lastSampleElapsedSeconds": last_sample,
               "maxSampleGapSeconds": max_gap, "maxSampleDurationSeconds": max_duration, "maxSampleLagSeconds": max_lag,
               "sampleLogBytes": log_bytes, "targetExitConfirmed": exit_info is not None, "exit": exit_info,
               "limitations": LIMITATIONS}
    probe.write_json(args.run_dir / "summary.json", summary)
    return summary


def positive_seconds(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError("Use finite positive seconds")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("snapshot", "record"):
        command = commands.add_parser(name)
        command.add_argument("--pid", type=int, required=True)
        command.add_argument("--created", type=probe.positive_decimal, required=True)
        command.add_argument("--exe", type=Path, required=True)
        if name == "snapshot":
            command.add_argument("--output", type=Path, required=True)
        else:
            command.add_argument("--run-dir", type=Path, required=True)
            command.add_argument("--interval-seconds", type=positive_seconds, default=5.0)
            command.add_argument("--duration-seconds", type=positive_seconds, default=3600.0)
    args = parser.parse_args()
    if os.name != "nt" or not args.exe.is_absolute() or args.exe.name.lower() != "muxray.exe":
        parser.error("Use Windows and the exact absolute MuxRay.exe path")
    output = args.output if args.command == "snapshot" else args.run_dir
    if not output.is_absolute() or output.exists():
        parser.error("Use a new absolute output path")
    if args.command == "record" and (args.interval_seconds < 1 or args.duration_seconds > 86400):
        parser.error("Interval must be at least 1 second; duration is bounded to 86400 seconds")
    api = WindowsAPI()
    if args.command == "record":
        result = record(args, api)
        print(json.dumps(result, ensure_ascii=False))
        return 0 if result["complete"] else 1
    handle, identity = probe.attach(api, args.pid, args.created, str(args.exe))
    try:
        result = collect(api, handle, identity)
        result.update(observedUTC=probe.utc_now(), target=identity, inspectorPID=os.getpid(),
                      scriptSHA256=probe.sha256(Path(__file__)), exeSHA256=probe.sha256(args.exe), limitations=LIMITATIONS)
    finally:
        api.close(handle)
    probe.write_json(args.output, result)
    print(json.dumps({key: result[key] for key in ("status", "target", "threadCount", "handleTypes", "errors")}, ensure_ascii=False))
    return 0 if result["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())

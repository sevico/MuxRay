"""Read-only Windows process resource evidence for native desktop acceptance.

identity prints an exact PID/creation FILETIME/image identity. record requires
that identity and a new absolute output directory. mark/stop send requests to
the sampler, never to the observed application. No GUI or process termination
API is used. CPU is reported with one logical core equal to 100 percent.
"""

import argparse
import ctypes
from ctypes import wintypes
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import sys
import time
import uuid

MAX_IDENTITIES = 4096
MAX_LIVE = 128
MAX_MARKS = 1024
MAX_LOG_BYTES = 256 << 20
MAX_TIMING_LOG_BYTES = 64 << 20
LIMITATIONS = [
    "Private bytes measure private committed memory, not physical RAM.",
    "Summed working sets can count shared pages more than once.",
    "Sampled maxima can miss peaks and short-lived processes between snapshots.",
    "Only observed descendants of the retained process identity are attributed; shared or unrelated WebView2 processes are excluded.",
    "GPU allocations, system file caches and resource use before attachment are not measured.",
    "Stage names are operator labels, not inferred GUI events; requested and observed timestamps include dispatch delay.",
    "CPU intervals ending at a stage transition can include work from the preceding stage.",
    "OS process exit does not establish orderly source-file closure.",
    "Timing phases measure elapsed wall time, including possible descheduling; they do not prove exclusive OS-call execution time.",
]


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8")


def write_json(path, value, replace=False):
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        with temporary.open("xb") as stream:
            stream.write(json_bytes(value))
        if replace:
            # Windows readers may briefly omit FILE_SHARE_DELETE while reading
            # status.json. Retry that sharing race, never delete the old file.
            deadline = time.monotonic() + .5
            while True:
                try:
                    os.replace(temporary, path)
                    break
                except OSError as error:
                    if getattr(error, "winerror", None) not in (5, 32, 33) or time.monotonic() >= deadline:
                        raise
                    time.sleep(.01)
        else:
            # Windows rename refuses an existing destination. On other hosts
            # this helper is only used by isolated tests, not by record.
            if path.exists():
                raise FileExistsError(path)
            temporary.rename(path)
    finally:
        temporary.unlink(missing_ok=True)


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def same_image(left, right):
    return os.path.normcase(os.path.realpath(left)) == os.path.normcase(os.path.realpath(right))


def positive_decimal(value):
    if not value.isascii() or not value.isdecimal() or value.startswith("0") or int(value) > (1 << 64) - 1:
        raise argparse.ArgumentTypeError("Use a canonical positive uint64 decimal string")
    return value


class FILETIME(ctypes.Structure):
    _fields_ = [("low", wintypes.DWORD), ("high", wintypes.DWORD)]

    def integer(self):
        return self.low | self.high << 32


class PROCESSENTRY32W(ctypes.Structure):
    _fields_ = [("dwSize", wintypes.DWORD), ("cntUsage", wintypes.DWORD),
                ("th32ProcessID", wintypes.DWORD), ("th32DefaultHeapID", ctypes.c_size_t),
                ("th32ModuleID", wintypes.DWORD), ("cntThreads", wintypes.DWORD),
                ("th32ParentProcessID", wintypes.DWORD), ("pcPriClassBase", wintypes.LONG),
                ("dwFlags", wintypes.DWORD), ("szExeFile", wintypes.WCHAR * 260)]


class MEMORY_COUNTERS(ctypes.Structure):
    _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD)] + [
        (name, ctypes.c_size_t) for name in (
            "PeakWorkingSetSize", "WorkingSetSize", "QuotaPeakPagedPoolUsage", "QuotaPagedPoolUsage",
            "QuotaPeakNonPagedPoolUsage", "QuotaNonPagedPoolUsage", "PagefileUsage", "PeakPagefileUsage", "PrivateUsage")]


class WindowsAPI:
    def __init__(self):
        if os.name != "nt":
            raise RuntimeError("Live resource sampling requires Windows")
        self.kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        signatures = {
            "OpenProcess": ([wintypes.DWORD, wintypes.BOOL, wintypes.DWORD], wintypes.HANDLE),
            "CloseHandle": ([wintypes.HANDLE], wintypes.BOOL),
            "GetProcessTimes": ([wintypes.HANDLE] + [ctypes.POINTER(FILETIME)] * 4, wintypes.BOOL),
            "QueryFullProcessImageNameW": ([wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)], wintypes.BOOL),
            "WaitForSingleObject": ([wintypes.HANDLE, wintypes.DWORD], wintypes.DWORD),
            "GetExitCodeProcess": ([wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)], wintypes.BOOL),
            "GetProcessHandleCount": ([wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)], wintypes.BOOL),
            "K32GetProcessMemoryInfo": ([wintypes.HANDLE, ctypes.POINTER(MEMORY_COUNTERS), wintypes.DWORD], wintypes.BOOL),
            "CreateToolhelp32Snapshot": ([wintypes.DWORD, wintypes.DWORD], wintypes.HANDLE),
            "Process32FirstW": ([wintypes.HANDLE, ctypes.POINTER(PROCESSENTRY32W)], wintypes.BOOL),
            "Process32NextW": ([wintypes.HANDLE, ctypes.POINTER(PROCESSENTRY32W)], wintypes.BOOL),
        }
        for name, (arguments, result) in signatures.items():
            function = getattr(self.kernel, name)
            function.argtypes, function.restype = arguments, result

    @staticmethod
    def error(operation):
        code = ctypes.get_last_error()
        return OSError(code, operation + ": " + ctypes.FormatError(code).strip())

    def open(self, pid):
        # Limited query, VM read (PSAPI), synchronize. Never request mutation.
        if not 0 < pid <= 0xffffffff:
            raise ValueError("PID must fit a positive Windows DWORD")
        handle = self.kernel.OpenProcess(0x1000 | 0x0010 | 0x00100000, False, pid)
        if not handle:
            raise self.error("OpenProcess")
        return handle

    def close(self, handle):
        if handle:
            self.kernel.CloseHandle(handle)

    def times(self, handle):
        created, exited, kernel, user = FILETIME(), FILETIME(), FILETIME(), FILETIME()
        if not self.kernel.GetProcessTimes(handle, ctypes.byref(created), ctypes.byref(exited), ctypes.byref(kernel), ctypes.byref(user)):
            raise self.error("GetProcessTimes")
        return {"created": str(created.integer()), "exitFileTime": str(exited.integer()) if exited.integer() else None,
                "kernel100ns": str(kernel.integer()), "user100ns": str(user.integer())}

    def identity(self, handle, pid):
        size = wintypes.DWORD(32768)
        image = ctypes.create_unicode_buffer(size.value)
        if not self.kernel.QueryFullProcessImageNameW(handle, 0, image, ctypes.byref(size)):
            raise self.error("QueryFullProcessImageName")
        return {"pid": pid, "created": self.times(handle)["created"], "exe": image.value}

    def lifecycle(self, handle):
        waited = self.kernel.WaitForSingleObject(handle, 0)
        if waited not in (0, 258):
            raise self.error("WaitForSingleObject")
        result = self.times(handle)
        result["exited"] = waited == 0
        result["exitCode"] = None
        if result["exited"]:
            code = wintypes.DWORD()
            if not self.kernel.GetExitCodeProcess(handle, ctypes.byref(code)):
                raise self.error("GetExitCodeProcess")
            result["exitCode"] = code.value
        return result

    def metrics(self, handle):
        result = self.lifecycle(handle)
        result.update(privateBytes=None, workingSetBytes=None, handles=None, errors=[])
        if result["exited"]:
            return result
        counters = MEMORY_COUNTERS()
        counters.cb = ctypes.sizeof(counters)
        if self.kernel.K32GetProcessMemoryInfo(handle, ctypes.byref(counters), counters.cb):
            result.update(privateBytes=counters.PrivateUsage, workingSetBytes=counters.WorkingSetSize)
        else:
            result["errors"].append(str(self.error("GetProcessMemoryInfo")))
        count = wintypes.DWORD()
        if self.kernel.GetProcessHandleCount(handle, ctypes.byref(count)):
            result["handles"] = count.value
        else:
            result["errors"].append(str(self.error("GetProcessHandleCount")))
        return result

    def processes(self):
        handle = self.kernel.CreateToolhelp32Snapshot(2, 0)
        if handle == ctypes.c_void_p(-1).value:
            raise self.error("CreateToolhelp32Snapshot")
        try:
            entry = PROCESSENTRY32W()
            entry.dwSize = ctypes.sizeof(entry)
            result = []
            okay = self.kernel.Process32FirstW(handle, ctypes.byref(entry))
            while okay:
                result.append((entry.th32ProcessID, entry.th32ParentProcessID))
                okay = self.kernel.Process32NextW(handle, ctypes.byref(entry))
            if ctypes.get_last_error() != 18:  # ERROR_NO_MORE_FILES
                raise self.error("Process32 enumeration")
            return result
        finally:
            self.close(handle)


def attach(api, pid, created, exe):
    handle = api.open(pid)
    try:
        identity = api.identity(handle, pid)
        if identity["created"] != created or not same_image(identity["exe"], exe):
            raise ValueError("Process creation identity or executable differs; refusing PID reuse")
        if api.lifecycle(handle)["exited"]:
            raise ValueError("The requested process has already exited")
        return handle, identity
    except BaseException:
        api.close(handle)
        raise


@dataclass
class Tracked:
    identity: dict
    handle: object
    parent: str | None
    group: str
    exit_info: dict | None = None
    last_cpu: int | None = None
    last_time: float | None = None

    @property
    def key(self):
        return f'{self.identity["pid"]}:{self.identity["created"]}'


def parent_matches(parent, created):
    return int(created) >= int(parent.identity["created"]) and (
        parent.exit_info is None or int(created) <= int(parent.exit_info["exitFileTime"]))


class Tracker:
    def __init__(self, api, handle, identity, cores=None):
        self.api = api
        root = Tracked(identity, handle, None, "root")
        self.root = root
        self.known = {root.key: root}
        self.cores = cores or os.cpu_count() or 1
        self.discovery_errors = []
        self.discovery_failures = 0

    def discover(self):
        self.discovery_errors = []
        try:
            table = self.api.processes()
        except OSError as error:
            self.discovery_errors.append({"operation": "enumerate", "error": str(error)})
            self.discovery_failures += 1
            return
        active_pids = {p.identity["pid"] for p in self.known.values() if p.handle is not None}
        attempted = set()
        # New grandchildren can be earlier in the OS snapshot than parents.
        while True:
            added = False
            for pid, parent_pid in table:
                if pid in active_pids or pid in attempted:
                    continue
                parents = [p for p in self.known.values() if p.identity["pid"] == parent_pid]
                if not parents:
                    continue
                attempted.add(pid)
                handle = None
                try:
                    handle = self.api.open(pid)
                    identity = self.api.identity(handle, pid)
                    matched = None
                    for parent in parents:
                        if parent.handle is not None:
                            life = self.api.lifecycle(parent.handle)
                            if life["exited"]:
                                parent.exit_info = life
                        if parent_matches(parent, identity["created"]):
                            matched = parent
                            break
                    if matched is None:
                        continue
                    if len(self.known) >= MAX_IDENTITIES or len(active_pids) >= MAX_LIVE:
                        raise RuntimeError("Tracked-process budget exceeded")
                    group = "webview2" if Path(identity["exe"]).name.lower() == "msedgewebview2.exe" else "otherDescendants"
                    process = Tracked(identity, handle, matched.key, group)
                    if process.key in self.known:
                        continue
                    self.known[process.key] = process
                    active_pids.add(pid)
                    handle = None
                    added = True
                except OSError as error:
                    self.discovery_errors.append({"pid": pid, "parentPid": parent_pid, "error": str(error)})
                    self.discovery_failures += 1
                finally:
                    self.api.close(handle)
            if not added:
                break

    def sample(self, now):
        rows = []
        for process in self.known.values():
            if process.handle is None:
                continue
            row = {**process.identity, "identity": process.key, "parentIdentity": process.parent, "group": process.group,
                   "exited": None, "privateBytes": None, "workingSetBytes": None, "handles": None,
                   "cpuPercent": None, "cpuPercentOfMachine": None, "cpuDelta100ns": None, "errors": []}
            try:
                row.update(self.api.metrics(process.handle))
                cpu = int(row["kernel100ns"]) + int(row["user100ns"])
                row["cpuTime100ns"] = str(cpu)
                if process.last_cpu is not None and now > process.last_time:
                    delta = cpu - process.last_cpu
                    if delta < 0:
                        row["errors"].append("CPU counter regressed")
                    else:
                        row["cpuDelta100ns"] = str(delta)
                        row["cpuPercent"] = round(delta / 100000 / (now - process.last_time), 4)
                        row["cpuPercentOfMachine"] = round(row["cpuPercent"] / self.cores, 4)
                process.last_cpu, process.last_time = cpu, now
                if row["exited"]:
                    process.exit_info = {key: row[key] for key in ("exitFileTime", "exitCode", "kernel100ns", "user100ns", "exited")}
                    self.api.close(process.handle)
                    process.handle = None
            except OSError as error:
                row["errors"].append(str(error))
            rows.append(row)
        return rows

    def inventory(self):
        return [{**p.identity, "identity": p.key, "parentIdentity": p.parent, "group": p.group,
                 "exit": p.exit_info, "exitConfirmed": p.handle is None and p.exit_info is not None} for p in self.known.values()]

    def close(self):
        for process in self.known.values():
            self.api.close(process.handle)
            process.handle = None


def aggregate(rows, group=None):
    rows = [row for row in rows if group is None or row["group"] == group]
    live = [row for row in rows if row["exited"] is not True]
    result = {"liveProcesses": len(live), "observedProcesses": len(rows)}
    for name in ("privateBytes", "workingSetBytes", "handles"):
        values = [row[name] for row in live]
        result[name] = sum(values) if all(value is not None for value in values) else None
    for name in ("cpuPercent", "cpuPercentOfMachine"):
        values = [row[name] for row in rows]
        result[name] = round(sum(values), 4) if all(value is not None for value in values) else None
    result["completeMetrics"] = all(result[name] is not None for name in ("privateBytes", "workingSetBytes", "handles", "cpuPercent"))
    return result


def update_peaks(peaks, groups):
    for group, values in groups.items():
        target = peaks.setdefault(group, {"samples": 0, "incompleteSamples": 0, "max": {}})
        target["samples"] += 1
        target["incompleteSamples"] += not values["completeMetrics"]
        for key, value in values.items():
            if key not in ("completeMetrics", "observedProcesses") and value is not None:
                target["max"][key] = max(target["max"].get(key, value), value)


def consume_controls(directory, run_id, stage, sample_index, elapsed):
    acknowledgments = []
    stop = False
    # Each request has its own file. Publication by rename prevents partial JSON.
    requests = sorted((directory / "control").glob("*.request.json"))
    if len(requests) > MAX_MARKS:
        raise RuntimeError("Control-request budget exceeded")
    for path in requests:
        acknowledgment = path.with_name(path.name.replace(".request.json", ".ack.json"))
        if acknowledgment.exists():
            continue
        if path.stat().st_size > 8192:
            raise ValueError("Oversized control request")
        request = json.loads(path.read_text(encoding="utf-8"))
        if request.get("runId") != run_id or request.get("action") not in ("mark", "stop"):
            raise ValueError("Control request does not belong to this sampler")
        if request["action"] == "mark":
            value = request.get("stage")
            if not isinstance(value, str) or not value.strip() or len(value) > 128:
                raise ValueError("Invalid stage label")
            stage = value
        else:
            stop = True
        ack = {**request, "observedUTC": utc_now(), "observedElapsedSeconds": round(elapsed, 6),
               "appliesFromSample": sample_index, "stage": stage}
        write_json(acknowledgment, ack)
        acknowledgments.append(ack)
    return stage, stop, acknowledgments


def record(args, api=None):
    api = api or WindowsAPI()
    if not args.exe.is_absolute() or not args.output_dir.is_absolute():
        raise ValueError("Use absolute executable and new output-directory paths")
    handle, identity = attach(api, args.pid, args.created, args.exe)
    tracker = Tracker(api, handle, identity)
    try:
        args.output_dir.mkdir(parents=False, exist_ok=False)
        (args.output_dir / "control").mkdir()
        own_handle = api.open(os.getpid())
        try:
            sampler = api.identity(own_handle, os.getpid())
        finally:
            api.close(own_handle)
        manifest = {"schemaVersion": 1, "runId": uuid.uuid4().hex, "target": identity, "sampler": sampler,
                    "startedUTC": utc_now(), "exeSHA256": sha256(args.exe), "toolSHA256": sha256(Path(__file__)),
                    "intervalMs": args.interval_ms, "treeRefreshMs": 1000, "durationSeconds": args.duration_seconds,
                    "exitGraceSeconds": args.exit_grace_seconds, "logicalProcessors": tracker.cores,
                    "limits": {"liveProcesses": MAX_LIVE, "totalIdentities": MAX_IDENTITIES, "controlRequests": MAX_MARKS,
                               "sampleLogBytes": MAX_LOG_BYTES, "timingLogBytes": MAX_TIMING_LOG_BYTES},
                    "timingDiagnostics": {"file": "timings.jsonl", "clock": "time.monotonic seconds",
                                          "iterationEnd": "After status writing, before diagnostic-log writing and scheduled sleep",
                                          "sampleLogWriteIncludes": "JSON encoding, write and flush",
                                          "controlIncludes": "Request scan, acknowledgments and marks log flush",
                                          "previousIteration": "Diagnostic-log write and scheduled sleep from the preceding iteration; final values are in summary.json"},
                    "limitations": LIMITATIONS}
        write_json(args.output_dir / "manifest.json", manifest)
        started = time.monotonic()
        deadline = started + args.duration_seconds
        stage, index, next_discovery, next_sample = "unmarked", 0, started, started
        root_exit_observed = None
        peaks, stages, marks = {}, {}, []
        log_bytes, reason, failure = 0, "duration_limit", None
        max_lag, incomplete_discovery_samples = 0.0, 0
        status_failures, last_status_error = 0, None
        timing_log_bytes, timing_records = 0, 0
        previous_timing_write, previous_sleep = None, None
        last_iteration_finished = None
        try:
            with (args.output_dir / "samples.jsonl").open("xb") as samples, (args.output_dir / "marks.jsonl").open("xb") as marker_log, (args.output_dir / "timings.jsonl").open("xb") as timing_log:
                while True:
                    now = time.monotonic()
                    timing = {"index": index, "iterationStartedUTC": utc_now(),
                              "iterationStartedMonotonicSeconds": round(now, 6),
                              "iterationStartedElapsedSeconds": round(now - started, 6),
                              "scheduledMonotonicSeconds": round(next_sample, 6),
                              "previousTimingLogWriteSeconds": previous_timing_write,
                              "previousSleep": previous_sleep}
                    phase_started = time.monotonic()
                    stage, stop, acknowledgments = consume_controls(args.output_dir, manifest["runId"], stage, index, now - started)
                    for ack in acknowledgments:
                        marker_log.write(json_bytes(ack))
                    if acknowledgments:
                        marker_log.flush()
                        marks.extend(acknowledgments)
                    timing["controlSeconds"] = round(time.monotonic() - phase_started, 6)
                    timing["discoveryCalled"] = now >= next_discovery
                    phase_started = time.monotonic()
                    if timing["discoveryCalled"]:
                        tracker.discover()
                        next_discovery = time.monotonic() + 1
                    timing["discoverySeconds"] = round(time.monotonic() - phase_started, 6)
                    phase_started = time.monotonic()
                    rows = tracker.sample(time.monotonic())
                    timing["metricsSeconds"] = round(time.monotonic() - phase_started, 6)
                    groups = {group: aggregate(rows, None if group == "all" else group) for group in ("root", "webview2", "otherDescendants", "all")}
                    lag = max(0, now - next_sample)
                    max_lag = max(max_lag, lag)
                    sample = {"index": index, "observedUTC": utc_now(), "elapsedSeconds": round(now - started, 6),
                              "stage": stage, "sampleLagSeconds": round(lag, 6), "processes": rows, "groups": groups,
                              "treeCoverageComplete": not tracker.discovery_errors, "discoveryErrors": tracker.discovery_errors}
                    phase_started = time.monotonic()
                    encoded = json_bytes(sample)
                    if log_bytes + len(encoded) > MAX_LOG_BYTES:
                        reason = "sample_log_limit"
                        break
                    samples.write(encoded)
                    samples.flush()
                    log_bytes += len(encoded)
                    timing["sampleLogWriteSeconds"] = round(time.monotonic() - phase_started, 6)
                    update_peaks(peaks, groups)
                    phase = stages.setdefault(stage, {"firstSample": index, "lastSample": index, "firstObservedElapsedSeconds": sample["elapsedSeconds"], "lastObservedElapsedSeconds": sample["elapsedSeconds"], "peaks": {}})
                    phase.update(lastSample=index, lastObservedElapsedSeconds=sample["elapsedSeconds"])
                    update_peaks(phase["peaks"], groups)
                    incomplete_discovery_samples += bool(tracker.discovery_errors)
                    index += 1
                    if tracker.root.exit_info is not None and root_exit_observed is None:
                        root_exit_observed = time.monotonic()
                    status = {"runId": manifest["runId"], "running": True, "stage": stage, "samples": index,
                              "observedUTC": sample["observedUTC"], "targetExitConfirmed": tracker.root.handle is None,
                              "latestGroups": groups}
                    phase_started = time.monotonic()
                    timing["statusWriteSucceeded"] = True
                    try:
                        write_json(args.output_dir / "status.json", status, replace=True)
                    except OSError as error:
                        # Status is a convenience projection; a reader's lock
                        # must not discard the authoritative sample stream.
                        status_failures += 1
                        last_status_error = str(error)
                        timing["statusWriteSucceeded"] = False
                    timing["statusWriteSeconds"] = round(time.monotonic() - phase_started, 6)
                    finished = time.monotonic()
                    timing.update(iterationFinishedUTC=utc_now(), iterationFinishedMonotonicSeconds=round(finished, 6),
                                  iterationFinishedElapsedSeconds=round(finished - started, 6),
                                  iterationWorkSeconds=round(finished - now, 6))
                    last_iteration_finished = timing["iterationFinishedElapsedSeconds"]
                    timing_started = time.monotonic()
                    encoded_timing = json_bytes(timing)
                    if timing_log_bytes + len(encoded_timing) > MAX_TIMING_LOG_BYTES:
                        reason = "timing_log_limit"
                        break
                    timing_log.write(encoded_timing)
                    timing_log.flush()
                    timing_log_bytes += len(encoded_timing)
                    timing_records += 1
                    previous_timing_write = round(time.monotonic() - timing_started, 6)
                    previous_sleep = None
                    if stop:
                        reason = "operator_stop"
                        break
                    if root_exit_observed is not None:
                        # Keep a bounded grace even after all known children exit,
                        # so another tree refresh can discover a surviving child.
                        if time.monotonic() - root_exit_observed >= args.exit_grace_seconds:
                            reason = "target_exit_grace_complete"
                            break
                    if time.monotonic() >= deadline:
                        break
                    next_sample += args.interval_ms / 1000
                    delay = next_sample - time.monotonic()
                    if delay <= 0:
                        next_sample = time.monotonic()
                    else:
                        requested_sleep = min(delay, max(0, deadline - time.monotonic()))
                        sleep_started = time.monotonic()
                        time.sleep(requested_sleep)
                        previous_sleep = {"requestedSeconds": round(requested_sleep, 6),
                                          "observedSeconds": round(time.monotonic() - sleep_started, 6)}
        except KeyboardInterrupt:
            reason = "sampler_interrupted"
        except Exception as error:
            reason, failure = "sampler_error", f"{type(error).__name__}: {error}"
        inventory = tracker.inventory()
        result = {"schemaVersion": 1, "runId": manifest["runId"], "startedUTC": manifest["startedUTC"], "finishedUTC": utc_now(),
                  "elapsedSeconds": round(time.monotonic() - started, 6), "stopReason": reason, "error": failure,
                  "samples": index, "sampleLogBytes": log_bytes, "maxSampleLagSeconds": round(max_lag, 6),
                  "targetExitConfirmed": tracker.root.handle is None and tracker.root.exit_info is not None,
                  "allObservedDescendantsExited": all(item["exitConfirmed"] for item in inventory if item["group"] != "root"),
                  "discoveryFailures": tracker.discovery_failures, "incompleteTreeSamples": incomplete_discovery_samples,
                  "statusWriteFailures": status_failures, "lastStatusError": last_status_error,
                  "timingRecords": timing_records, "timingLogBytes": timing_log_bytes,
                  "lastIterationFinishedElapsedSeconds": last_iteration_finished,
                  "lastTimingLogWriteSeconds": previous_timing_write, "lastSleep": previous_sleep,
                  "processes": inventory, "peaks": peaks, "stages": stages, "marks": marks, "limitations": LIMITATIONS}
        write_json(args.output_dir / "summary.json", result)
        try:
            write_json(args.output_dir / "status.json", {"runId": manifest["runId"], "running": False, "stage": stage,
                       "samples": index, "stopReason": reason, "targetExitConfirmed": result["targetExitConfirmed"]}, replace=True)
        except OSError:
            # summary.json is the final receipt even if a status reader remains.
            pass
        return result
    finally:
        tracker.close()


def validate_record(args):
    if not 0 < args.pid <= 0xffffffff or not 0 < args.duration_seconds <= 7200 or not 250 <= args.interval_ms <= 5000 or not 0 <= args.exit_grace_seconds <= 60:
        raise ValueError("Require positive DWORD PID, duration (0,7200] s, interval [250,5000] ms, grace [0,60] s")


def control(args):
    directory = args.run_dir
    if not directory.is_absolute():
        raise ValueError("Use an absolute run-directory path")
    manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
    if (directory / "summary.json").exists():
        raise ValueError("The sampler has already finished")
    api = WindowsAPI()
    own = manifest["sampler"]
    handle, _ = attach(api, own["pid"], own["created"], own["exe"])
    api.close(handle)
    request = {"runId": manifest["runId"], "requestId": uuid.uuid4().hex, "action": args.command, "requestedUTC": utc_now()}
    if args.command == "mark":
        if not args.stage.strip() or len(args.stage) > 128 or len(args.note) > 1000:
            raise ValueError("Stage must contain 1–128 characters; note at most 1000")
        request.update(stage=args.stage, note=args.note)
    name = f'{time.time_ns():020d}-{request["requestId"]}'
    path = directory / "control" / (name + ".request.json")
    write_json(path, request)
    acknowledgment = path.with_name(name + ".ack.json")
    deadline = time.monotonic() + args.wait_seconds
    while time.monotonic() < deadline:
        if acknowledgment.exists():
            return json.loads(acknowledgment.read_text(encoding="utf-8"))
        time.sleep(0.1)
    return {**request, "confirmation": "unconfirmed", "requestFile": str(path)}


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    commands = result.add_subparsers(dest="command", required=True)
    identify = commands.add_parser("identity", help="Inspect the explicitly named process")
    identify.add_argument("--pid", type=int, required=True)
    identify.add_argument("--exe", type=Path, required=True)
    recording = commands.add_parser("record", help="Attach and sample; never launch or terminate the target")
    recording.add_argument("--pid", type=int, required=True)
    recording.add_argument("--created", type=positive_decimal, required=True)
    recording.add_argument("--exe", type=Path, required=True)
    recording.add_argument("--output-dir", type=Path, required=True)
    recording.add_argument("--duration-seconds", type=float, default=900)
    recording.add_argument("--interval-ms", type=int, default=500)
    recording.add_argument("--exit-grace-seconds", type=float, default=5)
    for command in ("mark", "stop"):
        command_parser = commands.add_parser(command, help="Send an operator request to the sampler only")
        command_parser.add_argument("--run-dir", type=Path, required=True)
        command_parser.add_argument("--wait-seconds", type=float, default=5)
        if command == "mark":
            command_parser.add_argument("--stage", required=True)
            command_parser.add_argument("--note", default="")
    return result


def main():
    arguments = parser().parse_args()
    try:
        if arguments.command == "identity":
            if not 0 < arguments.pid <= 0xffffffff or not arguments.exe.is_absolute():
                raise ValueError("Use a positive PID and an absolute executable path")
            api = WindowsAPI()
            handle = api.open(arguments.pid)
            try:
                result = api.identity(handle, arguments.pid)
                if not same_image(result["exe"], arguments.exe) or api.lifecycle(handle)["exited"]:
                    raise ValueError("The named executable is not the live target")
            finally:
                api.close(handle)
        elif arguments.command == "record":
            validate_record(arguments)
            result = record(arguments)
        else:
            if not 0 <= arguments.wait_seconds <= 15:
                raise ValueError("Confirmation wait must be between 0 and 15 seconds")
            result = control(arguments)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1 if result.get("error") or result.get("confirmation") == "unconfirmed" else 0
    except (OSError, ValueError, RuntimeError) as error:
        print(f"resource_probe: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

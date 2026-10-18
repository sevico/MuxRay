"""Launch an existing desktop EXE and retain bounded local exit evidence.

Example (run the collector in the background if desired):
  python capture_exit.py --exe C:\\build\\MuxRay.exe --output C:\\evidence\\run-001

The product window is not hidden and no GUI input is issued. The target inherits
the collector's environment. stdout.bin and stderr.bin are separate raw byte
prefixes, each limited to 1 MiB in memory and on disk. Readers do no disk I/O;
an independent writer persists prefixes while both pipes continue to be drained.
Raw output CAN contain paths or other private data. It stays local, is not
privacy-filtered, and is never uploaded. Do not share it without inspection.

capture.json records the actual child process return code, not a window-list
inference. The executable hash describes the file read before launch. Process
exit does not prove orderly media-file closure or resource stability.
captureComplete means both pipes reached EOF and retained prefixes were written
and read back with matching hashes. It does not mean all bytes were retained.
retainedBytes/sha256 describe the memory prefix; writtenBytes counts accepted
writes, and verifiedBytes/persistedSha256 describe final disk readback. Inspect
each stream's truncated/missingData as well as persistenceComplete.

--timeout-seconds is an observation deadline, NOT permission to terminate the
product. At the deadline capture-timeout.json is queued for the independent
writer, and timedOut=true is recorded in memory; collection continues until the
user closes the product. Disk stalls can delay receipt persistence. Ctrl+C in
the observation/wait loops records an interruption and continues collecting.
Do not kill the collector while the product is running: closing its pipes can affect output
writers, and a final exit/tail record would be unavailable.

After the target exits, --drain-timeout-seconds limits the wait for pipe EOF (a
descendant may retain a pipe). A missing EOF is explicitly incomplete evidence;
no descendant is terminated. No automatic retry or product restart is performed.
Final persistence has a five-second wait limit. If the writer is stalled, the
CLI summary reports writerTimedOut=true and receiptMayBeStale=true; the on-disk
receipt may be older, missing, or a final snapshot prepared before that timeout.
The CLI's incomplete result remains authoritative even if that previously
prepared receipt reaches disk later; the receipt alone cannot upgrade the run.

Collector exit codes: 0 = complete capture, target exit 0, no deadline/interrupt;
1 = complete capture of a nonzero target exit; 2 = timeout, interruption, launch
failure, collection error, or unconfirmed stream tail. Target exitCode is always
reported separately. These synthetic helper tests do not validate native GUI
behavior: python -m unittest discover -s tools/desktop-native -p capture_exit_test.py
"""

import argparse
import copy
import ctypes
from ctypes import wintypes
from datetime import datetime, timedelta, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import threading
import time


STREAM_LIMIT_BYTES = 1 << 20
READ_BYTES = 64 << 10
WRITER_WAIT_SECONDS = 5.0


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def error_record(error):
    return {"type": type(error).__name__, "message": str(error)[:1024]}


def write_json(path, value):
    temporary = path.with_name(path.name + ".tmp")
    # Only one collector writes this newly created output directory.
    with temporary.open("w", encoding="utf-8", newline="\n") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


def windows_creation_identity(process):
    if os.name != "nt":
        return {"creationFiletime": None, "osStartedUtc": None}
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    get_times = kernel32.GetProcessTimes
    get_times.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
    get_times.restype = wintypes.BOOL
    creation, end, kernel, user = (wintypes.FILETIME() for _ in range(4))
    # Popen retains the exact process handle, including after a fast exit. Do
    # not reopen by PID and accidentally identify a later process generation.
    if not get_times(int(process._handle), ctypes.byref(creation), ctypes.byref(end),
                     ctypes.byref(kernel), ctypes.byref(user)):
        raise ctypes.WinError(ctypes.get_last_error())
    ticks = creation.dwLowDateTime | creation.dwHighDateTime << 32
    started = datetime(1601, 1, 1, tzinfo=timezone.utc) + timedelta(microseconds=ticks // 10)
    return {"creationFiletime": str(ticks), "osStartedUtc": started.isoformat()}


class StreamCapture:
    def __init__(self, name, path):
        self.name = name
        self.path = path
        self.lock = threading.Lock()
        self.done = threading.Event()
        self.thread = None
        self.observed = 0
        self.prefix = bytearray()
        self.eof = False
        self.frozen = False
        self.read_error = None
        self.digest = hashlib.sha256()

    def start(self, pipe):
        self.thread = threading.Thread(target=self._read, args=(pipe,), daemon=True,
                                       name="capture-" + self.name)
        self.thread.start()

    def _read(self, pipe):
        try:
            while True:
                block = os.read(pipe.fileno(), READ_BYTES)
                if not block:
                    with self.lock:
                        if not self.frozen:
                            self.eof = True
                    return
                with self.lock:
                    if self.frozen:
                        continue
                    self.observed += len(block)
                    remaining = STREAM_LIMIT_BYTES - len(self.prefix)
                    if remaining <= 0:
                        continue
                    retained = memoryview(block)[:remaining]
                    self.prefix.extend(retained)
                    self.digest.update(retained)
        except OSError as error:
            with self.lock:
                if not self.frozen:
                    self.read_error = error_record(error)
        finally:
            pipe.close()
            self.done.set()

    def _snapshot_locked(self):
        discarded = self.observed - len(self.prefix)
        return {
            "file": self.path.name,
            "limitBytes": STREAM_LIMIT_BYTES,
            "observedBytes": self.observed,
            "retainedBytes": len(self.prefix),
            "discardedBytes": discarded,
            "truncated": self.observed > STREAM_LIMIT_BYTES,
            "eofObserved": self.eof,
            "tailUnknown": not self.eof or self.read_error is not None,
            "missingData": discarded > 0 or not self.eof or self.read_error is not None,
            "sha256": self.digest.hexdigest(),
            "readError": self.read_error,
        }

    def snapshot(self):
        with self.lock:
            return self._snapshot_locked()

    def freeze(self):
        with self.lock:
            self.frozen = True
            return self._snapshot_locked()

    def chunk(self, offset, end):
        # At most one 64 KiB copy per writer operation. No disk work or file
        # lock is held while a reader's lock is held.
        with self.lock:
            return bytes(self.prefix[offset:min(end, offset + READ_BYTES)])


class DiskWriter:
    """One bounded pending status plus the two named milestone receipts.

    Disk operations never hold a reader lock or the writer's state/queue lock.
    The reader prefixes are the sole retained raw data, not an unbounded queue.
    """

    def __init__(self, output, streams):
        self.output = output
        self.streams = streams
        self.lock = threading.Lock()
        self.wake = threading.Event()
        self.done = threading.Event()
        self.pending = None
        self.milestones = {}
        self.result = None
        self.timed_out = False
        self.errors = []
        self.files = {}
        self.states = {}
        try:
            for name, stream in streams.items():
                self.files[name] = stream.path.open("xb", buffering=0)
                self.states[name] = {"writtenBytes": 0, "syncedBytes": 0,
                                     "verifiedBytes": None, "persistedSha256": None,
                                     "persistenceComplete": False, "writeError": None}
        except Exception:
            for file in self.files.values():
                file.close()
            raise
        self.thread = threading.Thread(target=self._run, daemon=True, name="capture-disk-writer")

    def submit(self, record, filename="capture.json", final=False):
        request = (copy.deepcopy(record), final)
        with self.lock:
            if filename == "capture.json":
                self.pending = request
            else:
                # Only capture-start.json and capture-timeout.json are used.
                self.milestones[filename] = request
        self.wake.set()

    def _write_chunk(self, file, block):
        return file.write(block)

    def _sync(self, file):
        os.fsync(file.fileno())

    def _persist_stream(self, name, final):
        stream = self.streams[name]
        expected = stream.snapshot()
        with self.lock:
            position = self.states[name]["writtenBytes"]
            failed = self.states[name]["writeError"] is not None
        if failed:
            return
        try:
            while position < expected["retainedBytes"]:
                block = memoryview(stream.chunk(position, expected["retainedBytes"]))
                while block:
                    written = self._write_chunk(self.files[name], block)
                    if written is None or written <= 0 or written > len(block):
                        raise OSError("Output write returned an invalid byte count")
                    position += written
                    block = block[written:]
                    with self.lock:
                        self.states[name]["writtenBytes"] = position
            self._sync(self.files[name])
            with self.lock:
                self.states[name]["syncedBytes"] = position
            if final:
                digest = hashlib.sha256()
                verified = 0
                with stream.path.open("rb") as file:
                    while True:
                        block = file.read(READ_BYTES)
                        if not block:
                            break
                        verified += len(block)
                        if verified > STREAM_LIMIT_BYTES:
                            raise OSError("Persisted stream exceeds retention limit")
                        digest.update(block)
                actual_hash = digest.hexdigest()
                with self.lock:
                    self.states[name].update(verifiedBytes=verified, persistedSha256=actual_hash)
                if verified != expected["retainedBytes"] or actual_hash != expected["sha256"]:
                    raise OSError("Persisted prefix does not match retained memory prefix")
                with self.lock:
                    self.states[name]["persistenceComplete"] = True
        except OSError as error:
            with self.lock:
                self.states[name]["writeError"] = error_record(error)

    def snapshot_record(self, record, final=False):
        result = copy.deepcopy(record)
        with self.lock:
            states = copy.deepcopy(self.states)
            errors = copy.deepcopy(self.errors)
            timed_out = self.timed_out
        result["streams"] = {}
        for name, stream in self.streams.items():
            item = stream.snapshot()
            item.update(states[name])
            item["missingData"] |= not item["persistenceComplete"]
            result["streams"][name] = item
        result["errors"].extend(errors)
        result["writerTimedOut"] = timed_out
        result["receiptMayBeStale"] = timed_out or bool(errors)
        result["persistenceComplete"] = final and not errors and all(
            item["persistenceComplete"] for item in result["streams"].values())
        result["captureComplete"] = (
            result["exitConfirmed"] and not result["errors"] and not timed_out
            and result["persistenceComplete"] and all(
                item["eofObserved"] and item["readError"] is None
                for item in result["streams"].values()))
        result["updatedUtc"] = utc_now()
        return result

    def _run(self):
        try:
            while True:
                self.wake.wait()
                with self.lock:
                    tasks = list(self.milestones.items())
                    self.milestones.clear()
                    if self.pending is not None:
                        tasks.append(("capture.json", self.pending))
                        self.pending = None
                    self.wake.clear()
                for filename, (record, final) in tasks:
                    for name in self.streams:
                        self._persist_stream(name, final)
                    result = self.snapshot_record(record, final)
                    try:
                        write_json(self.output / filename, result)
                    except OSError as error:
                        with self.lock:
                            if not self.errors:
                                self.errors.append({"stage": "receipt_write", **error_record(error)})
                        result = self.snapshot_record(record, final)
                    if final:
                        self.result = result
                        return
        except Exception as error:
            with self.lock:
                self.errors.append({"stage": "writer", **error_record(error)})
        finally:
            for file in self.files.values():
                try:
                    file.close()
                except OSError as error:
                    with self.lock:
                        self.errors.append({"stage": "writer_close", **error_record(error)})
            self.done.set()

    def finish(self, record, wait_seconds):
        self.submit(record, final=True)
        deadline = time.monotonic() + wait_seconds
        while not self.done.is_set() and time.monotonic() < deadline:
            try:
                self.done.wait(min(0.05, max(0, deadline - time.monotonic())))
            except KeyboardInterrupt:
                record["collectorInterruptions"] += 1
        if not self.done.is_set():
            with self.lock:
                self.timed_out = True
            return self.snapshot_record(record)
        result = self.result or self.snapshot_record(record)
        # An interrupt while awaiting persistence remains a collector failure
        # even when an already-written receipt predates that interrupt.
        if result["collectorInterruptions"] != record["collectorInterruptions"]:
            result["receiptMayBeStale"] = True
        result["collectorInterruptions"] = record["collectorInterruptions"]
        with self.lock:
            close_errors = [error for error in self.errors if error not in result["errors"]]
        if close_errors:
            result["errors"].extend(copy.deepcopy(close_errors))
            result["captureComplete"] = False
            result["persistenceComplete"] = False
            result["receiptMayBeStale"] = True
        return result


def capture(executable, output, arguments=(), timeout_seconds=900.0,
            drain_timeout_seconds=5.0, cwd=None, writer_wait_seconds=WRITER_WAIT_SECONDS):
    executable = Path(executable).resolve(strict=True)
    if not executable.is_file():
        raise ValueError("--exe must be a regular file")
    output = Path(output)
    if not output.is_absolute():
        raise ValueError("--output must be absolute")
    output.mkdir(parents=True, exist_ok=False)
    record = {
        "schemaVersion": 2,
        "tool": "capture_exit.py",
        "executable": str(executable),
        "exeSha256": sha256(executable),
        "exeHashScope": "file read before launch; not a hash of loaded memory",
        "collectorPid": os.getpid(),
        "launchRequestedUtc": None,
        "launchReturnedUtc": None,
        "pid": None,
        "creationFiletime": None,
        "osStartedUtc": None,
        "phase": "prepared",
        "exitConfirmed": False,
        "exitCode": None,
        "exitObservedUtc": None,
        "timeoutSeconds": timeout_seconds,
        "drainTimeoutSeconds": drain_timeout_seconds,
        "writerWaitSeconds": writer_wait_seconds,
        "timedOut": False,
        "timeoutObservedUtc": None,
        "collectorInterruptions": 0,
        "drainTimedOut": False,
        "captureComplete": False,
        "persistenceComplete": False,
        "writerTimedOut": False,
        "receiptMayBeStale": False,
        "rawOutputMayContainPrivateData": True,
        "streams": {},
        "errors": [],
    }
    streams = {name: StreamCapture(name, output / (name + ".bin")) for name in ("stdout", "stderr")}
    writer = DiskWriter(output, streams)

    def save(name="capture.json", final=False):
        writer.submit(record, name, final)

    # Preflight disk I/O happens before launch. Once the product is running,
    # only the independent writer touches disk, including receipt writes.
    try:
        write_json(output / "capture.json", writer.snapshot_record(record))
    except Exception:
        for file in writer.files.values():
            file.close()
        raise
    writer.thread.start()

    record["launchRequestedUtc"] = utc_now()
    start = time.monotonic()
    try:
        process = subprocess.Popen(
            [str(executable), *arguments], cwd=str(cwd or executable.parent),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0,
            # env=None deliberately preserves the inherited environment.
            env=None, shell=False, close_fds=True,
            # Isolate console interrupts, without hiding a Windows GUI window.
            creationflags=subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0,
            start_new_session=os.name != "nt",
        )
    except (OSError, ValueError) as error:
        record["phase"] = "launch_failed"
        record["errors"].append({"stage": "launch", **error_record(error)})
        record["streams"] = {name: stream.freeze() for name, stream in streams.items()}
        return writer.finish(record, writer_wait_seconds)

    record["launchReturnedUtc"] = utc_now()
    record["pid"] = process.pid
    record["phase"] = "running"
    streams["stdout"].start(process.stdout)
    streams["stderr"].start(process.stderr)
    try:
        record.update(windows_creation_identity(process))
    except OSError as error:
        record["errors"].append({"stage": "process_identity", **error_record(error)})
    save("capture-start.json")
    save()
    next_status = time.monotonic() + 1.0
    while True:
        try:
            code = process.poll()
            if code is not None:
                record.update(exitConfirmed=True, exitCode=code,
                              exitObservedUtc=utc_now(), phase="draining")
                break
            now = time.monotonic()
            if not record["timedOut"] and now - start >= timeout_seconds:
                record.update(timedOut=True, timeoutObservedUtc=utc_now(), phase="timed_out_running")
                save("capture-timeout.json")
                save()
            elif now >= next_status:
                save()
                next_status = now + 1.0
            time.sleep(0.05)
        except KeyboardInterrupt:
            record["collectorInterruptions"] += 1
            record["phase"] = "interrupted_running"
            save()

    save()
    drain_deadline = time.monotonic() + drain_timeout_seconds
    while not all(stream.done.is_set() for stream in streams.values()):
        if time.monotonic() >= drain_deadline:
            record["drainTimedOut"] = True
            break
        try:
            time.sleep(min(0.02, max(0.0, drain_deadline - time.monotonic())))
        except KeyboardInterrupt:
            record["collectorInterruptions"] += 1
            record["drainTimedOut"] = True
            break
    record["streams"] = {name: stream.freeze() for name, stream in streams.items()}
    record["phase"] = "exited"
    record["elapsedSeconds"] = round(time.monotonic() - start, 6)
    return writer.finish(record, writer_wait_seconds)


def collector_exit_code(record):
    if (not record["captureComplete"] or record["timedOut"]
            or record["collectorInterruptions"] or record["drainTimedOut"]):
        return 2
    return 0 if record["exitCode"] == 0 else 1


def positive_seconds(value):
    parsed = float(value)
    if not math.isfinite(parsed) or parsed <= 0:
        raise argparse.ArgumentTypeError("Use a finite positive number of seconds")
    return parsed


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--exe", type=Path, required=True, help="Existing executable; no build or replacement")
    parser.add_argument("--output", type=Path, required=True, help="New absolute directory; never overwrite evidence")
    parser.add_argument("--cwd", type=Path, help="Target working directory (default: executable directory)")
    parser.add_argument("--timeout-seconds", type=positive_seconds, default=900.0)
    parser.add_argument("--drain-timeout-seconds", type=positive_seconds, default=5.0)
    parser.add_argument("arguments", nargs=argparse.REMAINDER, help="Optional target arguments after --")
    args = parser.parse_args(argv)
    arguments = args.arguments[1:] if args.arguments[:1] == ["--"] else args.arguments
    try:
        record = capture(args.exe, args.output, arguments, args.timeout_seconds,
                         args.drain_timeout_seconds, args.cwd)
    except (OSError, ValueError) as error:
        print("capture_exit: " + str(error), file=sys.stderr)
        return 2
    print(json.dumps({"pid": record["pid"], "exitCode": record["exitCode"],
                      "captureComplete": record["captureComplete"], "timedOut": record["timedOut"],
                      "persistenceComplete": record["persistenceComplete"],
                      "writerTimedOut": record["writerTimedOut"],
                      "collectorInterruptions": record["collectorInterruptions"],
                      "drainTimedOut": record["drainTimedOut"],
                      "receiptMayBeStale": record["receiptMayBeStale"],
                      "receipt": str(args.output / "capture.json")}))
    return collector_exit_code(record)


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Opt-in real CTRL_C_EVENT smoke tests, only inside a private hidden console."""
import argparse
import ctypes as C
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

from corpus import file_identity, require
from cancel_trials import distribution


class IOCounters(C.Structure):
    _fields_ = [(name, C.c_uint64) for name in ("reads", "writes", "other", "readBytes", "writeBytes", "otherBytes")]


class BasicLimits(C.Structure):
    _fields_ = [("processTime", C.c_int64), ("jobTime", C.c_int64), ("flags", C.c_uint32),
                ("minWorkingSet", C.c_size_t), ("maxWorkingSet", C.c_size_t), ("activeProcesses", C.c_uint32),
                ("affinity", C.c_size_t), ("priority", C.c_uint32), ("scheduling", C.c_uint32)]


class ExtendedLimits(C.Structure):
    _fields_ = [("basic", BasicLimits), ("io", IOCounters), ("processMemory", C.c_size_t),
                ("jobMemory", C.c_size_t), ("peakProcessMemory", C.c_size_t), ("peakJobMemory", C.c_size_t)]


def only_owned_console(actual, expected):
    require(len(actual) == len(set(actual)) and set(actual) == set(expected),
            "refusing console event: console contains unexpected processes")


class Windows:
    def __init__(self):
        require(os.name == "nt", "Windows console tests require Windows")
        self.dll = C.WinDLL("kernel32", use_last_error=True)
        signatures = {
            "GetConsoleProcessList": ([C.POINTER(C.c_uint32), C.c_uint32], C.c_uint32),
            "SetConsoleCtrlHandler": ([C.c_void_p, C.c_int], C.c_int),
            "GenerateConsoleCtrlEvent": ([C.c_uint32, C.c_uint32], C.c_int),
            "GetProcessIoCounters": ([C.c_void_p, C.POINTER(IOCounters)], C.c_int),
            "CreateJobObjectW": ([C.c_void_p, C.c_wchar_p], C.c_void_p),
            "SetInformationJobObject": ([C.c_void_p, C.c_int, C.c_void_p, C.c_uint32], C.c_int),
            "AssignProcessToJobObject": ([C.c_void_p, C.c_void_p], C.c_int),
            "CloseHandle": ([C.c_void_p], C.c_int),
        }
        for name, (args, result) in signatures.items():
            procedure = getattr(self.dll, name)
            procedure.argtypes, procedure.restype = args, result

    def checked(self, name, *args):
        result = getattr(self.dll, name)(*args)
        if not result:
            raise C.WinError(C.get_last_error())
        return result

    def members(self):
        items = (C.c_uint32 * 8)()
        count = self.checked("GetConsoleProcessList", items, len(items))
        require(count <= len(items), "refusing event: console has too many processes")
        return list(items[:count])

    def io_counters(self, process):
        counters = IOCounters()
        self.checked("GetProcessIoCounters", int(process._handle), C.byref(counters))
        return counters


class OwnedJob:
    def __init__(self, api):
        self.api = api
        self.handle = api.checked("CreateJobObjectW", None, None)
        limits = ExtendedLimits()
        limits.basic.flags = 0x2000  # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; no breakaway.
        try:
            api.checked("SetInformationJobObject", self.handle, 9, C.byref(limits), C.sizeof(limits))
        except BaseException:
            self.close()
            raise

    def assign(self, process):
        self.api.checked("AssignProcessToJobObject", self.handle, int(process._handle))

    def close(self):
        if self.handle:
            self.api.checked("CloseHandle", self.handle)
            self.handle = None


class Capture:
    def __init__(self, process, output_gate=False, limit=16 << 20):
        self.process = process
        self.buffers = [bytearray(), bytearray()]
        self.first_output = threading.Event()
        self.release = threading.Event()
        self.failed = threading.Event()
        self.threads = []
        if not output_gate:
            self.release.set()

        def drain(pipe, index):
            try:
                while chunk := os.read(pipe.fileno(), 4096):
                    if len(self.buffers[index]) + len(chunk) > limit:
                        self.failed.set()
                        return
                    self.buffers[index].extend(chunk)
                    if index == 0:
                        self.first_output.set()
                        if not self.release.wait(12):
                            self.failed.set()
                            return
            except OSError:
                self.failed.set()
            finally:
                pipe.close()

        for index, pipe in enumerate((process.stdout, process.stderr)):
            thread = threading.Thread(target=drain, args=(pipe, index), daemon=True)
            thread.start()
            self.threads.append(thread)

    def check(self):
        require(not self.failed.is_set(), "capture failed or exceeded output limit")

    def wait(self, timeout):
        deadline = time.monotonic() + timeout
        while self.process.poll() is None or any(t.is_alive() for t in self.threads):
            self.check()
            require(time.monotonic() < deadline, "console test timed out")
            time.sleep(.001)
        self.check()
        return self.process.returncode, bytes(self.buffers[0]), bytes(self.buffers[1])

    def join(self):
        self.release.set()
        for thread in self.threads:
            thread.join(timeout=2)
        require(not any(t.is_alive() for t in self.threads), "capture threads did not terminate")


CASES = {
    "version": ("none", ["version"]),
    "inspect-scan": ("scan", ["inspect"]),
    "tags-scan": ("scan", ["tags", "--count", "4096"]),
    "summary-scan": ("scan", ["summary", "--schema-version", "2"]),
    "detail-scan": ("scan", ["detail", "--tag", "0"]),
    "inspect-output": ("output", ["inspect"]),
    "tags-output": ("output", ["tags", "--count", "4096"]),
}


def helper(muxray, path, case):
    # Never attach to another console and never signal before both ownership
    # checks. Parent releases stdin only after assigning this helper to its job.
    require(sys.stdin.buffer.readline(16) == b"RUN\n", "missing parent job handshake")
    api = Windows()
    only_owned_console(api.members(), [os.getpid()])
    api.checked("SetConsoleCtrlHandler", None, 0)  # Child must inherit enabled Ctrl+C.
    phase, args = CASES[case]
    command = [str(muxray), *args] + ([] if phase == "none" else [str(path)])
    process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, bufsize=0)  # Inherit private console, not a new group.
    capture = Capture(process, output_gate=phase == "output")
    sent = None
    before_reads = 0
    try:
        # Change only helper state AFTER child creation: ignoring Ctrl+C is inherited.
        api.checked("SetConsoleCtrlHandler", None, 1)
        if phase != "none":
            deadline = time.monotonic() + 10
            while True:
                capture.check()
                require(process.poll() is None, "target exited before cancellation; test did not run")
                require(time.monotonic() < deadline, "target did not reach requested phase")
                counters = api.io_counters(process)
                if phase == "output" and capture.first_output.is_set():
                    break
                if phase == "scan" and counters.reads >= 1000:
                    require(not capture.first_output.is_set(), "scan phase already ended")
                    break
                time.sleep(.001)
            before_reads = counters.reads
            only_owned_console(api.members(), [os.getpid(), process.pid])
            sent = time.perf_counter_ns()
            api.checked("GenerateConsoleCtrlEvent", 0, 0)  # Real CTRL_C_EVENT; private console only.
            capture.release.set()  # Do not leave the target blocked on our stdout pipe.
        code, output, error = capture.wait(10)
        ended = time.perf_counter_ns()
        message = error.decode("utf-8", errors="replace")
        if phase == "none":
            require(code == 0 and output.startswith(b"muxray ") and not error, "normal version exit failed")
        else:
            require(code == 130 and "context canceled" in message and "discard incomplete stdout" in message,
                    f"Ctrl+C contract failed: exit={code}, stderr={message[:1024]}")
            if phase == "scan":
                require(not output and "scan input: context canceled" in message, "cancellation did not occur during scan")
            else:
                require(output and "write " in message, "cancellation did not occur during output")
        cleanup_deadline = time.monotonic() + 1
        while set(api.members()) != {os.getpid()} and time.monotonic() < cleanup_deadline:
            time.sleep(.001)
        only_owned_console(api.members(), [os.getpid()])
        try:
            json.loads(output)
            valid_json = True
        except (ValueError, UnicodeDecodeError):
            valid_json = False
        return {"case": case, "phase": phase, "event": "CTRL_C_EVENT" if sent else None,
                "exitCode": code, "eventToExitAndDrainMs": (ended - sent) / 1e6 if sent else None,
                "readOperationsBeforeSignal": before_reads, "stdoutBytes": len(output),
                "stdoutIsCompleteJSON": valid_json, "discardStdout": code == 130,
                "stderr": message, "consoleOwnershipChecked": True,
                "consoleMembersAfterExit": 1, "forcedTermination": False}
    finally:
        capture.release.set()
        if process.poll() is None:
            process.kill()  # Exact target we created, never an image-name/tree search.
        process.wait(timeout=2)
        capture.join()


def isolated_trial(muxray, path, case):
    api = Windows()
    job = OwnedJob(api)
    startup = subprocess.STARTUPINFO()
    startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
    startup.wShowWindow = subprocess.SW_HIDE
    process = None
    capture = None
    try:
        process = subprocess.Popen([sys.executable, "-B", str(Path(__file__).resolve()), "--helper", "--muxray", str(muxray),
            "--file", str(path), "--case", case], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            bufsize=0, startupinfo=startup, creationflags=subprocess.CREATE_NEW_CONSOLE)
        capture = Capture(process, limit=65536)
        job.assign(process)  # Helper is waiting on stdin; cannot create target yet.
        process.stdin.write(b"RUN\n")
        process.stdin.close()
        code, output, error = capture.wait(25)
        require(code == 0, f"isolated helper failed: {error.decode('utf-8', errors='replace')[:2048]}")
        return json.loads(output)
    finally:
        job.close()  # Also cleans descendants on timeout, overflow or parent failure.
        if process is not None:
            if process.stdin and not process.stdin.closed:
                process.stdin.close()
            if process.poll() is None:
                process.kill()  # Covers failure to assign the waiting helper to the job.
            process.wait(timeout=2)
        if capture is not None:
            capture.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--muxray", type=Path, required=True)
    parser.add_argument("--file", type=Path, required=True)
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--case", choices=CASES, help="run one selected case instead of full matrix")
    parser.add_argument("--output", type=Path, help="exclusive-create JSON evidence file")
    parser.add_argument("--helper", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    require(os.name == "nt" and 1 <= args.runs <= 10, "Windows required; runs must be 1-10")
    muxray, path = args.muxray.resolve(strict=True), args.file.resolve(strict=True)
    require(muxray.is_file() and path.is_file() and path.stat().st_size >= 1 << 30, "regular binary and large fixture required")
    if args.helper:
        require(args.case is not None, "helper needs one case")
        print(json.dumps(helper(muxray, path, args.case)))
        return
    require(args.output is None or not args.output.exists(), "refusing to overwrite evidence")
    report = {"toolVersion": 1, "binarySha256": file_identity(muxray)[1], "inputName": path.name,
              "sizeBytes": path.stat().st_size, "runsPerCase": args.runs, "privateHiddenConsole": True,
              "killOnJobClose": True, "trials": [], "failures": []}
    before = path.stat()
    for case in ([args.case] if args.case else CASES):
        for i in range(args.runs):
            try:
                report["trials"].append(isolated_trial(muxray, path, case))
            except (ValueError, OSError, subprocess.TimeoutExpired) as error:
                report["failures"].append({"case": case, "iteration": i, "error": str(error)})
                break
        if report["failures"]:
            break
        print(f"completed {case}: {args.runs} independent consoles", file=sys.stderr, flush=True)
    timings = [t["eventToExitAndDrainMs"] for t in report["trials"] if t["event"]]
    after = path.stat()
    if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns) or file_identity(muxray)[1] != report["binarySha256"]:
        report["failures"].append({"error": "input or binary changed during the matrix"})
    report["eventToExitAndDrainMs"] = distribution(timings) if timings else None
    report["complete"] = not report["failures"]
    if args.output:
        with args.output.open("x", encoding="utf-8") as output:
            json.dump(report, output, indent=2)
            output.write("\n")
    print(json.dumps(report, indent=2))
    raise SystemExit(0 if report["complete"] else 1)


if __name__ == "__main__":
    main()

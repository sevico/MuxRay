"""Create bounded local H.264/AAC long-media fixtures; never download media.

Default: 40 minutes, 160x90, 25 fps, fixed 50-frame GOP and two B frames.
This is generated encoded media, not a real-world recording. Each run requires
a new absolute directory and retains its plan and any failed/partial output.
"""

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import signal
import subprocess
import threading
import time


VERSION = 1
MAX_BYTES = 256 << 20
MAX_TAGS = 500_000
OUTPUT_LIMIT = 64 << 10


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_json(path, value):
    with path.open("x", encoding="utf-8", newline="\n") as output:
        json.dump(value, output, indent=2, ensure_ascii=False)
        output.write("\n")


def stop_owned(process):
    if process.poll() is not None:
        return
    if os.name == "nt":
        subprocess.run(["taskkill.exe", "/PID", str(process.pid), "/T", "/F"],
                       stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, timeout=5, check=False,
                       creationflags=subprocess.CREATE_NO_WINDOW)
    else:
        os.killpg(process.pid, signal.SIGKILL)
    if process.poll() is None:
        process.kill()


def bounded_run(argv, timeout):
    """Run an argv without a shell, bounding both streams and wall time."""
    options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {"start_new_session": True}
    process = subprocess.Popen([str(x) for x in argv], stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)
    buffers = [bytearray(), bytearray()]
    overflow = threading.Event()

    def drain(pipe, buffer):
        with pipe:
            while data := pipe.read(4096):
                remaining = OUTPUT_LIMIT - len(buffer)
                buffer.extend(data[:remaining])
                if len(data) > remaining:
                    overflow.set()
                    return

    threads = [threading.Thread(target=drain, args=pair, daemon=True)
               for pair in zip((process.stdout, process.stderr), buffers)]
    for thread in threads:
        thread.start()
    start = time.monotonic()
    reason = None
    try:
        while process.poll() is None or any(thread.is_alive() for thread in threads):
            if overflow.is_set():
                reason = "output_limit"
                break
            if time.monotonic() - start >= timeout:
                reason = "timeout"
                break
            time.sleep(0.02)
    finally:
        stop_owned(process)
        process.wait(timeout=5)
        for thread in threads:
            thread.join(timeout=5)
    if overflow.is_set():
        reason = "output_limit"
    return {"exitCode": process.returncode, "failure": reason,
            "seconds": round(time.monotonic() - start, 3),
            "stdout": bytes(buffers[0]).decode("utf-8", errors="replace"),
            "stderr": bytes(buffers[1]).decode("utf-8", errors="replace")}


def recipe(seconds):
    require(isinstance(seconds, int) and 2 <= seconds <= 2400, "duration must be 2..2400 seconds")
    return ["-hide_banner", "-v", "error", "-nostdin", "-n", "-filter_threads", "1",
            "-filter_complex_threads", "1", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25",
            "-f", "lavfi", "-i", "sine=frequency=997:sample_rate=48000", "-t", str(seconds),
            "-map_metadata", "-1", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264",
            "-preset", "veryfast", "-threads:v", "1", "-pix_fmt", "yuv420p", "-b:v", "96k",
            "-maxrate", "128k", "-bufsize", "256k", "-g", "50", "-keyint_min", "50",
            "-sc_threshold", "0", "-bf", "2", "-c:a", "aac", "-threads:a", "1", "-b:a", "32k",
            "-ac", "1", "-flvflags", "no_metadata", "-fs", str(MAX_BYTES), "-f", "flv"]


def inspect_framing(path, max_bytes=MAX_BYTES, max_tags=MAX_TAGS):
    """Independent fixed-field walk with constant-size retained metadata."""
    size = path.stat().st_size
    require(13 <= size <= max_bytes, "fixture size outside budget")
    counts, first, last = Counter(), {}, {}
    key_count = 0
    key_after_retention = None
    count, max_dts, final = 0, 0, None
    with path.open("rb") as source:
        header = source.read(13)
        require(header[:9] == bytes.fromhex("464c56010500000009") and header[9:] == b"\0" * 4,
                "unexpected FLV header")
        offset = 13
        while offset < size:
            require(count < max_tags, "Tag count exceeds budget")
            source.seek(offset)
            raw = source.read(11)
            require(len(raw) == 11, "truncated Tag header")
            data_size = int.from_bytes(raw[1:4], "big")
            dts = int.from_bytes(raw[4:7], "big") | raw[7] << 24
            require(offset + 15 + data_size <= size, "truncated payload/trailer")
            require(raw[0] in (8, 9) and raw[8:] == b"\0" * 3, "unexpected Tag type/flags/stream")
            prefix = source.read(min(data_size, 5))
            require(len(prefix) >= 2, "missing codec packet header")
            if raw[0] == 8:
                require(prefix[0] >> 4 == 10 and prefix[1] in (0, 1), "expected AAC")
                kind = "aacConfig" if prefix[1] == 0 else "aacRaw"
            else:
                require(prefix[0] & 15 == 7 and prefix[1] in (0, 1, 2) and len(prefix) == 5, "expected AVC")
                kind = ("avcConfig", "avcNALU", "avcEOS")[prefix[1]]
            row = {"tagId": count, "offset": offset, "payloadOffset": offset + 11,
                   "dataSize": data_size, "dts": dts, "prefixHex": prefix.hex()}
            counts[kind] += 1
            first.setdefault(kind, row)
            last[kind] = row
            if kind == "avcNALU" and prefix[0] >> 4 == 1:
                key_count += 1
                if key_count == 1025:
                    key_after_retention = row
            source.seek(offset + 11 + data_size)
            require(int.from_bytes(source.read(4), "big") == data_size + 11, "PreviousTagSize mismatch")
            max_dts, final = max(max_dts, dts), row
            count += 1
            offset += data_size + 15
    require(path.stat().st_size == size, "source size changed during validation")
    return {"tagCount": count, "scanComplete": True, "counts": dict(counts), "maxDTS": max_dts,
            "tailTag": final, "first": first, "last": last, "declaredKeyframes": key_count,
            "firstKeyframeBeyondDesktopRetention": key_after_retention}


def validate_duration(facts, seconds):
    counts = facts["counts"]
    require(counts.get("avcNALU") == seconds * 25, "video frame count differs from recipe")
    require(counts.get("aacConfig") == 1 and counts.get("avcConfig") == 1 and counts.get("avcEOS") == 1,
            "codec configuration/EOS count differs from recipe")
    require(abs(counts.get("aacRaw", 0) - seconds * 48000 / 1024) <= 3,
            "audio packet coverage differs from recipe")
    require(facts["declaredKeyframes"] == math.ceil(seconds * 25 / 50), "fixed-GOP count differs from recipe")
    require(abs(facts["maxDTS"] - seconds * 1000) <= 200, "DTS coverage differs from recipe")


def generate(ffmpeg, output_dir, seconds=2400, timeout=600):
    require(output_dir.is_absolute(), "output directory must be absolute")
    require(1 <= timeout <= 600, "deadline must be 1..600 seconds")
    command_recipe = recipe(seconds)
    executable = ffmpeg.resolve(strict=True)
    require(executable.is_file(), "FFmpeg must be a file")
    directory = output_dir.resolve()
    directory.mkdir(parents=True, exist_ok=False)
    path = directory / f"generated-long-avc-aac-{seconds}s.flv"
    plan = {"tool": "MuxRay long_media", "toolVersion": VERSION,
            "createdAt": datetime.now(timezone.utc).isoformat(), "originClass": "generated",
            "notRealRecording": True, "durationSeconds": seconds, "timeoutSeconds": timeout,
            "maxBytes": MAX_BYTES, "maxTags": MAX_TAGS,
            "generatorSourceSHA256": sha256(Path(__file__)),
            "ffmpeg": str(executable), "ffmpegSHA256": sha256(executable),
            "argv": [str(executable), *command_recipe, str(path)],
            "fileName": path.name}
    write_json(directory / "generation-plan.json", plan)
    try:
        version = bounded_run([executable, "-version"], 10)
        write_json(directory / "ffmpeg-version.json", version)
        require(version["exitCode"] == 0 and version["failure"] is None, "FFmpeg version failed")
        run = bounded_run(plan["argv"], timeout)
        write_json(directory / "generation-process.json", run)
        require(run["exitCode"] == 0 and run["failure"] is None, "FFmpeg failed; partial output retained")
        facts = inspect_framing(path)
        validate_duration(facts, seconds)
        result = {"toolVersion": VERSION, "originClass": "generated", "notRealRecording": True,
                  "file": {"name": path.name, "sizeBytes": path.stat().st_size, "sha256": sha256(path)},
                  "ffmpegVersion": version["stdout"].splitlines()[0], "facts": facts,
                  "elapsedSeconds": run["seconds"], "generationPlanSHA256": sha256(directory / "generation-plan.json")}
        write_json(directory / "generation-result.json", result)
        return result
    except Exception as exc:
        write_json(directory / "generation-failure.json", {"errorType": type(exc).__name__, "message": str(exc),
                                                          "partialArtifactRetained": path.exists()})
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ffmpeg", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--duration-seconds", type=int, default=2400)
    parser.add_argument("--timeout-seconds", type=int, default=600)
    args = parser.parse_args()
    print(json.dumps(generate(args.ffmpeg, args.output_dir, args.duration_seconds, args.timeout_seconds), indent=2))


if __name__ == "__main__":
    main()

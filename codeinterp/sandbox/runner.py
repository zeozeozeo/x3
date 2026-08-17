#!/usr/bin/env python3
import base64
import json
import mimetypes
import os
import pathlib
import subprocess
import sys
import tempfile

MAX_CAPTURE = 64 * 1024
MAX_FILE = 8 * 1024 * 1024
MAX_TOTAL = 8 * 1024 * 1024


def clipped(value: bytes) -> str:
    if len(value) <= MAX_CAPTURE:
        return value.decode("utf-8", "replace")
    return value[:MAX_CAPTURE].decode("utf-8", "replace") + "\n[output truncated]"


def artifacts() -> list[dict]:
    result = []
    total = 0
    for path in sorted(pathlib.Path("/work").iterdir()):
        if not path.is_file() or path.is_symlink():
            continue
        size = path.stat().st_size
        if size > MAX_FILE or total + size > MAX_TOTAL:
            continue
        data = path.read_bytes()
        total += len(data)
        result.append({
            "name": path.name,
            "content_type": mimetypes.guess_type(path.name)[0] or "application/octet-stream",
            "data": base64.b64encode(data).decode("ascii"),
        })
    return result


def main() -> None:
    language = sys.argv[1] if len(sys.argv) > 1 else ""
    command = {
        "python": ["python3", "-I", "-"],
        "lua": ["lua5.4", "-"],
        "javascript": ["node", "--input-type=commonjs", "-"],
    }.get(language)
    if command is None:
        raise SystemExit("unsupported language")
    code = sys.stdin.buffer.read()
    with tempfile.TemporaryFile(dir="/tmp") as stdout, tempfile.TemporaryFile(dir="/tmp") as stderr:
        proc = subprocess.run(command, input=code, cwd="/work", stdout=stdout, stderr=stderr, check=False)
        stdout.seek(0)
        stderr.seek(0)
        captured_stdout = stdout.read(MAX_CAPTURE + 1)
        captured_stderr = stderr.read(MAX_CAPTURE + 1)
    json.dump({
        "stdout": clipped(captured_stdout),
        "stderr": clipped(captured_stderr),
        "exit_code": proc.returncode,
        "artifacts": artifacts(),
    }, sys.stdout, separators=(",", ":"))


if __name__ == "__main__":
    main()

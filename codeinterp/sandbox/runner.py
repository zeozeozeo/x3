#!/usr/bin/env python3
import base64
import json
import mimetypes
import os
import signal
import stat
import subprocess
import sys
import tempfile

MAX_CAPTURE = 64 * 1024
MAX_FILE = 8 * 1024 * 1024
MAX_TOTAL = 8 * 1024 * 1024
MAX_FILES = 32
UNTRUSTED_UID = 65534
UNTRUSTED_GID = 65534


def clipped(value: bytes) -> str:
    if len(value) <= MAX_CAPTURE:
        return value.decode("utf-8", "replace")
    return value[:MAX_CAPTURE].decode("utf-8", "replace") + "\n[output truncated]"


def artifacts() -> list[dict]:
    result = []
    total = 0
    work_fd = os.open("/work", os.O_RDONLY | os.O_DIRECTORY)
    try:
        names = sorted(os.listdir(work_fd))
        for name in names:
            if len(result) >= MAX_FILES:
                break
            if not name or name in (".", "..") or len(os.fsencode(name)) > 255:
                continue
            try:
                fd = os.open(name, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW, dir_fd=work_fd)
            except OSError:
                continue
            try:
                info = os.fstat(fd)
                if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_FILE or total + info.st_size > MAX_TOTAL:
                    continue
                data = os.read(fd, MAX_FILE + 1)
            finally:
                os.close(fd)
            if len(data) > MAX_FILE or total + len(data) > MAX_TOTAL:
                continue
            total += len(data)
            result.append({
                "name": name,
                "content_type": mimetypes.guess_type(name)[0] or "application/octet-stream",
                "data": base64.b64encode(data).decode("ascii"),
            })
    finally:
        os.close(work_fd)
    return result


def main() -> None:
    language = sys.argv[1] if len(sys.argv) > 1 else ""
    command = {
        "python": ["python3", "-I", "-"],
        "lua": ["lua5.4", "-"],
        "javascript": ["bun", "run", "-"],
        "typescript": ["bun", "run", "-"],
        "ruby": ["ruby", "-"],
        "php": ["php"],
        "perl": ["perl", "-"],
        "shell": ["bash", "--noprofile", "--norc", "-s"],
    }.get(language)
    if command is None:
        raise SystemExit("unsupported language")
    code = sys.stdin.buffer.read()
    # Some runtimes (notably older runsc releases) ignore uid=/gid= on tmpfs
    # mount options, leaving /work unusable for the untrusted user. Enforce it
    # here while still trusted root; harmless if the mount already honors it.
    os.chmod("/work", 0o700)
    os.chown("/work", UNTRUSTED_UID, UNTRUSTED_GID)
    with tempfile.TemporaryFile(dir="/tmp") as stdout, tempfile.TemporaryFile(dir="/tmp") as stderr:
        proc = subprocess.Popen(
            command,
            cwd="/work",
            stdin=subprocess.PIPE,
            stdout=stdout,
            stderr=stderr,
            start_new_session=True,
            user=UNTRUSTED_UID,
            group=UNTRUSTED_GID,
            extra_groups=[],
        )
        proc.communicate(code)
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
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

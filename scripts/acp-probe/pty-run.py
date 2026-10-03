#!/usr/bin/env python3
"""Run argv in a real PTY, mirroring its screen bytes to a log file (spec 492).

    pty-run.py <logfile> <cols> <rows> -- <argv...>

Used by probe.mjs for the "reopen in a Hive PTY" check. Node has no PTY in its
stdlib, and macOS script(1) refuses a piped stdin ("tcgetattr: Operation not
supported on socket"), so this is the smallest stdlib-only PTY host. Bytes on
our stdin are forwarded to the child (the probe answers first-run dialogs that
way); EOF on stdin is ignored so the child never sees a hang-up.
"""
import fcntl
import os
import pty
import select
import struct
import sys
import termios


def main() -> int:
    if len(sys.argv) < 6 or sys.argv[4] != "--":
        sys.stderr.write(__doc__)
        return 2
    log, cols, rows, argv = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[5:]
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = os.environ.get("TERM", "xterm-256color")
        try:
            os.execvp(argv[0], argv)
        except OSError as e:
            sys.stderr.write(f"pty-run: exec {argv[0]}: {e}\n")
            os._exit(127)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    stdin_open = True
    with open(log, "ab", buffering=0) as out:
        while True:
            watch = [fd] + ([0] if stdin_open else [])
            ready, _, _ = select.select(watch, [], [], 0.5)
            if fd in ready:
                try:
                    data = os.read(fd, 65536)
                except OSError:  # EIO: child side closed
                    break
                if not data:
                    break
                out.write(data)
            if 0 in ready:
                data = os.read(0, 4096)
                if data:
                    os.write(fd, data)
                else:
                    stdin_open = False
    _, status = os.waitpid(pid, 0)
    return os.waitstatus_to_exitcode(status) if hasattr(os, "waitstatus_to_exitcode") else 0


if __name__ == "__main__":
    sys.exit(main())

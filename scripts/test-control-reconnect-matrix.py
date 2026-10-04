#!/usr/bin/env python3
"""Run current control recovery tests against the actual older peer sources.

All checkouts, certificates and SQLite data are temporary; no production service
or network endpoint is used. The test exercises control packages, not full apps.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


def extract(ref, destination, subtree=None):
    args = ["archive", ref]
    if subtree:
        args.append(subtree)
    subprocess.run(["tar", "-xf", "-", "-C", str(destination)],
                   input=git(*args), check=True)


def main():
    changed = git("diff", "--name-only", "-z").split(b"\0")
    changed += git("ls-files", "--others", "--exclude-standard", "-z").split(b"\0")
    env = dict(os.environ, GOCACHE=str(ROOT / ".cache/go-build"))
    matrix = [("master", "e600698"), ("node", "601fdda")]
    for role, ref in matrix:
        with tempfile.TemporaryDirectory(prefix="mirror-control-matrix-") as temp:
            work = Path(temp)
            extract("HEAD", work)
            for raw in changed:
                if not raw:
                    continue
                relative = Path(os.fsdecode(raw))
                source, target = ROOT / relative, work / relative
                if source.is_file():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(source, target)
                elif target.is_file():
                    target.unlink()
            subtree = f"internal/{role}/control"
            shutil.rmtree(work / subtree)
            extract(ref, work, subtree)
            # Give the historical peer its own actual queue/reader sources.
            # Otherwise both peers would silently use the repaired shared
            # reader, masking the 601fdda eight-message inbound limit.
            with tempfile.TemporaryDirectory(prefix="mirror-old-queue-") as shared:
                extract(ref, shared, "internal/controlv2")
                target = work / "internal/compatpeer/controlv2"
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copytree(Path(shared) / "internal/controlv2", target)
            for source in (work / subtree).glob("*.go"):
                source.write_text(source.read_text().replace(
                    '"mirror-server/internal/controlv2"',
                    '"mirror-server/internal/compatpeer/controlv2"'))
            # Reuse the same assertions, input data and injected disconnect for
            # both version combinations. All peer code comes from git archive.
            test = Path("internal/master/control/v2_reconnect_e2e_test.go")
            shutil.copy2(ROOT / test, work / test)
            print(f"\nActual {role} control sources: {ref}; other peer: current repair", flush=True)
            subprocess.run(["go", "test", "./internal/master/control", "-run",
                            "TestV2EndToEnd", "-count=1", "-v"],
                           cwd=work, env=env, check=True, timeout=150)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""A guard must fail when its own detection stops working.

    python3 scripts/check-guards-blind.py

THE FAILURE THIS CATCHES, and why the existing lanes do not. selftest-checks.py
breaks the TREE and requires the guard to fire, which proves the guard catches a
violation. vacuity_audit() requires the expected output not to be something the
guard prints anyway, which proves the arm discriminates. Neither asks the third
question: if the guard's own pattern stops matching — a reformat, a rename, a
regex edited one character too far — does it fail, or does it scan nothing, find
nothing, and report success?

Measured when this was written: nineteen guards failed loudly, two did not.
check-tdz and check-mark-font both exited 0 with their detection blinded, and
check-tdz is the only thing standing between a reordered declaration and a blank
page — the browser enforces that ordering and no source harness can see it. A
version that silently stops looking is worse than none, because the suite goes on
reporting the ordering as verified. Both now floor their census; this is what
stops the next one regressing the same way.

HOW IT WORKS. For each guard with a module-level `NAME = re.compile(r"...")`,
rewrite that one pattern to something that cannot match, run the guard, and
require a non-zero exit. The edit is made on a COPY under a temporary directory;
the tree is never written to, which matters because an earlier hand-run of this
same idea left a guard blinded in the working tree when it timed out.

GUARDS WITH NO SINGLE NAMED PATTERN are reported, not failed. Several build
their matcher inline or hold several equal patterns, and blinding one of those
proves nothing either way.
"""
import io
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
SCRIPTS = ROOT / "scripts"

# Guards this cannot run: they need a chain, a gno toolchain, or they rewrite the
# tree themselves. Named with the reason so the list cannot quietly grow.
SKIP = {
    "check-live-reads": "queries a running node",
    "check-isolation": "runs the realm suite once per test, tens of minutes",
    "check-mutation-scope": "drives mutate.py over the corpus",
    "check-mutant-collisions": "drives mutate.py over the corpus",
    "check-guards-blind": "this file",
}

PATTERN = re.compile(r"^([A-Z][A-Z_0-9]*)\s*=\s*re\.compile\((r['\"])", re.M)
TIMEOUT = 180


def blind(src, sym):
    """Replace one named pattern's regex with one that cannot match."""
    return re.sub(r"^(%s\s*=\s*re\.compile\()r(['\"]).*?\2" % re.escape(sym),
                  lambda m: m.group(1) + 'r"ZZ_BLINDED_NEVER_MATCHES_ZZ"',
                  src, count=1, flags=re.M | re.S)


def main():
    quiet, loud, skipped, nopat, slow = [], [], [], [], []
    work = tempfile.mkdtemp(prefix="kourt-blind-")
    try:
        for p in sorted(SCRIPTS.glob("check-*.py")):
            name = p.stem
            if name in SKIP:
                skipped.append(name)
                continue
            src = io.open(p, encoding="utf-8").read()
            m = PATTERN.search(src)
            if not m:
                nopat.append(name)
                continue
            out = blind(src, m.group(1))
            if out == src:
                nopat.append(name)
                continue
            # A COPY, ALWAYS. The guard is run from the repo so its relative
            # paths still resolve, but the file it runs is the blinded copy in
            # the temp dir — the tree is never modified, so a timeout here
            # cannot leave a guard disarmed behind us.
            tmp = os.path.join(work, name + ".py")
            io.open(tmp, "w", encoding="utf-8").write(out)
            try:
                rc = subprocess.run([sys.executable, tmp], cwd=str(ROOT),
                                    capture_output=True, timeout=TIMEOUT).returncode
            except subprocess.TimeoutExpired:
                slow.append(name)
                continue
            (quiet if rc == 0 else loud).append("%s (%s)" % (name, m.group(1)))
    finally:
        shutil.rmtree(work, ignore_errors=True)

    for name in slow:
        print("  %-34s timed out at %ds; blinding it proves nothing"
              % (name, TIMEOUT), file=sys.stderr)

    if quiet:
        print("check-guards-blind: %d guard(s) report success with their own detection "
              "blinded.\n" % len(quiet), file=sys.stderr)
        for n in quiet:
            print("  %s" % n, file=sys.stderr)
        print("\nEach scans nothing, finds nothing, and exits 0. Floor the census the way\n"
              "check-spend-paths.py does — zero sites means the pattern broke, not that\n"
              "the tree is clean.", file=sys.stderr)
        return 1

    print("check-guards-blind: %d guard(s) fail when blinded, %d have no single named "
          "pattern to blind, %d skipped." % (len(loud), len(nopat), len(skipped)))
    if not loud:
        print("check-guards-blind: blinded no guard at all, so this check is scanning "
              "for a shape scripts/ no longer has.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

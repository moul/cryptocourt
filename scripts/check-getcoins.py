#!/usr/bin/env python3
"""A balance read whose cost a stranger chooses is a bomb with someone else's timer.

    python3 scripts/check-getcoins.py

THE RULE. Reading one balance is `banker.GetCoin(addr, denom)`. `GetCoins(addr)`
reads EVERY denom the address holds, and anyone may send any address a new denom
without its consent -- so the cost of the read is set by a third party, not by
this realm. On any address an outsider can reach, that is a permanent
out-of-gas waiting for somebody to decide it should happen.

THIS IS NOT HYPOTHETICAL AND IT IS NOT HISTORY: it is live on gnoland-1 right
now. The deployed buy.gno reads

    b.GetCoins(chain.PackageAddress(burnSinkPath)).AmountOf(gnotDenom)

from BurnedGNOT(), which renderAdminParams() calls, which Render() reaches for
the `admin-params` page. And BurnSink() publishes that address on purpose, so
anybody can reconcile the burn -- which also tells an attacker precisely where
to send junk denoms to make the page cost more to read every time. The repo was
fixed to GetCoin; a package deploys exactly once, so the fix cannot reach that
path. This guard exists so the version people CAN still fix does not drift back.

TESTS ARE EXEMPT, and the distinction is the whole point rather than
convenience: a test picks its own addresses, so no stranger sets the cost. What
makes GetCoins dangerous is reachability by someone who wants it to hurt.

THE CENSUS COUNTS THE SAFE FORM, not the dangerous one. Counting GetCoins could
not tell "the realm is clean" from "the scan broke" -- both read zero. GetCoin
must be non-zero for the scan to have looked at anything at all.
"""
import io
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
REALM = ROOT / "realm"

# BANNED first: check-guards-blind blinds the first named pattern, and with this
# one blinded nothing is ever flagged -- so the census floor below is what has to
# turn that into a failure.
BANNED = re.compile(r"\bGetCoins\s*\(")
SAFE = re.compile(r"\bGetCoin\s*\(")
CENSUS_FLOOR = 1


def strip_comments(src):
    """Comment text out, code left in place, string literals respected.

    buy.gno documents the rule in a comment that NAMES GetCoins. Without this the
    guard would flag the sentence explaining why the code does not do the thing.
    """
    out, i, n = [], 0, len(src)
    while i < n:
        c = src[i]
        if c in "\"'`":
            q = c
            out.append(c)
            i += 1
            while i < n:
                if src[i] == "\\" and q != "`":
                    out.append("  ")
                    i += 2
                    continue
                out.append(src[i])
                if src[i] == q:
                    i += 1
                    break
                i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            out.append("\n" * src[i:j].count("\n"))
            i = j
            continue
        out.append(c)
        i += 1
    return "".join(out)


def main():
    safe_seen, bad = 0, []
    for p in sorted(REALM.rglob("*.gno")):
        if p.name.endswith(("_test.gno", "_filetest.gno")):
            continue
        text = strip_comments(io.open(p, encoding="utf-8").read())
        rel = str(p.relative_to(REALM))
        for lineno, line in enumerate(text.split("\n"), 1):
            if BANNED.search(line):
                bad.append((rel, lineno, line.strip()[:90]))
            # GetCoins also matches GetCoin, so count only what is not banned
            elif SAFE.search(line):
                safe_seen += 1

    if bad:
        print("check-getcoins: %d balance read(s) whose cost a third party sets.\n"
              % len(bad), file=sys.stderr)
        for rel, lineno, src in bad:
            print("  %s:%d\n      %s" % (rel, lineno, src), file=sys.stderr)
        print("\nUse GetCoin(addr, denom). GetCoins walks every denom the address holds,\n"
              "and anyone can give an address a denom it never asked for. The deployed\n"
              "buy.gno still does this on the published burn sink -- see this file's\n"
              "header for what that costs.", file=sys.stderr)
        return 1

    if safe_seen < CENSUS_FLOOR:
        print("check-getcoins: found no balance reads at all, so this check is scanning\n"
              "for a shape the realm no longer has. Zero GetCoins means nothing when\n"
              "zero GetCoin means the scan never looked.", file=sys.stderr)
        return 1

    print("check-getcoins: %d balance read(s), every one naming its denom." % safe_seen)
    return 0


if __name__ == "__main__":
    sys.exit(main())

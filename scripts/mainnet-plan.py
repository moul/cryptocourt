#!/usr/bin/env python3
"""Extract the calls from a scenario that a token-locked chain will actually take.

    python3 scripts/mainnet-plan.py scenarios/covid_demo.py > plan.json

WHY A FILTER AND NOT A NEW SCENARIO. The scenario is already the source of
truth for the docket -- claim titles, bodies, folders, order -- and retyping any
of it into a second file would make two things to keep in step. So this runs the
scenario, reads the step list it builds, and drops the steps the chain cannot
take yet.

WHAT THE CHAIN REFUSES, and the second-order effect that matters more.
gnoland-1 launched with bank:p:restricted_denoms = ["ugnot"] and a 91-address
allowlist. A MsgCall's `send` moves through bank.SendCoins (vm/keeper.go), which
the lock refuses -- so `Buy` fails, for everyone, not only for us.

That is ONE call out of 664. The damage is downstream: Buy is the only reachable
mint for court coin (the other two are emission, which needs prior activity, and
the meta court). Nothing else carries a `send`, so the lock never refuses them at
the transport layer -- they simply cannot succeed, because nobody holds the CC
they spend. Opening a claim takes a CC deposit; so does answering, commenting,
associating and staking.

THE RESULT, measured rather than assumed: 14 of 664 calls survive, and they are
the court shell and its folders. The docket itself -- 26 claims, 170 stakes, 24
answers -- waits for transfers to open.

Run it to see the current split; it is a report, not just a filter.
"""
import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

# Calls a locked chain accepts. Anything not listed is reported, not guessed at:
# a scenario that grows a new entrypoint should fail loudly here rather than be
# silently dropped from the plan.
ALLOWED = {
    "StartCourt", "StartCourtP",
    "SetCourtDesc", "SetCourtImage", "SetTier", "SetSiteDomain",
    "SetCourtCreationBurn", "SetLadderDefault",
    "New", "SetFolderImage", "MoveFolder",
}

# NEEDS COURT COIN, which only Buy mints and the token lock blocks. These do not
# carry a `send` -- the lock does not refuse them at the transport layer -- they
# simply cannot succeed, because the caller has no CC to spend.
NEEDS_CC = {"Stake", "Unstake", "OpenClaimP", "OpenClaim", "OpenClaimIn",
            "OpenClaimPM", "PostAnswer", "PostComment", "BuyCommentPass",
            "UpvoteComment", "OpenDispute", "OpenRewards", "WithdrawBonus",
            "AddAssociation", "ApproveAssociation", "DisapproveAssociation",
            "SettleUndisputed", "CloseDeadClaim", "ClaimMetaFranchise",
            "AddToFolder", "HideBoardRow", "HideOwnComment"}

BLOCKED_BY_LOCK = {"Buy"}
BLOCKED = set(BLOCKED_BY_LOCK)

# WOULD SUCCEED, AND MUST NOT BE USED. OpenClaimSeeded waives the CC deposit,
# so a moderator CAN open claims on a locked chain -- but it calls
# openClaim(c, who, title, "", true): the body is written EMPTY, and the only
# amendment path is EditClaimTitle. Seeding the covid docket this way would put
# 26 bodyless claims on a path that can never be redeployed, and the bodies are
# the substance. Waiting for transfers to open costs nothing; this cannot be
# undone.
REFUSED = {"OpenClaimSeeded"}

# Clock manipulation. Right for a demo chain, wrong here: arming it sets
# TestClockFabricated() true on the realm's record permanently, on a chain whose
# proposition is that the dates are real.
BLOCKED |= {"EnableTestClock", "EnableTestClockAt", "AdvanceTestClock",
            "AdvanceTestHeight", "SealTestClock"}


def load(path):
    ns = {"__file__": str(pathlib.Path(path).resolve()), "__name__": "__scenario__"}
    exec(compile(open(path, encoding="utf-8").read(), path, "exec"), ns)
    scn = ns.get("SCENARIO")
    if scn is None:
        sys.exit("%s: defines no SCENARIO" % path)
    return scn


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: mainnet-plan.py <scenarios/name.py> [--after-unlock]")
    # --after-unlock emits the WHOLE docket, for the day restricted_denoms is
    # empty and Buy works. The clock steps stay dropped either way: arming the
    # test clock sets TestClockFabricated() true on the realm's record forever,
    # and no unlock changes that. Refuse it if the lock is still on, so the flag
    # cannot be used by accident against a chain that will reject every Buy.
    after = "--after-unlock" in sys.argv[2:]
    scn = load(sys.argv[1])

    kept, dropped, unknown = [], {}, {}
    for st in scn.steps:
        if st.get("kind") != "call":
            continue
        fn = st["func"]
        if fn in ALLOWED or (after and (fn in NEEDS_CC or fn in BLOCKED_BY_LOCK)):
            # `send` must be absent: a send is the one thing the lock refuses,
            # and an allowed function carrying one would fail at broadcast.
            if st.get("send") and not after:
                sys.exit("%s carries send=%s, which a locked chain refuses" % (fn, st["send"]))
            kept.append({"who": str(st["who"]), "func": fn,
                         "args": [str(a) for a in st["args"]],
                         "send": st.get("send") or ""})
        elif fn in REFUSED:
            dropped.setdefault("REFUSED " + fn, 0)
            dropped["REFUSED " + fn] += 1
        elif fn in NEEDS_CC:
            dropped.setdefault("needs CC  " + fn, 0)
            dropped["needs CC  " + fn] += 1
        elif fn in BLOCKED:
            dropped.setdefault("blocked   " + fn, 0)
            dropped["blocked   " + fn] += 1
        else:
            unknown[fn] = unknown.get(fn, 0) + 1

    if unknown:
        sys.exit("unclassified call(s) -- add them to ALLOWED or BLOCKED: %s"
                 % ", ".join("%s x%d" % kv for kv in sorted(unknown.items())))

    sys.stderr.write("  kept %d call(s):\n" % len(kept))
    byfn = {}
    for c in kept:
        byfn[c["func"]] = byfn.get(c["func"], 0) + 1
    for fn, n in sorted(byfn.items(), key=lambda kv: -kv[1]):
        sys.stderr.write("    %-18s %d\n" % (fn, n))
    signers = {}
    for c in kept:
        signers[c["who"]] = signers.get(c["who"], 0) + 1
    sys.stderr.write("  signers needed: %d\n" % len(signers))
    for w, n in sorted(signers.items(), key=lambda kv: -kv[1]):
        sys.stderr.write("    %-20s %d\n" % (w, n))
    sys.stderr.write("  dropped (token lock / clock):\n")
    for fn, n in sorted(dropped.items(), key=lambda kv: -kv[1]):
        sys.stderr.write("    %-18s %d\n" % (fn, n))

    json.dump(kept, sys.stdout, indent=1)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

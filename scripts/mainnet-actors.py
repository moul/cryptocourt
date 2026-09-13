#!/usr/bin/env python3
"""Fill in the chain's half of the actors file, and say what still needs funding.

    minsign addrs plan.json > addrs.json          # offline: seed -> addresses
    python3 scripts/mainnet-actors.py addrs.json > actors.json   # online
    minsign plan plan.json actors.json ...        # offline: sign

WHY THIS IS A SEPARATE STEP AND NOT A FLAG. Only the seed can say which address
an index derives, and only the chain can say what account number that address
was given — and the machine holding the seed is not on a network. So the two
halves meet in a file that crosses the air gap, and this is the online half.

AN ACCOUNT NUMBER DOES NOT EXIST UNTIL THE ADDRESS HAS COINS. gnoland-1 assigns
one when an account is first funded, so running this before funding reports
every actor as absent — which is the point: it is the funding checklist.

A SEQUENCE READ HERE IS A CLAIM ABOUT THE FUTURE. It is correct only while
nothing else sends from these addresses between now and the broadcast. That is
the same assumption the deploy made, and the same way it can be void: if
anything lands first, re-run this and re-sign. A wrong sequence costs a rejected
broadcast, not money.
"""
import json
import base64
import sys
import urllib.request

RPC = "https://rpc.gno.land"


def query(path):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "abci_query",
                       "params": {"path": path}}).encode()
    req = urllib.request.Request(RPC, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)["result"]["response"]["ResponseBase"]


def account(addr):
    """Returns (account_number, sequence, coins) or None if the chain has no
    such account — which is what an unfunded address looks like."""
    r = query("auth/accounts/%s" % addr)
    if r.get("Error") or not r.get("Data"):
        return None
    raw = base64.b64decode(r["Data"]).decode()
    if not raw.strip() or raw.strip() == "null":
        return None
    a = json.loads(raw)["BaseAccount"]
    return int(a["account_number"]), int(a["sequence"]), a.get("coins") or "0ugnot"


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: mainnet-actors.py <addrs.json>")
    actors = json.load(open(sys.argv[1], encoding="utf-8"))

    missing = []
    for name in sorted(actors, key=lambda n: actors[n]["index"]):
        a = actors[name]
        got = account(a["address"])
        if got is None:
            missing.append((name, a["address"]))
            sys.stderr.write("  %-16s index=%-3d UNFUNDED  %s\n"
                             % (name, a["index"], a["address"]))
            continue
        a["account_number"], a["sequence"], coins = got
        sys.stderr.write("  %-16s index=%-3d acct=%-9d seq=%-4d %s\n"
                         % (name, a["index"], a["account_number"], a["sequence"], coins))

    if missing:
        sys.stderr.write("\n  %d actor(s) hold nothing, so the chain has given them no\n"
                         "  account number and nothing they sign can be accepted:\n" % len(missing))
        for name, addr in missing:
            sys.stderr.write("    %-16s %s\n" % (name, addr))
        sys.stderr.write("  Fund them, then run this again.\n")
        # Still write the file: the addresses in it are what the funding step needs.
        json.dump(actors, sys.stdout, indent=1, sort_keys=True)
        sys.stdout.write("\n")
        return 1

    json.dump(actors, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write("\n")
    sys.stderr.write("  all %d actor(s) funded and numbered\n" % len(actors))
    return 0


if __name__ == "__main__":
    sys.exit(main())

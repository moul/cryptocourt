# tools/

The pieces that put a realm on a chain the deploying key cannot reach directly.

Each file's header carries its own reasoning — why it exists, what it refuses,
what was measured. This page is only the **order**, because the order is the one
thing no single file can state.

## Why any of this exists

The address holding the funds on `gnoland-1` is a Ledger, and a Ledger cannot
sign an `addpkg`: the transport gives out around 8,840 bytes and the smallest
package is 8,924. Stock `gnokey` cannot run on the machine that *can* sign,
because it links `crypto/x509`, which binds a macOS 12 symbol at load time.

So the key lives on an air-gapped machine, and what crosses the gap is a signed
transaction — never a key, never a mnemonic.

## Deploying packages

```
scripts/retarget.sh <namespace> <keyname> [realm-name]   # rewrite import paths
go run ./tools/stripcomments <build>/r/<realm>           # fit the 1MB RPC limit
tools/minsign/build.sh                                   # build the signer
tools/minsign/verify.sh <binary> <pkgdir>                # prove it matches gnokey
                                                         #   -- carry to the offline box --
NS=<namespace> ACCNUM=<n> tools/bundle/1-sign-OFFLINE.sh
                                                         #   -- carry signatures back --
tools/bundle/2-broadcast-ONLINE.sh
```

`retarget.sh` refuses a dirty source tree. That is not fussiness: a package path
can be deployed exactly once, and an uncommitted edit has already reached
mainnet this way.

`stripcomments` is not optional. The tx is base64 on the wire, which inflates by
a third against a 1,000,000-byte RPC body limit — so the realm's source has to
stay under about 750KB, and it has been over 900KB since well before the first
deploy. Stripping comments takes roughly two thirds off and it fits with room.

(Those are measurements, not invariants: the realm grew 2KB in the day after
this file was written. Run the stripper and read what it prints — it reports the
base64 size and the limit on every run, so the check is never out of date.)

It tokenises rather than pattern-matching, and asserts the non-comment token
streams are unchanged.

**Run `verify.sh` before trusting a `minsign` binary.** It is the only reason to
trust one: these signatures are RFC6979-deterministic, so byte-identical output
against stock gnokey means identical cryptography. `build.sh` patches gno's own
source in three places to reach a Go old enough to target macOS 10.13, and no
amount of reading those patches substitutes for the comparison.

## Seeding a court

```
scripts/mainnet-plan.py <scenario> [--after-unlock] > plan.json
minsign addrs plan.json > addrs.json                     # offline: seed -> addresses
scripts/mainnet-actors.py addrs.json > actors.json       # online: + account numbers
                                                         #   -- fund those addresses --
scripts/mainnet-actors.py addrs.json > actors.json       # again, now they exist
minsign plan plan.json actors.json <pkgpath> ...         # offline
tools/bundle/2-broadcast-ONLINE.sh
```

`mainnet-plan.py` reads the scenario and drops what the chain will not take. It
refuses `OpenClaimSeeded` even though that call would work: it writes the claim
body EMPTY and only the title can be amended afterwards, so it would put bodyless
claims on a path that can never be redeployed.

It also separates the calls that change **policy** from the ones that add data.
The covid scenario sets the site domain and puts court creation at 2 GNOT — both
ordinary steps on a demo chain, both decisions on a live realm, and both easy to
miss inside what reads like a data load. They stay in the plan; they are just
reported where you cannot walk past them.

`actors.json` must come from **the plan you are signing**. Indices are assigned
by first appearance, so the same name gets a different index — and a different
address — out of a different plan. `minsign plan` recomputes the order and
refuses on disagreement, because the alternative is discovering it at broadcast
with the whole batch to redo.

An account number does not exist until the address holds coins, which is why
`mainnet-actors.py` runs twice: the first pass is the funding checklist.

## What is not here

Anything that needs the mnemonic runs on the air-gapped machine and nowhere
else. No tool in this directory reads a key from a file, takes one as an
argument, or writes one anywhere.

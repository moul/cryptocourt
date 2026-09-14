# Deploying kourtv2 to a live chain

Written against the tree at the time of writing. Every claim here was read out of
the source, and the line references are so you can re-check them rather than
trust this file.

## Your address is not in the codebase, and must not be

There is no admin constant. `directoryAdmin` is a plain `var` (`directory.gno:17`)
and it is set exactly once, to the creator of the **first court**:

```go
// court.gno:862
if directoryAdmin == "" {
    directoryAdmin = admin
}
```

The global DAO then bootstraps from it (`moderation.gno:96`), and everything
admin-gated reads through `adminAddr()` (`adminparams.gno:48`), which returns the
DAO's admin once a DAO exists and `directoryAdmin` before that.

**So you become admin by signing the first `StartCourt`, not by anything in the
source.** Nothing needs editing to make `g1ecsuj0…` the admin.

## Two identities, captured at two different moments

They are independent, and both are permanent.

| identity | captured when | captured how | powers |
|---|---|---|---|
| **deployer** | `AddPackage` | `tcDeployer = unsafe.OriginCaller()` at package init (`testclock.gno:153`) | the test clock, for ever — `mustDeployer` gates every write (`testclock.gno:158`) |
| **directory admin** | first `StartCourt` | the caller becomes `directoryAdmin` | the global DAO: pricing, moderation, parameters |

You are right that the AddPackage address is known during `init()` — the realm
already relies on it, which is what the deployer row above is.

If you want both, **sign the AddPackage and the first `StartCourt` with the same
ledger key.** Nothing enforces that they match; the realm will happily end up with
one key owning the clock and another owning the DAO.

## Will the covid court exist after deploying?

**No.** Deploying the package creates zero courts.

There are five `init()` functions in the realm and none creates a court:
`boardmod.gno:44`, `court.gno:511`, `modvote.gno:93`, `posting.gno:89` are
invariant checks, and `testclock.gno:153` is the deployer capture. Courts exist
only after somebody sends `StartCourt` (`court.gno:778`) and then the claims,
stakes and answers on top.

## What it costs, which is not mainly the burn

The court-creation burn is **zero out of the box**:

```go
// courtburn.gno:43
const courtCreationBurnInit = int64(0)
```

It is a `var` initialised from that constant and changed later by
`SetCourtCreationBurn` (`courtburn.gno:59`), which only the DAO admin may call.
The 2 GNOT you may remember is a value an admin set on the existing deployment,
not a default you inherit.

What actually costs GNOT:

1. **Gas on every transaction**, starting with the `AddPackage` of a large realm.
2. **Funding the actors.** This is the real constraint for a seeded demo.
   `scripts/seed-remote.sh` makes seventeen keys per run and its own header says
   why that matters: *"Balances come from nothing only at genesis; after the first
   block the routes are the faucet's 100-GNOT drip or a transfer from somebody who
   already holds coin."*

So: on a chain whose genesis you control, seed at genesis. On a chain already
running, you fund each actor from your own balance or accept a smaller scenario.
Having "enough GNOT" is necessary but it is not sufficient — the seventeen
addresses are generated per run, so they cannot be pre-funded from a runbook.

## Decide before you deploy, because these are write-once

- **The package path.** Derived addresses hash the path: the realm/escrow address,
  and the keyless burn sink. Change the path after deploy and they all move, and
  realms cannot be redeployed in place.
- **Whether the test clock ships at all.** It is part of the package. It is
  gated to the deployer and it can be retired with `SealTestClock`
  (`testclock.gno:304`) — but sealing is a transaction you have to remember to
  send. On a real chain, decide whether you want a time-manipulation facility
  present and sealed, or absent.

## Order of operations

1. `AddPackage` the realm, **signed by the ledger key**. This fixes the deployer,
   and with it the test clock, permanently.
2. `StartCourt` for the first court, **signed by the same ledger key**. This fixes
   `directoryAdmin`, and with it the global DAO admin, permanently.
3. Only then set prices — `SetCourtCreationBurn` and the rest are admin-gated and
   there is no admin until step 2 has happened.
4. Seal the test clock if you have decided to.
5. Seed the remaining courts and claims, funding actors as step 2 of "what it
   costs" above requires.

Steps 1 and 2 are the two that cannot be undone or reassigned.

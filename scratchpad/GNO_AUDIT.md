# Gno audit — kourt realms (security + gas)

Running state for the `/loop 1m` audit. Each tick appends verified findings and
advances the worklist. **Nothing here is a finding until its input→sink trace is
written down** (audit.md evidence-gating rule, confidence ≥80%).

## Provenance — READ THIS BEFORE QUOTING ANY RESULT

**This is a LOCAL WORKING TREE audit, not an audit of a deployment.**
`audit.md` requires deployed realms be read from their chain; kourt's gno side is
pre-deploy local source, so the rule that applies is the pasted-source one:
*as-provided, not verified against any deployment.*

- Repo: `/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod`
- Commit: `5d0970f` on `main`
- Tree state: 1 modified file under `realm/` (`kourtv2/testclock.gno`, another
  session's in-flight edit — audited as-is, flagged if it matters)
- Chain: none. No `gno_status`, no `node_chain_id`. If any of this is later
  claimed of a deployment, it has to be re-read from that chain.

## Scope

76 non-test `.gno` files, 12 packages:

| Package | Files | Notes |
|---|---|---|
| `r/kourtv2` | 50 (21,187 ln) | the subject |
| `r/kourtv1` | 11 | superseded; audit only for shared patterns |
| `r/govern`, `r/offerer`, `r/ccwrap` | 7 | |
| `p/governor` | 7 | `/p/` lens: importer-conditional severity |
| `p/curve`, `p/cshares`, `p/checkpoint`, `p/twap`, `p/grc20votes`, `p/tickbook` | 6 | |

Tests: 87 files / 32,668 lines (not audited as subject; used as evidence of
intent and to check whether a guard is pinned).

## Phase 1 triage — corrected counts

First sweep returned all-zeros and was **wrong**: zsh does not word-split an
unquoted `$SRC`, so grep received the whole newline-joined file list as one
filename and searched nothing. Re-run via `find -print0 | xargs -0`, with a
`func`-count sanity check (1262) proving the pipeline reaches the source.

| Signal | Hits | Read |
|---|---|---|
| `IsUser(` | 1 | **comment only**, in `kourtv1/buy.gno:25`, explaining why it is NOT used. Authors know the trap. |
| `IsUserCall(` | 3 | the payment guards |
| `OriginSend` | 7 | 2 real reads (`kourtv2/buy.gno:46`, `courtburn.gno:126`), rest comments |
| `OriginCaller` | 9 | all `init()` deployer capture — the sanctioned pattern (init has no `cur`) |
| `PreviousRealm` | 2 | **comments only**, both saying not to use it |
| `.IsCurrent()` | 164 | vs 199 `realm` params — coverage gap to enumerate |
| `cur.Previous()` | 156 | |
| `crossing()` | 0 | no pre-0.9 stale spec |
| `banker.` | 12 | 3 real bankers in kourtv2 |
| `math.MaxInt/MinInt`, `unsafe.Sizeof` | 0 | no platform-divergence signal |
| `CurrentRealm` | 0 | |

## Verified so far (Phase 2)

### `kourtv2/buy.gno` — `Buy(cur realm, slug string)` — CLEAN

- `cur.IsCurrent()` then `prev.IsUserCall()` (**not** `IsUser`) then
  `unsafe.OriginSend()`. Guard-before-read ordering is correct.
- CEI holds: all state effects (`c.minted`, `recordPrice`, `coin.Mint`,
  `reindexBurn`, `accrueFranchise`) precede both `SendCoins` interactions.
- Self-buy blocked (`buyer == cur.Address()`).
- **Solvency invariant checked, not assumed**: the function pays out
  `spent + (sent-spent) = sent` from `cur.Address()`, so it is only solvent if
  `spent <= sent`. Verified in `p/curve/curve.gno:118 Minted`: the short-circuit
  returns `full` only under `coin >= full`; the main path's step-down loop breaks
  only on `cst <= coin` and the step-up loop advances only while `cst <= coin`.
  Holds on every path. Latent fragility (not a finding): the closing
  `spent, _ = c.Cost(from, delta)` discards `ok`, so if the invariant ever broke
  the realm would silently overpay rather than panic.

### `kourtv2/courtburn.gno` — `takeCourtCreationBurn(cur realm)` — CLEAN

- A **non-crossing helper taking a secondary `realm`**, and it checks
  `cur.IsCurrent()` before `cur.Previous()` — the exact Class 2 guard. This is
  the shape most realms get wrong; this one gets it right and documents why.
- Burns the entire payment (no refund path), deliberately, so no GNOT leaves the
  realm to a user here.

**Open question (not yet a finding):** when `courtCreationBurn <= 0` the function
returns before the `IsUserCall` check, deliberately, so another *realm* can
create courts while the burn is unpriced. Need to establish what creating a court
grants before grading this. → worklist.

## Class sweep — result of the full catalog pass

| Class | Shape hunted | Hits |
|---|---|---|
| 1a/1b cur-disclosure | interface method declared with a `realm` param | **0** |
| 2 designation-forgery | exported fn taking `caller address`/`pkgPath string` as identity | **0** |
| 2 designation-forgery | non-crossing helper using a `realm` param for authority, unguarded | **1** (kourtv1, see R1) |
| 3 impl-substitution | exported fn taking `any` / `interface{}` | **0** |
| 4 closed-over-authority | exported fn taking `func(...)`, or function-typed state field | **0** |
| stored realm value | `realm` in struct field / var / map | **0** (all hits are prose) |
| aliased-pointer leak | exported fn returning `*T`, `[]*T`, `map[..]*T` | **0** in `/r/`; `/p/` hits are `New*` constructors returning fresh values |
| payment-bypass | `IsUser(` co-occurring with `OriginSend` | **0** (the one `IsUser(` is a comment saying not to) |
| platform divergence | `math.MinInt/MaxInt`, `unsafe.Sizeof` | **0** |
| float in deterministic path | `float32/64` | **0** (two comments explaining the deliberate avoidance) |
| map-order-as-output | `range` over a map feeding Render/pagination | **0** (every `range` is over a slice) |
| swallowed `.Get()` second return | `, _ := ….Get(` | **0** |
| admin with no rotation | hardcoded admin, no transfer | **N/A** — `TransferGlobalAdmin` exists, guarded |

Method: `find -print0 | xargs -0`, plus a Python pass that parses every function
signature, classifies it crossing-entrypoint vs secondary (exported +
package-level + `realm` first == entrypoint), and checks whether the `realm`
param is used for authority (`.Previous()/.Address()/.PkgPath()`) and whether
`.IsCurrent()` guards it. 175 functions take a `realm`; 143 entrypoints use it
for authority, 0 unguarded; 3 secondaries use it, 1 unguarded.

## Gas — write paths

Every full-tree `avl` walk reachable from a state-changing function is bounded by
an explicit cap enforced at insert. Checked individually, not assumed:

| Site | Walk | Bound |
|---|---|---|
| `association.gno:519` `AddAssociation` | `assocOut.Iterate(prefix, "")` | breaks on prefix mismatch AND on `outN > maxAssocOut` (32) |
| `association.gno:530` `AddAssociation` | `assocIn.Iterate(prefix, "")` | `maxAssocIn` (64) enforced at insert, so the prefix range cannot exceed it |
| `supersede.gno:136` `SupersedeClaim` | `supBy.Iterate(prefix, "")` | prefix break + `maxSupIn` |
| `folders.gno:843` `OrderFolders` | `folders.Iterate("", "")` — **full tree** | `maxFolders` (100) enforced at `folders.gno:309` |
| `modvote.gno:541/558/582` `ResolveElection` | `lines.Iterate("", "")` ×3 | `maxBallotLines` enforced at `modvote.gno:388` |
| `boardlegal.gno:267` `PurgeBoardRange` | `board.Iterate(fromRow, "")` | `maxBoardRowsPerClaim` (512) + `maxBatchRows` batching with resume cursor |

Note the `Iterate(prefix, "")` idiom: an empty end bound walks to the END OF THE
TREE, so these are only bounded because each callback breaks on prefix mismatch
or a counter. That is a real invariant, and it holds at every site above.

The code is already gas-conscious in ways worth recording: `OrderFolders` bounds
`len(ids)` *before* `strings.Split` so a megabyte of commas is refused in the
argument rather than allocated; `strips.gno:204` uses `IterateByOffset` to
descend in O(log n) rather than stepping; `PurgeBoardRange` counts *work* not
*visits*, with a comment describing the bug that distinction fixed.

## Worklist

- [x] Phase 1 triage sweep (re-run after the zsh word-split error)
- [x] `buy.gno` payment path + curve solvency invariant
- [x] `courtburn.gno` payment path
- [x] Full Class 1a/1b/2/3/4 sweep + operational signals
- [x] Entrypoint-vs-secondary `realm` classification (175 functions)
- [x] Gas: bounds on every write-path tree walk
- [ ] `render.gno`/`modrender.gno`/`electionrender.gno`/`relrender.gno` — Render()
      untrusted-content posture; needs `render.md` loaded
- [ ] `p/` packages under the importer-conditional lens (`p/governor` is the big one)
- [ ] What does `StartCourt` grant? (settles the burn-off open question below)
- [ ] Gas: repeated `.Get()` of the same key within one call; string building in loops
- [ ] Two-pass FP filter over R1

## Findings

### 🔴 R1 — `refundGNOT` trusts an unguarded `realm` param — confidence 90%

**Location**: `realm/r/kourtv1/buy.gno:68`

**Class**: Class 2 designation-forgery (`security.md` Audit-signals row
"Helper/secondary `rlm.Address()` without prior `rlm.IsCurrent()` check" — graded
**RED** in the table).

**Evidence**: `refundGNOT(cur realm, to address, amount int64)` is unexported, so
it is *not* a crossing entrypoint and gets no runtime currency guarantee for its
`realm` param. It uses that param for authority twice — `banker.NewBanker(
banker.BankerTypeRealmSend, cur)` and `cur.Address()` as the send source — with
no `cur.IsCurrent()`.

**Why this is NOT exploitable today, stated plainly**: it has exactly one call
site, `Buy` at `buy.gno:62`, and `Buy` checks `cur.IsCurrent()` at line 21. There
is no reachable path that passes a stale realm. It is also in a realm the repo
calls frozen (`scripts/check-render-text.py:161`).

**Why it is still RED**: `audit.md`'s catalog-floor rule — a catalog-graded shape
is emitted at its table grade, and "not exploitable on the current VM" is
explicitly not grounds to lower, because the guarantee is version-bound. The
grade is about the shape, not today's reachability. A second call site is all it
takes.

**Considered objection**: *"the parent guards it, so this is a missed-existing-
guard false positive"* (evidence-gating rule #2). The guard genuinely exists one
layer up — which is why this is reported as unreachable rather than as funds at
risk — but rule #2 governs whether the finding is real, and the catalog floor
governs its grade. The shape is present, so it is not removed.

**Recommendation**: three lines, and kourtv2 already ships them. Its
`takeCourtCreationBurn(cur realm)` is the identical shape and opens with
`if !cur.IsCurrent() { panic(errStaleRealm) }`, with a comment that names the
reason: *"this is a secondary realm parameter by the time it reaches this
function, and a stale one would name the wrong caller."* Copy that into
`refundGNOT`, or fold the two SendCoins lines back into `Buy`.

### 🟡 Y1 — user text can emit HTML block types 6/7 into the rendered page — confidence 85%

**Location**: `realm/r/kourtv2/board.gno:1187` (`boardTextVisible` → `sanitize.Block`),
reached from `Render()` via `render.gno:119 → renderBoardHidden → board.gno:789/792`,
`renderBoardOne → board.gno:1059`. Same shape for claim bodies at
`modrender.gno:220` (`claimBodyQuoted`).

**Class**: `render.md` § Audit signals — "Raw `<script>`, `<style>`, `<iframe>` in
output → RED: goldmark renders raw HTML blocks verbatim; gnoweb has no sanitizer."
Graded down to YELLOW here for the reasons below.

**Evidence, end to end**: a board comment is user-submitted text bounded only by
`maxBoardTextLen = 2000` (`board.gno:56,239`) — no charset filter, `<` is
accepted. It reaches the page through `boardTextVisible` → `sanitize.Block` →
`b.WriteString(... + "\n\n")` at line start → `Render()`. **Verified against the
real library**, not the realm's comment about it: `p/nt/markdown/sanitize/v0`'s
own doc for `Block` states its `\n\n` envelope *"bounds CM §4.6 HTML block types
6 and 7 (`<div>`, `<table>`, `<form>`, arbitrary `<foo>` tags) which close on a
blank line and **are NOT escaped in any mode**."* Types 1–5 (`<script>`, `<pre>`,
`<style>`, `<textarea>`, comments, CDATA) **are** escaped. `<iframe>` is a
CommonMark type-6 tag, so it is not.

**Why this is not a realm coding error**: the realm uses the right sanitizer,
routes every piece of user text through one named gate, and enforces that with a
fail-closed census (`scripts/check-render-text.py`). It documented this exact
residual accurately at `modrender.gno:206-219` — including that it *tested* it
("a body of `<div>` reached the page at line start"). Nothing here is unaware.

**Considered objections, both of which I chased before grading**:

1. *"`Block` escapes it."* — It does not, and I checked the library rather than
   the comment. Its envelope **contains** the block; it does not remove it.
2. *"`Blockquote` fixes it for claim bodies."* — It does not, technically.
   `sanitize.Blockquote` is `Block` plus `> ` per line, so HTML still opens —
   inside a `<blockquote>`. That is *visual attribution*, which is exactly what
   the realm claims for it ("the author's statement, quoted, not the court's own
   words"), and it is a real mitigation for spoofing the court's voice. It is not
   prevention. This is why the finding covers claim bodies too, not just the board.

**Residual, stated concretely**: a 2000-character board comment containing
`<iframe src=…>` or `<table>` renders that element on a page people stake money
on, bounded to its own paragraph region. `<form>` is partly defanged by gnoweb's
`form-action 'self'` CSP (render.md), which stops off-site posting but not the
rendering of a convincing fake form.

**Recommendation**: this is the one place the realm could go beyond the library.
Cheapest option is to reject or escape `<` at the **write** boundary for board
text — a 2000-char field with no legitimate markup-tag use — rather than at the
read boundary, so the bytes never enter state. If instead the risk is accepted,
say so in `MODERATION.md` next to the existing note, because right now the
analysis lives in a code comment on the claim path and the board path inherits
the conclusion without restating it.

**Asymmetry worth noting separately**: board *replies* are manually quoted
(`board.gno:1118` prefixes `> ` per line) and claim bodies are quoted
(`claimBodyQuoted`), but board *top-level* rows are not (`board.gno:789/792/1059`
write `Block` output bare). `boardTextVisible`'s own comment says a comment is
"lower-trust than a claim body, not higher" — so the most prominent board surface
has the weakest attribution of the three. Consistency argues for quoting it too.

### 🟡 Y2 — `p/governor` has a re-entrancy latch that does not cover the vote path — confidence 85%

**Location**: `realm/p/governor/governor.gno:921-978` (`castVote`); same shape in
`propose()` at `:684-768`. The latch itself is at `:263-265, 1284-1285`.

**Class**: Class 3 impl-substitution — "functions that call an interface method
AND mutate state on the same path without a generation counter or in-flight
flag". Here the in-flight flag **exists** and is simply not set on this path.

**Evidence — the exact ordering**:

```
922  if g.executing { panic }        CHECK   (latch read, never written here)
930  if p.voted.Has(who) { panic }   CHECK   (the double-vote guard)
950  w := g.voters.PastVotes(...)    INTERACTION  — caller-supplied interface
962  p.yes += w                      EFFECT
978  p.voted.Set(who, ...)           EFFECT  — the guard's own write
```

Checks → Interactions → Effects. CEI is inverted, and the specific hazard is that
the double-vote guard at 930 reads state that is not written until 978, with an
untrusted external call at 950 in between. `g.executing` is written in exactly
one place — `Execute` at 1284 — so it is false for the whole of `castVote`.

**Exploit shape**: an `Electorate` whose `PastVotes` re-enters `Vote` for the
same voter and proposal. The nested frame passes 922 (latch false) and 930
(`voted` not yet set), recurses, and each unwinding frame adds `w` to `p.yes`.
Vote weight multiplied by recursion depth.

**Not exploitable in kourtv2, verified rather than assumed**: `court.gno:841`
constructs `governor.New(coin, coin)` — the electorate is the realm's own
`grc20votes` ledger — and `grc20votes.go:276 PastVotes` is a pure archive read
(`mustBeSealed`, `getAccount`, `ValueAt`) with no callback and no cross-realm
call. The `*Governor` is also unexported (`court.gno:618`) and no exported
kourtv2 function returns it, the `Electorate`, or the `Token`, so no external
party can substitute an implementation.

**Severity**: YELLOW under the `/p/` lens — an importer must actively misuse it
by supplying a re-entrant electorate. **RED in any realm that lets an external
party choose the `Electorate` implementation.**

**Considered objection**: *"the latch at 922 already prevents this."* It cannot —
it is read on this path and written on no path but `Execute`. A check with no
corresponding write is not a guard, and that is the whole of the finding.

**Recommendation**: set the latch for the duration of `castVote` and `propose`,
symmetrically with `Execute`'s existing `g.executing = true` +
`defer func(){ g.executing = false }()`. Alternatively move `p.voted.Set(...)`
above line 950 so the guard's write precedes the external call. The first is
closer to what the code already does and covers `propose` in the same stroke.

### ❌ Y3 — WITHDRAWN. The cap already existed and I missed it.

**This finding was wrong.** `maxModSetSize = 32` is declared at
`moderation.gno:30` and enforced at `modvote.gno:231` inside `canonicalMembers`,
the single chokepoint **both** growth paths funnel through (`AppointMods` at
`moderation.gno:483`, `RegisterModCandidate` at `modvote.gno:203`). The
constant's own comment says it exists to bound "currentSetID's O(n²)
concatenation" — the realm had already found this exact issue and fixed it.

Real worst case is 32 keys → ~21KB copied, not the 5.2MB the finding claimed.

**How I got it wrong, because the method failure is the lesson.** Two errors
compounded:

1. I grepped for `maxMods`, `maxMembers`, `members.Size()` and `len(canon)`. The
   guard reads `len(in) > maxModSetSize` inside a shared helper, so every one of
   those patterns missed it.
2. I then ran a census of `max*` constants and **piped it through `head -24`**,
   which truncated the list before `moderation.gno`. I read a truncated listing
   as evidence of absence. That is the same class of error as the zsh
   word-splitting bug in the first triage: a command that did not look for what I
   claimed it had ruled out.

The two-pass FP filter did not catch it either, because I challenged *"is it
called often enough to matter?"* and *"is a DAO set inherently small?"* — both
downstream questions — and never re-asked the upstream one, *"is it actually
uncapped?"* Evidence-gating rule #2 is "verify the guard isn't already present in
another layer", and I skipped it for this finding.

Also corrected: the finding named `AddGlobalMod → seatGlobalMember` as the growth
path. That grows `d.members` (the global DAO set); `currentSetID` reads
`cm.members` (the per-court set). Wrong set as well as wrong conclusion.

*What survives*: nothing as a finding. `currentSetID` was still rewritten to use
`strings.Builder`, as a tidy-up rather than a fix — see "Fixes applied".

### ~~🟡 Y3 (original text, kept for the record)~~ — the member set is the one uncapped collection — **SUPERSEDED, see above**

**Location**: `realm/r/kourtv2/moderation.gno:1151-1158` (`currentSetID`), called
from `moderation.gno:1137` (`suspendSet`) and `modvote.gno:676` (the suspension
check). Growth path: `AddGlobalMod` → `seatGlobalMember` (`moderation.gno:115+`).

**Class**: `security.md` § operational — "Admin/privileged inputs with no bound
check → YELLOW, admin footgun; bound at the function boundary." Gas dimension
alongside it.

**Evidence**:

```go
func currentSetID(cm *courtMod) string {
    s := ""
    cm.members.Iterate("", "", func(k string, _ any) bool {
        s += k + ","          // reallocates and copies the whole prefix each time
        return false
    })
    return s
}
```

Three compounding facts, each checked:

1. **The set is uncapped.** `AddGlobalMod` → `seatGlobalMember` performs a
   validity check and an already-a-member early return, and no size check. No
   `maxMembers`/`members.Size()` guard exists anywhere in the realm.
2. **The build is quadratic.** `s += k + ","` on an address key (~41 bytes)
   copies the entire accumulated prefix on every iteration: ~52 KB copied at 50
   members, ~5.2 MB at 500.
3. **The result is persisted.** `cm.suspendedSetID = currentSetID(cm)` at
   `moderation.gno:1137` writes it into realm storage, so storage cost grows with
   the set too, and is re-written on every suspension.

**Why this is the interesting one rather than a nitpick**: every other growable
collection in this realm is explicitly bounded — `maxAssocOut` 32, `maxAssocIn`
64, `maxBoardRowsPerClaim` 512, `maxFolders` 100, `maxFolderItems` 200,
`maxBallotLines` 64, `maxClaimMediaCount` 7, `maxMediaPage` 64, and a dozen more.
The single collection with no bound is the one whose size drives a quadratic
build **and** a stored value. That reads as an oversight rather than a decision.

**Considered objection**: *"only the admin can grow it, so it is self-inflicted."*
Correct, and it is why this is YELLOW and not RED — there is no external DoS
here. It remains an admin footgun of exactly the kind the operational table
names: one privileged input with no bound at the boundary, where the cost lands
on every later moderation action rather than on the call that caused it.

**Recommendation**: two small changes, both already idiomatic here.
(a) Add a `maxMods` constant and check `members.Size()` in `seatGlobalMember`,
matching the twenty other caps.
(b) Build with `strings.Builder` — `board.gno` alone uses it 13 times, so
`currentSetID` is the outlier, not the pattern.

## Gas — allocation patterns

String accumulation with `+=` inside an iteration, separated from the numeric
`+=` hits that share the grep signature:

| Site | In | Bounded by |
|---|---|---|
| `moderation.gno:1154` | `currentSetID` | **nothing** — see Y3 |
| `holders.gno:106` | `TopHolders` | `topHoldersMax` (clamped at entry) |
| `votelock.gno:390` | `CommitmentsOf` | locks per claim |
| `governor.gno:1635/1637/1658` | `render` | proposal count |
| `batch.gno:46`, `rules.gno:40` | render helpers | batch / rules size |
| `folders.gno:929-947` | flag assembly | ≤4 appends, not a loop accumulation |

Only `currentSetID` is both unbounded and on a state-changing path; the rest are
read/render paths with a bound. Recorded so the next pass does not re-derive it.

## Two-pass false-positive filter

Pass 2 challenges each pass-1 finding and tries to refute it. Verification was
done against source, not argument, wherever a check was possible.

### R1 — refundGNOT — **KEPT at RED**, mechanism clarified

*Objection*: "Class 2 is designation-*forgery* — forging who the caller is.
`refundGNOT` never asks who called; it uses `cur` to mint a banker and to name
its own realm address. The described harm (an address that 'no longer refers to
the live caller') does not map."

*Resolution*: the objection sharpens the finding but does not remove it. The
catalog row is literal — "`rlm.Address()` without prior `rlm.IsCurrent()`" — and
that is present. The floor rule permits going below grade only if the pattern is
absent or the trace is wrong; neither holds. **Kept RED, with the mechanism
restated honestly: this is an unguarded *capability* parameter, not caller
misattribution.** The headline stays "not exploitable, frozen realm, 3-line
hardening" so it cannot be misread as funds at risk.

### Y1 — HTML types 6/7 — **DOWNGRADED YELLOW → 🟢 GREEN**

*Objection*: "This is a property of `sanitize` and gnoweb that every realm
accepting user text has. Filing it against kourt is misattribution."

*Verification done anyway*: confirmed at implementation level, not from the
realm's comment — `sanitize.gno:436 Block` calls `markdown.EscapeBlockHazards`
then wraps `\n\n`, and its inline comment states CM §4.6 types 6/7 "are NOT
escaped in any mode". So `<iframe>`/`<form>`/`<table>` genuinely reach the page.

*Resolution*: **the objection holds.** render.md's two rows are
"user-submitted strings echoed **without escaping**" (YELLOW) and "raw
`<script>/<style>/<iframe>` in output" (RED). Kourt does not match the first —
it escapes every user string through one named gate with a fail-closed census —
and the second row describes a realm emitting raw HTML itself, which kourt does
not do. Reading it otherwise would grade every realm on the chain RED, which is
a reductio. Downgraded to GREEN: correct use of the available tool, residual owned
by the library. The two recommendations survive as recommendations, not findings.

### Y2 — governor latch — **KEPT at YELLOW**

*Objection A*: "The latch at 922 already prevents re-entrancy." Refuted: it is
read on this path and written only in `Execute` (`grep` for `executing` returns
exactly one write, line 1284). A check with no corresponding write guards nothing.

*Objection B*: "Would re-entry actually double-count?" Traced: `g.mustProposal(id)`
returns a pointer, so the nested frame mutates the same `*proposal`. The nested
frame passes 930 because 978 has not run, adds `w` to `p.yes`, and the outer
frame adds `w` again on unwind. It double-counts.

*Objection C*: "kourtv2 is safe, so this is moot." Accepted, and it is exactly
why the grade is YELLOW rather than RED — recorded in the finding along with the
verification that `grc20votes.PastVotes` is a pure archive read.

### Y3 — uncapped member set — **KEPT at YELLOW**

*Objection*: "`currentSetID` only runs on suspension, which is rare, so the
quadratic cost never materialises."

*Refuted by checking the call site rather than assuming*: `modvote.gno:676` is
inside **`installModSet()`**, not a suspension path — so it runs on every
mod-set installation. The second caller (`moderation.gno:1137`) is the suspend
path. Two write paths, not one rare one.

*Second objection*: "a DAO member set is inherently small." Plausible as intent,
but nothing in the code says so, and the realm bounds twenty other collections
explicitly. An intended bound that is not written down is not a bound.

## Confidence

First pass: 4 findings (1 RED, 3 YELLOW).
After FP filter: 3 findings (1 RED, 2 YELLOW), Δ 1 downgraded to GREEN.
**After implementation: 2 findings (1 RED, 1 YELLOW), Δ 1 WITHDRAWN as a false
positive** — Y3's guard already existed and two truncated/mis-targeted greps hid
it. Attempting the fix is what surfaced the error; the filter had not.

## Fixes applied

All three landed, `make realm-test` green (21/21, exit 0) and all ten realm gates
green (`check-guards-armed`, `-run`, `-stale-guards`, `-control-anchors`,
`-nontransferable`, `-abort-assertions`, `-storage`, `-read-purity`,
`-render-text`, `-spend-paths`).

**R1 — `realm/r/kourtv1/buy.gno` `refundGNOT`** — added the `cur.IsCurrent()`
guard, with a comment saying why it is not redundant with `Buy`'s: the function
is unexported, so its `realm` is a secondary parameter with no runtime currency
promise, and both uses are capability uses (minting the banker, naming the send
account). kourtv2's `takeCourtCreationBurn` is the same shape and already
guarded.

**Y2 — `realm/p/governor/governor.gno`** — `castVote` and `propose` now HOLD the
latch (`g.executing = true` + `defer` reset) rather than only reading it.
Verified `settle()` cannot re-enter them, and `propose`'s only callers are the
two `Offer` entrypoints, so nothing legitimate nests.

  *The three panic messages gained a clause rather than new wording.* Rewriting
  them outright broke `TestExecutionCannotReEnterTheGovernor`, which pins each by
  substring — correctly, since a panic string is API for anyone matching on it.
  They now read "…from inside an execution **or a vote**", which is true and
  leaves the pinned text in place.

  *New regression test*: `TestAVoteCannotReEnterThroughTheElectorate` installs a
  `hostileCouncil` whose `PastVotes` re-enters `Vote`, and asserts on the TALLY
  rather than on the panic — a test that only checked for a refusal would still
  pass if the refusal arrived after the weight was added. **Mutation-tested**:
  removing the `castVote` latch fails it, and it is the ONLY test that fails,
  which is also the proof the gap was untested before.

**Y3 — `realm/r/kourtv2/moderation.gno` `currentSetID`** — rewritten with
`strings.Builder`. Filed as a tidy-up, not a fix, and the comment says so: at 32
members the old form copied ~21KB, which is nothing. The reason to change it
anyway is that `maxModSetSize` is currently load-bearing for a cost it no longer
has to be — with a linear build, that cap can be chosen for what a working m-of-n
committee should be rather than for what fits in a transaction.

## Not fixed, deliberately

- **The Y1 GREEN recommendations** (reject `<` at the board's write boundary;
  quote top-level board rows for consistency with replies and claim bodies).
  Both are visible product changes — the first stops anyone typing `a < b` in a
  2000-character comment — and neither fixes a defect. They are the owner's call,
  not an auditor's.
- **`p/curve.Minted`'s discarded `ok`** (`spent, _ = c.Cost(from, delta)`). The
  solvency invariant holds on every path, so changing it would be churn on
  correct code; recorded under Open questions instead.
- **`p/tickbook`'s tick-by-tick scan** of empty ticks where the occupancy bitmap
  could be bit-scanned. A micro-optimisation on a bounded loop.

## 🟢 GREEN — noted, not blocking

### Court creation when the burn is unpriced — **open question CLOSED, no race**

The worry was that with `courtCreationBurn <= 0`, `takeCourtCreationBurn` returns
before its `IsUserCall` check, so a *realm* can create courts — and
`court.gno:862` contains `if directoryAdmin == "" { directoryAdmin = admin }`,
which bootstraps the realm-wide admin from the first court's creator. If that
were reachable publicly, first-to-create would capture the admin seat.

It is not. `meta.gno:113` `init()` is unconditional and calls
`startCourt(unsafe.OriginCaller(), …)`, so `directoryAdmin` is set to the
**deployer inside the deploy transaction**, before any external call is possible.
`startCourtUser` then refuses `metaSlug`, so the public path cannot retake it.
The residual permissiveness only lets a realm admin *its own* court, which is
what a user creating one already gets. Spam is priced separately — `startCourt`'s
comment: "court-count floods are storage-deposit-priced".

Init ordering is load-bearing here. Anything that later makes that `init()`
conditional, or moves the meta-court creation out of it, re-opens the capture.

### Class 1a/1b cur-disclosure — comprehensively clean

Every cross-realm call in the repo wraps the capability: `cross(cur)` at
`ccwrap.gno:262/302/327` and `offerer.gno:98`. **Zero** sites pass a bare `cur`
as an argument to a foreign realm's function, and zero forward it to an interface
or callback. Combined with zero interfaces declaring a `realm` parameter, the
whole 1a/1b surface is absent rather than merely guarded.

### `r/ccwrap` value conservation — correct, and for the stated reason

`Wrap` pulls then mints (`ccwrap.gno:302-305`); `Unwrap` burns then releases
(`:324-327`). Both orderings are the safe one and both are documented with the
failure they prevent — "a mint before it would hand out an unbacked claim on a
failed deposit", and the mirror for burn-before-release. The realm holds no
privilege inside kourtv2: it moves coin through an allowance
(`TransferFromCC`), which is what the comment means by "the allowance is what
buys this package its complete lack of privilege". `mustWrapRoom` caps the
escrow before the pull.

### The remaining `/p/` packages — loops bounded, overflow checked

Closing the over-claim in the previous pass: the class sweep covered all 12
packages, but targeted invariant reads had only covered 3 of 6 `/p/`. The other
three are now done.

- **`p/tickbook`** — the crossing loop (`:253-300`) is bounded three ways: an
  explicit `crossed >= maxLevels` break, a `demand == 0` break, and `tk :=
  uint8(t)` capping the tick space at 256 with occupancy held in two `uint64`s.
  Overflow is handled with `overflow.Mul64`/`Add64` and `ok` checks that panic.
  *Observation, not a finding*: empty ticks are stepped one at a time
  (`t += step; continue`) when the occupancy bitmap could be bit-scanned to jump
  straight to the next set bit. Worth ~100 cheap iterations on a sparse book;
  a micro-optimisation, not a defect.
- **`p/checkpoint`** — `ValueAt` (`:211`) is **not** the linear history scan its
  backward `for i := len(s)/12 - 1` loop suggests. Two O(1) fast paths (`at >=
  s.e0 → s.cur`, `at >= s.e1 → s.prev`) answer recent epochs outright, and the
  governor asks at `Epoch()-1`, so the vote path hits them. Older queries do an
  O(log n) bptree `ReverseIterate` bounded below by the key's own prefix, and the
  linear part is within a single page. This matters because `ValueAt` is what
  `grc20votes.PastVotes` calls on the governor's vote path — it was the most
  promising remaining gas candidate and it does not fire.
- **`p/twap`** — `steps` is explicitly clamped to the ring size (`:161`, "More
  than n steps just laps the ring, so cap the work at n"), and `for idx < 0 {
  idx += r.n }` (`:223`) runs at most once given the `bkt <= r.base-r.filled`
  break above it. The bucket sum carries an explicit wrapped-sum check.

## Coverage

All 12 packages have now been through the class sweep and the entrypoint/secondary
classification. `r/offerer`, `r/govern`, `r/kourtv1` and the six `/p/` packages
were inside every global sweep (76 files); `p/governor`, `p/curve`,
`p/grc20votes` and `r/ccwrap` additionally got targeted reads for their specific
invariants (re-entrancy latch, curve solvency, pure-read electorate, 1:1 backing).

## Open questions
- **`p/curve.Minted` discards `ok`.** The closing `spent, _ = c.Cost(from, delta)`
  drops the overflow flag. The solvency invariant holds on every path today, so
  this is fragility rather than a finding: if it ever broke, the realm would
  overpay silently instead of panicking.

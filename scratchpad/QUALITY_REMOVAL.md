# Removing the quality vote — what it would actually take

Owner's ask: *"it's bad UX to require two stages of voting for every claim… how
can we reduce this to one vote?"* and then *"can quality be removed?"*

Reviewed in three passes against the code. Each pass found something the
previous one had asserted wrongly or missed; §6 records what moved, because the
corrections are the useful part.

## 0. Two things I said in chat that were wrong

**"It's just a 0/1/2 multiplier on rewards."** Wrong by omission. `tierLowX = 0`
(court.gno:61) and `want := mustMul(cs.tier, midGross)` (crystallize.gno:86) — a
LOW verdict does not shrink a claim's draw, it **zeroes** it. Quality is the
author-mill defence, named as such at crystallize.gno:177, court.gno:91 and
standing.gno:672.

**"They'd have to share one epoch."** Also wrong. Merging the *transaction* does
not require merging the *weights*. `VoteDispute` weighs at the round's epoch,
`VoteQuality` at `cs.qualityEpoch` pinned at the answer, and one entrypoint can
apply each with its own — the realm already computes them independently
(`ClaimVoteWeightOf` returns the pair). Nothing economic has to change to put
both questions on one ballot.

## 1. What "quality" actually is

Five separable things under one name. The owner's complaint is only about (D).

| | Thing | Where | Removing it costs |
|---|---|---|---|
| A | **The tier** — 0/1/2 multiplier on the claim draw, the author bonus (HIGH only), the answerer cap (`tier×bond/2`), `capBonus` | crystallize.gno, standing.gno | the mill defence; the only "this claim was worthless" signal |
| B | **The flag lane** — one slot per claim, bond `max(flagMin, 2%·X̄)` doubling per inconclusive cycle then frozen at 4×, 7-day cooldown, bounty on a conclusive low | quality.gno | the only way to raise quality on an **undisputed** claim |
| C | **The slash** — `max(4.5%·X̄, 1.6× forgone mid draw)` reserved from the answer bond, plus the counter-flag window the answerer may force one re-vote in | quality.gno, crystallize.gno | the only penalty for answering a worthless claim |
| D | **The ballot** — a sealed 3-bucket tally riding an open dispute or flag | quality.gno:195 | *nothing economic* — this is delivery, not policy |
| E | **`slotConsumed`** — the standing ledger's "the court actually decided something here" flag | quality.gno (2 writes), standing.gno:700 | see §3, this is the expensive one |

**Quality never stands alone.** `qualityQuestionOpen = flagOpen \|\| disputeOpen
\|\| counterOpen` (quality.gno:144). An ordinary claim is never put to a quality
vote. The two ballots only collide on a claim somebody disputed or flagged.

## 2. Size

| Surface | Count |
|---|---|
| `quality.gno` / `quality_test.gno` | 1,154 / 2,955 lines |
| Other realm tests referencing it | 15 files, ~145 refs (audit_m3 47, crystallize 30, votelock 17, standing 15, escrowsum 12, structural 11) |
| Exported realm functions | 11 |
| `claimState` fields | ~20 |
| Overlay | `qualitySection`, 8 buttons, 4 reads |

## 3. The two things that make full removal expensive

Neither is in the quality files, which is why neither is obvious.

### 3a. Every conviction credit halves, permanently

`creditWinConviction` (standing.gno:694) applies `standingUnadjudicatedBps`
(= 5,000 bps = **half**) whenever `!cs.slotConsumed`. And `slotConsumed` is
written in exactly two places, **both inside quality.gno** (461 ResolveFlag, 768
resolveQualityRide) — both "a quality tally that reached a conclusion".

Remove quality and `slotConsumed` is never true again, so **every winning
staker in every court is credited at the unadjudicated half-rate forever**, and
the full rate becomes unreachable. That is a system-wide change to the standing
ledger, not a niche effect, and nothing in the quality files would show it.

Of the four standing categories: **flag** → never credited, **author** → never
credited (`creditAuthorHigh` gates on `tier == tierHighX`), **conviction** →
permanently halved, **dispute** → unaffected.

### 3b. The `flag` standing category cannot simply be deleted

standing.gno:346 says it plainly: *"this list is part of that wire and adding to
the middle would renumber every stored row. Append only."* `standingCatFlag` is
index 0 of a persisted enum. Removing it renumbers every stored standing row in
every court.

So even in a full removal the category has to **stay as a dormant slot**. The
rate five-tuple is also serialized (standing.gno:288, 327) and sanity-checked
against itself (`if r.flag < r.authorHigh || r.dispute < r.authorHigh`,
standing.gno:223) — both need answers that are not "delete the field".

## 4. Four options, cheapest first

### Option 1 — Merge the ballots. Keep all the economics. **(recommended)**

`VoteDispute(courtSlug, claimID, choice, tier)` — one signature, two lanes, each
weighed at its own epoch. `VoteQuality` stays as the flag lane's entrypoint.

- Answers exactly what the owner complained about.
- **Zero economic change.** No epoch merge, no policy change, nothing to re-vet.
- Both lanes already share the participant exclusion (`isParticipant`), so the
  merged call has one guard rather than two.

Two things it must get right, and the first is not cosmetic:

- **"No opinion" needs a real value, and a naive merge leans the way the mill
  wants.** Buckets are 0/1/2 with no abstain (quality.gno:202), so merging
  without a fourth value posts MID for every verdict voter. Read
  `applyQualityTally` (quality.gno:391) and that is not neutral in either term:

      turnout = qLowW + qMidW + qHighW        // added mid RAISES turnout
      conclusive requires turnout >= fullBar  // ...so it manufactures conclusions
      medianLow := qLowW*2 >= turnout         // ...and DILUTES a low that would carry

  So the extra mid weight both pushes inconclusive tallies over the full bar
  into **conclusive MID**, and suppresses lows that would otherwise have taken
  the median. Conclusive-mid is precisely the mill's preferred outcome: the
  claim draws in full, and `slotConsumed` gets set, which also unlocks the full
  conviction standing rate (§3a). A merge that defaults to mid is therefore not
  merely imprecise — it is aligned with the attack the tier exists to refuse.
  The fourth value is the whole safety of this option.
- **Atomicity.** If the quality half panics (no quality-epoch weight, common for
  anyone who bought in after the answer) the whole transaction reverts and the
  verdict vote is lost with it. Weigh-and-skip, or say plainly that the ballot is
  all-or-nothing.

### Option 2 — Drop the flag lane (B) and the slash (C). Keep the tier (A), riding disputes only.

- Removes 8 of 11 exported functions, the bond/bounty/cooldown/cycle machinery
  and the counter-flag window — roughly half of `quality.gno`.
- **But `slotConsumed` survives** via `resolveQualityRide`, so §3a does not bite.
  This is the cut that keeps the standing ledger intact.
- Cost: quality is decided only when someone disputes. An undisputed worthless
  claim keeps the default mid and draws mid, so the mill defence becomes
  "somebody must post a dispute bond to zero it" — strictly weaker and strictly
  more expensive than a flag.

### Option 3 — Remove quality entirely. Fix `tier = tierMidX`.

- Deletes ~4,100 lines of realm + test, 11 exported functions, ~20 state fields,
  the overlay section.
- Pays for it with §3a **and** §3b, plus: every claim draws the mid amount
  including worthless ones, and nothing penalises answering them. standing.gno:676
  describes that exact attack — *"open a claim, self-answer it, self-stake the
  answered side, settle undisputed, collect on a default nobody voted for"* — as
  the thing the tier arm exists to refuse.

### Option 4 — Replace the vote with a measurement.

Derive the tier from something already on chain: distinct stakers, stake depth,
whether it was disputed at all, conviction spread across sides.

- Removes the ballot **and** keeps a mill defence.
- Cost: every measurement is gameable in its own way, and it changes who gets
  paid — an economics design task, not a refactor, and not something to smuggle
  in as a UI change.

## 5. Recommendation

**Do Option 1 now.** It is the only one that answers the actual complaint — two
ballots — at zero economic cost, and it is reversible.

**Then treat 2/3/4 as a separate economics decision.** "Is the flag lane worth
its complexity?" is a fair question and the answer may be no; Option 2 is the
honest version of it, because it is the only cut that leaves the standing ledger
alone. It is not the same question as "why am I signing twice", and answering
that one by deleting the mill defence would trade a UI annoyance for an
economic hole.

## 6. If Option 3 is chosen anyway — order of work

1. **Answer the mill question first, in writing.** What stops a mill once the
   tier is fixed at mid? If the answer is "nothing, accepted", record it as an
   explicit decision, not a silent consequence.
2. **Decide what `slotConsumed` means with no quality lane** (§3a). Either retire
   the unadjudicated rate — which is a deliberate change to what every staker
   earns — or find a new writer for the flag. Landing this by accident halves
   every conviction credit in every court.
3. Freeze the constant: `tier` → `tierMidX` at every write site (session.gno:51,
   dispute.gno:521, dispute.gno:593, quality.gno:464, quality.gno:765). Land it
   alone and run the full suite; arithmetic simplification is where a silent
   factor of two hides. **Do not grep `.tier`** — `directory.gno:48` is a
   *court* tier, a different field on a different struct.
4. Delete the slash and counter window (C). `escrowsum_test.gno` asserts the
   escrow holds exactly the sum of a **closed** obligation set, of which the flag
   bond and pending slash are members — that identity must be re-derived, not
   deleted.
5. Delete the flag lane (B) and its 8 entrypoints — but keep `standingCatFlag` as
   a dormant slot (§3b).
6. Delete the ballot (D): `qVoted`, `qLowW/qMidW/qHighW`, `qVoteSeq`.
7. Overlay: `qualitySection`, its 4 reads, `ClaimVoteWeightOf` loses its second
   return, and `qualityLockLine` goes with it.
8. Guards: `check-epoch-coherence` counts sealed-epoch functions, vote-weight
   expressions and self-only vote-lock sites; its arms must be **re-derived**,
   not relaxed.

## 7. Not verified

- **Migration.** A realm change is a redeploy. A claim mid-flag at that moment
  has a bond escrowed and a slash possibly reserved; nothing here says what
  happens to it.
- `audit_m3_test.gno` (47 refs) reads like an adversarial suite, so some cases
  are probably *about* the slash rather than merely using it. Those are findings
  someone paid for; they should be re-read before deletion, not counted as churn.

## 8. What each review pass changed

- **Pass 1 → 2:** found §3a (the conviction halving) and §3b (the persisted
  category index). Both live in standing.gno, neither is visible from the quality
  files, and Option 3 had been costed without either.
- **Pass 3:** found the hazard in my own recommendation. `applyQualityTally`
  gates on turnout and on a weighted median, so a merged ballot defaulting to
  mid manufactures conclusive-mid verdicts and dilutes lows — the mill's
  preferred outcome, reached through a change I had described as having "zero
  economic effect". It has zero economic effect *only* with a real "no opinion"
  value. Pass 1 and pass 2 both listed that as a detail to tidy up.
- **Pass 2 → 3:** found that Option 2 is *not* merely a smaller Option 3 —
  `resolveQualityRide` keeps `slotConsumed` alive, so Option 2 avoids §3a
  entirely. That is what makes it the honest middle, and pass 1 had them on a
  single continuum.
- Also corrected: the mill attack is described in standing.gno, not only in the
  crystallize comments, and it names the standing ledger as a target rather than
  just the draw.

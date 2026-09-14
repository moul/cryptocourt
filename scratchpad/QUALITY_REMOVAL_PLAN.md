# Removing quality — implementation plan

Decision taken: quality goes. The tier, the flag lane, the slash and the ballot
all come out. This plan is *how*, and it exists mainly to name the six things
that look like quality and are not — two of which are money bugs waiting for a
confident delete — and the one thing that does not look like quality and is.

Reviewed in passes; §8 records what each pass changed.

## 1. The six traps

Deleting `quality.gno` and grepping for `tier` breaks the realm in six places
that have nothing to do with quality. Two of them are money bugs.

### T1 — `qVoted` is the DISPUTE carrot's record, not the quality tally's

Despite the name, it holds three key namespaces:

| key | written by | read by |
|---|---|---|
| `"d" + pid + addr` → choice | dispute.gno:224 | `PullCarrot` (crystallize.gno:436) |
| `"dw" + pid + addr` → weight | dispute.gno:229 | `PullCarrot` (crystallize.gno:440) |
| `beClaimKey(qVoteSeq) + addr` | quality.gno:277 | `PullCarrot`'s quality branch (447) |

**The tree stays.** Only the third namespace and `PullCarrot`'s
`cs.conclusiveSeq` branch go. Rename the field `voted` in the same commit — a
`qVoted` with no q is how the next reader gets this wrong again.

### T2 — `slotConsumed` is the standing ledger's rate switch

Written only in quality.gno (461, 768); read in standing.gno:700 where
`creditWinConviction` applies `standingUnadjudicatedBps` (5,000 = **half**)
whenever it is false. Delete quality naively and every winning staker in every
court is credited at half rate forever. **This is the one decision the plan has
to make rather than mechanically carry out** — see §2.

### T3 — `standingCatFlag` is index 0 of a persisted enum

standing.gno:346: *"this list is part of that wire and adding to the middle
would renumber every stored row. Append only."* The category stays as a dormant
slot. The rate tuple is also serialized (288, 327) and sanity-checked against
itself (223, `r.flag < r.authorHigh`), so `r.flag` stays in the struct too.

### T4 — `directory.gno:48` `c.tier` is a COURT tier

Different field, different struct, unrelated. Do not grep `.tier` and delete.

### T5 — the carrot's already-claimed latch is keyed on `conclusiveSeq` — MONEY

`claimedKey := "c" + beClaimKey(cs.conclusiveSeq) + string(who)`
(crystallize.gno:456). On a **dispute**-decided claim `conclusiveSeq` is 0, so
the key is `"c" + <eight zero bytes> + addr` — it works today as a per-address
latch by accident of the constant, not by design.

`conclusiveSeq` is on the delete list (§4 step 6). Re-key this latch in the same
commit, deliberately, and assert the double-pull refusal — getting it wrong pays
the carrot **twice to the same address**. This is the most dangerous single line
in the removal.

### T6 — the carrot's per-voter clamp is defined in FLAG-BOND constants — MONEY

crystallize.gno:465: `b0 := cs.xBarFrozen * flagBondXBps / 10000`, floored at
`flagMinCC`, then `clamp := b0/2 - 1`. The carrot is capped at *half a flag bond
minus one*. Delete `flagBondXBps` and `flagMinCC` with the flag lane (§4 step 5)
and the clamp loses its definition.

Decide what the clamp means with no flag: either promote the two constants out
of the flag lane under their own name — they are now simply "the carrot clamp
base" — or re-express the clamp against something surviving. Do **not** inline
the numbers; a magic constant here is a payout ceiling nobody can find.

## 2. The one real decision: what replaces `slotConsumed`

`slotConsumed` means *"the court actually decided something here"*, and it
selects between the full conviction rate and half.

**Recommended: re-key it to `cs.decidedPID != 0`.** That is set in dispute.gno
(344, 395) when a dispute round decides a claim, and it is the surviving thing
that means the same sentence. The two-rate structure is preserved, the meaning
is preserved, and the numbers move only for claims that had a *quality*
adjudication but no dispute — which is exactly the population the flag lane
existed to reach, and which is being removed on purpose.

**Conservative alternative:** fold the halving into the base rate
(`standingConvictionBps × 50%`) and delete the branch. Nothing changes for the
common path (most claims are unadjudicated today) and adjudicated ones quietly
join them. Take this if the re-key turns out to move more than expected —
measure before choosing.

**Do not** simply drop the branch and leave the full rate: that doubles every
conviction credit in every court, which is a monetary policy change wearing a
refactor's clothes.

## 3. What each tier read collapses to

`tier` is deleted, not fixed at a constant — a constant multiplier that is
always 1 is a field nobody can see is dead.

| site | now | becomes |
|---|---|---|
| crystallize.gno:86 | `want := mustMul(cs.tier, midGross)` | `want := midGross` |
| crystallize.gno:354 | `mustMul(cs.tier, cs.answerBond0) / 2` | `cs.answerBond0 / 2` |
| crystallize.gno:387 | `mustMul(cs.tier, rawAvg) / 2` | `rawAvg / 2` |
| crystallize.gno:181 | `creditAuthorHigh(c, cs, cs.tier)` | deleted — it gates on `tier == tierHighX`, so at no tier it never fires |
| standing.gno:695 | `if cs.tier < tierMidX { return }` | deleted — always false |
| render.gno:434-455 | the quality display block | deleted |

Note the shape: three of these are `× 1`, so the arithmetic is unchanged. The
draw does **not** move for a mid claim. It moves only for claims that would have
been rated low (draw was 0, becomes full) or high (draw was 2×, becomes 1×).
That is the accepted economic consequence, stated as a number rather than left
implicit.

`creditAuthorHigh` disappearing means the **author standing category is never
credited again**. `AuthorBonus` — the money lane — is *not* tier-gated
(crystallize.gno:300-315) and is unaffected.

## 4. Order of work

Each step lands alone, green, and is committed before the next.

1. **`slotConsumed` re-key (§2), while quality still exists.** Change
   standing.gno:700 to the new signal and land it. Doing it first means the
   standing suite is exercising the new rule against the *old* code, so any
   movement is visible in isolation rather than buried in a 4,000-line deletion.
2. **Freeze the tier.** Replace the six reads per §3, delete `creditAuthorHigh`,
   keep the field written but unread. Run the full suite: this is where a silent
   factor of two would hide, and it is still reversible here.
3. **Drop the field.** `tier`, `tierFinal`, `tierLowX/MidX/HighX`, and the
   tier-clobber guards at session.gno:50, dispute.gno:520, dispute.gno:592.
4. **The slash and counter window.** `pendingSlash`, `pendingSlashUntil`,
   `counterUsed/Open/VoteEnd`, `slashLevied`, `slashFlagger`, `ResolveCounter`,
   `ResolveSlashWindow`, `CounterFlag`. `escrowsum_test.gno` asserts the escrow
   holds exactly the sum of a **closed** obligation set. `escrowObligations`
   sums six per-claim terms — deposit, fee, answerBond, disputeBond, flagBond,
   pendingSlash — and this step removes the last two, so the identity is
   **re-derived to four**, not deleted. (The header's "five" counts transfer
   *kinds*, including the election lane's nomination bond; that is a different
   number from the term count and I read one as the other on an earlier pass.)
5. **The flag lane.** `OpenFlag`, `ResolveFlag`, `FlagState`, `FlagBondNext`,
   `FlagCycles`, `flagBondFor`, `flagCooldownBlocks`, and the flag fields.
   `standingCatFlag` and `r.flag` stay (T3).
6. **The ballot.** `VoteQuality`, `QualityTier`, `QualityBars`,
   `applyQualityTally`, `resolveQualityRide`, `reaskQualityTally`,
   `qualityQuestionOpen`, `qLowW/qMidW/qHighW`, `qVoteSeq`, `conclusiveSeq`,
   `qualityEpoch`, `qualityReasked`. `qVoted` survives and is renamed (T1).
7. **`ClaimVoteWeightOf` and `VoteWeightWhy` lose their second return.** Both are
   read by the overlay; the pair shape exists only because there were two lanes.
8. **Overlay.** `qualitySection`, its 4 reads, `qualityLockLine` and the
   `qualcommit` span, the `d.tier`/`d.tierFinal`/`d.flagState`/`d.flagBondNext`
   fields in `claimDetail`, and the demo dataset's quality columns.
9. **Guards, re-derived not relaxed.** `check-epoch-coherence` arms 5 (the
   quality vote-lock's terminal predicate), 10 (`qualityQuestionOpen` has one
   definition — the arm's subject ceases to exist, so the arm is deleted with a
   note, not weakened), `LOCKVOTE_CALLS_N` 3 → 2, and the escrow obligation set
   5 → 3 per step 4.
10. **Docs.** PLAN.md §3.4 / §10.2 Q1-Q5, MODERATION.md, VOTEFLOOR.md all
    describe the lane. An ADR records the decision and the accepted consequence.

## 5. Tests

`quality_test.gno` (2,955 lines) goes wholesale. The other 15 files are the
work:

- **`audit_m3_test.gno` (47 refs)** — adversarial cases someone paid for. Read
  each one and ask whether it is *about* the slash or merely *uses* it. The
  second kind gets rewritten; the first kind is a finding that dies with its
  mechanism, and that should be noted in the ADR rather than silently dropped.
- **`escrowsum_test.gno` (12)** — a conservation identity. Re-derive (step 4).
- **`crystallize_test.gno` (30)** — mostly tier arithmetic; the `× 1` collapse
  means most expectations keep their numbers, which makes it a good check that
  step 2 was right.
- **`standing_test.gno` (15)** — will move with §2, and is the test that says
  whether the re-key was the right call.
- **`votelock_test.gno` (17)** — the quality lane's release rule goes; the
  verdict lane's stays.

## 6. What this costs, stated once

Worthless claims draw the same as good ones. Nothing penalises answering one.
The author standing category is never credited. Those are the accepted terms of
the decision, not surprises to discover in review.

## 7. Checked and settled

- **`qualityEpoch` has no non-quality reader.** Written at answer.gno:213, read
  only in quality.gno and voteweight.gno's quality lane. It goes.
- **No emission is stranded by narrowing the carrot.** `carrotTotal` is a budget
  figure; `mintEmission` runs at pull time. And an undisputed, unflagged claim
  already has no payable carrot today — `PullCarrot`'s `default:` panics with
  "no vote resolved this claim". Removal narrows the payable set from
  {dispute-decided, quality-decided} to {dispute-decided}; it orphans no coin.

## 7b. Still not settled

- **Migration.** A realm change is a redeploy. A claim mid-flag has a bond
  escrowed and possibly a slash reserved; a claim mid-dispute has a quality ride
  accumulating. Nothing here says what happens to those, and on a live chain
  that is the first question, not the last.

## 8. What each pass changed

- **Pass 1 → 2:** found T1. The plan had `qVoted` in the delete list, which
  would have broken `PullCarrot` for every dispute-decided claim — the carrot is
  paid off the `"d"`/`"dw"` namespaces in the same tree.
- **Pass 2 → 3:** found that the money-lane `AuthorBonus` is *not* tier-gated,
  only the standing credit is. The earlier draft implied both died together.
- **Pass 4:** found T5 and T6, both money. The carrot's already-claimed latch is
  keyed on a field being deleted (double-pull risk), and its per-voter clamp is
  defined in flag-bond constants that go with the flag lane. Also corrected the
  escrow identity from "five members, re-derive to three" to "six terms,
  re-derive to four".
- **Pass 3:** changed step order — the `slotConsumed` re-key moves to step 1,
  *before* the deletion, so its effect on the standing ledger is measurable in
  isolation instead of arriving inside a 4,000-line diff.

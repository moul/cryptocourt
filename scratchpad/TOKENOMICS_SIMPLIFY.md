# Value from attention — what the code already does, and the one gap

Owner's model: *"if a claim is interesting, many people participate; if not, few
will. Total volume (yes+no) is a proxy for low/mid/high."* And: *"they can't game
the crowd as long as the token is well distributed."*

Both are right, and the second is already how the system is built. The finding
that matters is not that this needs adding — it is that **it is already the
payout's base**, and that one gap in it is currently covered by the slash we are
about to delete.

## 1. It is already the base

Three facts from the code, none of which I knew when this conversation started:

**X̄ is the claim's own time-averaged TOTAL stake** — yes plus no, open interest
— sampled on a trailing window (answer.gno:2, court.gno:340). It is the "total
volume" of the proposal, already computed, already snapshotted at the answer as
`xBarFrozen`, and already the base every bond and slash in the system is priced
off (answer.gno:6).

**The draw is proportional to it.** answer.gno:103 states the identity outright:

    midGross / X̄ = rateBpsFP · T_c / 1e10

So a claim's payout already scales with total attention × how long it was held.
More money on the question, for longer, means a bigger draw. That is the owner's
model, and it is the formula.

**There is already a supply-fraction attention gate.** A claim cannot be answered
until its trailing X̄ clears `effMinAnswerX = max(dust, bps × supply)`
(court.gno:781, `supplyFloor`). Raw supply, not votable, *"votable is deflatable
for free"*. An unanswered claim never crystallizes and never draws. So **"nobody
bothered ⇒ no payout" is enforced today**, at the answer, by a bar keyed to
supply — exactly the distribution argument.

The tier was never the attention signal. It was an orthogonal human judgement
*on top of* an attention-derived number: a way to say "lots of money looked at
this and it was still worthless". Deleting it leaves the owner's model running
by itself.

**Therefore: do not build a volume-derived tier.** It would multiply an
attention-derived quantity by an attention-derived step — the same variable
counted twice, and a draw that goes superlinear in the one number an attacker
controls. The simplification is the deletion already in flight, not a
replacement.

## 2. The gap, and why it is urgent now

The gate and the payout do **not** measure the same thing.

| | measured over | can be drained |
|---|---|---|
| answerability gate | trailing 3-hour X̄ | **yes** |
| the draw | banked LIFETIME conviction | **no** — `Unstake` keeps conviction by design (F9, no-loss) |

answer.gno:120-128 records the attack in full: *"a mill could bank conviction
with a large stake, drain to minAnswerX two hours before answering, and post a
bond 224× smaller than the slash it had earned: caught in full, it paid ~51 CC
against a 11,220 CC exposure."*

The fix that shipped was a **collateralization floor on the answer bond**, sized
so the bond covers the slash it may later owe. That is: the drain path is
currently deterred **by the slash** — the mechanism this project is deleting.

So removing quality does not merely remove a judgement. It removes the thing
standing in front of a known, measured, documented capital attack, and leaves
the collateralization floor guarding a slash that no longer exists.

**This is the finding. Everything below follows from it.**

## 3. The simplification that actually works: one number, both jobs

Make the quantity that *gates* the same quantity that *pays*. Then draining is
self-defeating, because the number an attacker drains is the number that pays
them — no separate deterrent required, and nothing to vote on.

Concretely: the answerability floor should be read on the same integral the draw
is computed from, not on a 3-hour trailing average of it.

- A mill that banks conviction and drains has a large integral and a small
  trailing average. Today: passes the gate, draws big. Under this: passes the
  gate (integral is large) and draws big — **no change**. ✗
- Reverse it — gate and pay on the trailing average at freeze — and the mill
  that drains fails the gate *and* earns nothing. ✓

So the direction is: **pay on X̄ at freeze, not on the lifetime integral.**
`xBarFrozen` is already snapshotted at exactly the right moment and is already
the base for every other price in the system. The draw would become a function
of the same number, and F9's "unstaking keeps conviction" stops being a payout
subsidy — it keeps being true for *refunds*, which is what no-loss is actually
about.

This is a real change to the emission formula, and it wants its own vetting
against the calibration in COURTS_TOKENOMICS.md — the identity in §1 is what the
rate constants were fitted to. I am flagging it, not claiming it is safe by
inspection.

## 4. The options, and what each costs

### A — Delete the tier, change nothing else. (What is currently in flight.)

Payout stays ∝ lifetime conviction; the answerability floor stays on trailing X̄.
Simple, small, and it ships the owner's model for the honest case.

**Cost: the drain path loses its only deterrent.** The bond-collateralization
floor stays in the code guarding nothing. A mill banks conviction, drains before
answering, answers cheaply, draws on banked conviction, and nothing refuses it.

If this is chosen, the collateralization floor and its deploy check should be
deleted in the same commit rather than left as a guard over a hole, and the
attack should be recorded as accepted with its measured numbers (51 CC paid
against 11,220 CC).

### B — Delete the tier AND move the draw onto `xBarFrozen`. (Recommended.)

One number gates and pays. The drain attack dies without a slash, a flag or a
ballot, which is the whole point: the mechanism is not replaced, it is made
unnecessary.

**Cost:** it re-prices every claim, so the rate constants need re-fitting, and
"held a long time" stops being rewarded independently of "held a lot". That
second part is a genuine change in what the token pays for — conviction was
deliberately stake × *time*.

### C — Delete the tier, raise `ordAnswerXFloorBps`.

A single parameter, no code. Makes "nobody bothered" bite harder at the gate.

**Cost: it does not touch the drain path at all** — the mill drains *to* the
floor, so raising the floor raises the mill's cost linearly while leaving the
attack shape intact. It is a mitigation, not a fix, and should not be mistaken
for one.

## 5. Recommendation

**B**, and treat the re-fit as the actual work. A and C both leave a measured
capital attack with its deterrent deleted, and A leaves a guard standing over
nothing, which is worse than either — a future reader will trust it.

If B's re-fit is too large a piece of work to take now, then **A with the
consequences written down and the dead collateralization floor removed** is
honest. What is not honest is A with the floor left in place, because the code
will read as though the drain path is still covered.

## 6. What this design cannot do, in any option

Attention becomes value by construction. There is no way left to say "a lot of
money looked at this and it was still worthless" — that judgement was the tier's
entire job. A well-funded holder can manufacture attention by staking both sides
(principal returns 1× on both), bounded by their holdings, by time, and by the
opportunity cost of the lock. That bound is the distribution argument, and it is
the same bound the quality vote had, which is why trading one for the other is
reasonable.

## 7. Not verified

- Whether the rate constants in COURTS_TOKENOMICS.md can be re-fitted onto
  `xBarFrozen` without breaking the per-claim cap (G_MAX) and reservoir clamps.
  The identity in §1 is the fitting basis and it would change.
- Whether anything other than the slash reads the bond-collateralization floor.
- Whether `oi.Average`'s window (`answerWindow`) has other callers that would
  need to move with the draw.

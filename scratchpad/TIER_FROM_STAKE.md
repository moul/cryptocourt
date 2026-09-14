# Tier from stake — replacing the low/mid/high vote with the claim's own size

Owner's proposal, in their words: *"when I stake 10 CC on claim A and claim B,
but claim A only has 100 CC total staked whereas claim B has 1000 CC staked, I
should earn more from claim B than claim A."*

## 1. The gap this closes, verified

A winner's slice, from the code:

    gross = drawWinners × myConv / winPoolConv                 (crystallize.gno:301)
    drawWinners = d × 80/93                                    (crystallize.gno:162)
    d           = min(tier × midGross, budget, reservoir)      (crystallize.gno:86-160)
    midGross    = winPoolConv                                  (crystallize.gno:66-72)

Substituting, while the clamps are slack:

    gross = tier × myConv × 0.86

**The pool cancels.** The same 10 CC earns the same amount on a 100 CC claim and
a 1000 CC claim. Worse, once `want > c.curPeriodBudget` binds, the busy claim
pays *less* per unit — the clamp is the only place claim size enters a staker's
rate today, and it enters with the wrong sign.

`tier` is the only free term in that expression. That is why it is the lever.

## 2. The change

`tier` keeps its type, its range and its position in the formula. Only its input
changes:

    was:  a sealed 3-bucket vote, riding a dispute or a flag
    now:  a step function of the claim's own total stake

Everything downstream — the draw, the answerer cap, `capBonus` — reads `cs.tier`
exactly as it does today and does not change at all.

## 3. What is measured

`xBarFrozen` — the claim's time-averaged TOTAL stake (yes + no), snapshotted at
the answer (answer.gno:5). It is already the base every bond and slash is priced
off, so the tier joins a family rather than inventing a measure.

Time-averaged, not peak, matters here: a flash-stake before the freeze moves a
peak and barely moves an average, so the cheapest way to buy a tier is already
the one the measure resists.

## 4. The bars

Reuse the shape `qualityBars` already has (quality.gno:295): a claim-keyed arm,
floored on a supply fraction so a small claim in a large court cannot reach the
top band, and clamped to a third of votable so the bar is reachable at all.

    high   xBarFrozen ≥ highBar     = max(highBps × supply, …)
    mid    xBarFrozen ≥ midBar
    low    below midBar

The supply floor is the owner's distribution argument made mechanical: you
cannot buy a band without holding a real fraction of the token, and holding it
is what the bar prices.

## 5. The constraint the step sizes must satisfy

This is the part to get right, and it is checkable rather than a matter of
taste.

A holder can lift their own claim's band by staking **both sides** — principal
returns 1× on both, so the losing half costs only time and opportunity. Suppose
they must put up total `2X` to reach a band, with `X` on the side that wins.

    self-staked at the higher band:   payout ∝ tier(high) × X
    honest at the lower band:         payout ∝ tier(mid)  × X

So the manipulation pays if and only if

    tier(high) / tier(mid)  >  (capital needed for high) / (capital needed for mid)

**Therefore: the tier steps must not grow faster than the bars they are keyed
to.** With bands at 1× and 2× the multiplier and bars a factor of 2 apart, the
two sides are equal and self-staking is rate-neutral — the whale doubles their
capital to double their multiplier and earns the same *rate* as an honest staker
one band down. That is the calibration target, and it should be asserted as a
test, not left as a comment.

Note this is a strictly better position than the vote had: a whale could vote a
tier with weight they merely *held*, whereas here they must **lock** it on the
claim, for its whole life, on both sides.

## 6. What it does to emission

The draw becomes superlinear in total stake — deliberately, since that is the
whole point. Two existing clamps already bound it and both stay:

- `want > c.curPeriodBudget` — the per-period budget clamp
- `reserveJunior(c, want)` — the reservoir clamp

Because the top band is 2× and that is what `tier == tierHighX` already meant,
**the ceiling does not move**. Today a claim can reach 2× by vote; after, by
size. The maximum draw per claim is unchanged, which is what keeps this a
re-keying rather than a re-calibration.

The distribution across claims does change: today HIGH is rare because it needs
a ⅔ mandate; after, it is whatever fraction of claims clear the bar. That is the
number to model before choosing bps — §9.

## 7. What is deleted with the vote

Everything in QUALITY_REMOVAL_PLAN.md §4 steps 4-6 still goes: the flag lane,
the slash, the counter window, the sealed tally. The tier survives as a derived
value, so:

- `cs.tier` stays, computed at crystallize from `xBarFrozen` instead of stored
  by a tally. It can stop being a stored field entirely — one fewer thing to
  keep consistent.
- `tierFinal` goes: a derived tier is final the moment the claim freezes.
- `QualityTier` keeps its signature and answers from the derivation, so the
  overlay's read does not move.
- The six traps in that plan's §1 are unchanged and still apply.

## 8. What this cannot do, stated once

Attention becomes value by construction. A heavily-staked claim that is
worthless draws the top band, and nothing left in the system can say otherwise.
That is the trade being made deliberately: one ballot instead of two, at the
cost of the court's ability to overrule the money.

The drain path from TOKENOMICS_SIMPLIFY.md §2 is **not** addressed by this and
gets worse: `xBarFrozen` is a trailing average at the answer, while the draw is
a lifetime integral, and `Unstake` keeps conviction. A mill that banks
conviction and drains before answering now also lands in a low band — which
*reduces* its draw, so this change cuts against the attack rather than for it,
but it does not close it. The bond-collateralization floor that currently
deters it is guarding a slash being deleted, and that is still an open item.

## 9. Before writing code

1. **Model the band distribution** on the demo court's claim sizes: what
   fraction lands low/mid/high at candidate bps. A design where 95% of claims
   are low is a different system from one where 95% are mid.
2. **Pin the neutrality condition from §5 as a test** — the whale's rate at the
   band they bought must not exceed the honest staker's rate one band down.
3. **Decide what LOW pays.** `tierLowX = 0` today, so a low claim draws nothing
   at all. Under a *vote* that meant "the court judged this worthless"; under
   *size* it means "few people staked", which is a much weaker statement to zero
   somebody's return on. A non-zero low band (say 0.5×) is probably right, and
   it is a one-constant decision that changes what the whole system feels like.

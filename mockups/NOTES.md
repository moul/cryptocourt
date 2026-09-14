# Cypherpunk / Blade Runner concepts — running log

**Nothing here is on the site.** These are mockups over a captured snapshot of the
real claim page (`_fragment.html`), so each concept is judged against real
numbers, the real chart and the real ticket rather than lorem ipsum. Vary the CSS,
never the content. Not committed.

## The rule everything is judged against

Kourt is a **court of record**. The register is Blade Runner 2049's *LAPD*
screens — functional, military, minimal iconography, clear labels — and not its
Wallace Corp interiors, which are luxury. Territory Studio's own brief was
"abstract, organic, optical, physical". Legibility of a number beats atmosphere
every single time: if an effect makes a stake percentage, a date or a block
height harder to read, it is wrong and it goes.

## Where the ideas come from

- Territory Studio's BR2049 work: a deliberately limited palette, **amber and
  cyan** as the two accents, and "screen burn suggesting old technology".
- The cypherpunk/brutalist writing: 1px borders, neon or stark white on dark,
  a "structured wireframe" where components feel **physical and engineered**
  rather than floating; terminal fonts and **crosshairs as graphic elements**;
  "Command Line Chic" — keyboard-driven, a command bar instead of navigation;
  a **surveillance** register for privacy tech.
- Restraint is repeatedly named as what separates the good from the costume.

## Concepts

| # | file | idea | verdict so far |
|---|---|---|---|
| 1 | `c1-records-terminal.html` | amber monospace field LABELS, cyan data trace, corner ticks instead of borders | **strongest so far.** Reads immediately as a records terminal and costs nothing in legibility — every number is untouched. The two-hue split (amber = what the page tells you, cyan = what the chain recorded) does real work. |
| 2 | `c2-phosphor.html` | screen burn: a ruling plus a vignette confined to the main column | **revised after doing the arithmetic, and the arithmetic forced a retreat.** The shipped alpha failed WCAG AA for ordinary body text, not just the rail footer I had noticed. Now every case passes; the effect survived but is the mildest thing in the gallery. |
| 3 | `c3-crosshair.html` | reticle on the chart's "now" point; corner brackets on hover; registration ticks in the margin | **revised, and the revision is the point.** Brackets read well — precise and quiet. But looking at it properly caught two things the first screenshot hid: the margin ticks were painted 14px *inside* the opaque rail and so were never visible at all, and the "reticle" was a *smaller* dot (r:3) than the one it replaced (r:4) — I shrank the thing meant to draw the eye. Ticks moved into the column's own padding (x=292, rail ends 282, text starts 333); the dot is now a stroked ring at r:5. |

| 4 | `c4-wireframe.html` | 1px rules, no fills, square corners, no shadow — plus tabular figures | **the opposite bet from 01**: 01 adds marks, this takes surfaces away, so the page becomes a drawing rather than a stack of cards. Verified applied: fills `rgba(0,0,0,0)`, shadow `none`, radius `0`. Two honest caveats below. |

## What looking at things has cost so far

Two of the three concepts had an element that did nothing, and neither was visible
in the first screenshot I took of it:

- **c3's margin ticks** were positioned at `left:-14px` on `.main`, whose left edge
  is *exactly* the rail's right edge — so they rendered underneath an opaque
  sidebar. Measuring the pseudo-element's absolute x against the rail's right edge
  is the only way that shows up.
- **c3's reticle** was smaller than the dot it replaced. "It applied" and "it does
  what I meant" are different claims, and the computed value only proves the first.

Both are the same lesson as the live-site work: render it, measure it, and compare
against the thing it replaced — not against the intention.

| 5 | `c5-microfiche.html` · `-docket` | the OTHER half of Territory's reference pile — E-Ink, microfiche, optical lenses, not neon. Near-monochrome plate, no glow at all, ink trace. | **the reflective option, and measurably the most legible.** Trace contrast 16.83:1 against paper where c1's cyan is 10.27:1. YES/NO keep their hues because those are the only colours carrying meaning. One self-inflicted defect, found and fixed — see below. |

| 6 | `c6-composite.html` · `-docket` | **not a sixth aesthetic** — the elements that measured well, assembled: amber mono labels + corner ticks (01), no fills + ruled stats strip (04), reticle ring (03), .10 ruling (02) | the one meant for picking from. Busier than 01 alone but not maximal, because every element does a different job: labels are hierarchy, ticks are registration, cyan is data, the ring is the point. |

| 7 | `c7-switchboard.html` | **every element from 01–06 as an independent switch**, over BOTH captured pages, with the six concepts as presets | the one to actually use. The gallery could only offer whole treatments; "pick and choose" needs the pieces separable. The readout at the bottom of the panel is literally what to tell me. |

| 8 | `c8-filed.html` · `-docket` | the register the brief asks for, taken literally: the stamp, the seal, the case number, FILED. A court's own visual language, which predates all the science fiction. | the stamp works — double-ruled and tracked at 1.89px, it reads as pressed rather than drawn. Deliberately **no rotation**: a tilted stamp would be the one purely decorative thing in the gallery and fights every alignment on the page. |

## Two things c4 taught, neither of which was the intention

**The tabular-figures claim is RETRACTED, after two attempts to save it.** The
change is real — proportional digits drift 7.88px between `111` and `888`,
tabular drift 0.00. First I said the claim page could not show it and captured
the docket to prove it. Then I measured the docket's %-YES column: `rightSpread:
0.00`, every percentage sharing the same left AND right edge. They were already
aligned, by the grid, before tabular figures did anything. So the payoff is
invisible in Kourt's tables because every numeric column here is already
box-aligned by layout. It stays in the concept as a cosmetic no-op at worst, but
it is not a legibility argument and I should stop making it.

**The docket capture was still worth it** for the other half: `.grid.stats`
measured `statsCells: 0` against the claim page, i.e. entirely untested. Ruled
cells instead of tinted blocks is the single best thing in this concept, and it
could not be seen at all until the court page was in the gallery.

**Removing the fills makes the shipped background grid much more prominent.** The
grid is already live on the site at .055 alpha, and it was tuned against panels
that COVER most of it. Take the fills away and it shows through everywhere, which
happens to look like a blueprint and is the best accident so far — but it means
c4 and the shipped grid are coupled. Anyone adopting c4 should re-tune the grid
alpha at the same time, not after.

**Fixed since:** the rules were `--line-2` on near-black — the faintest thing on
a page whose whole claim is "engineered". They are accent-tinted hairlines now
(`rgba(154,168,238,.30)`), and the ruled stats strip reads properly.

**Also fixed, and only measurement found it:** the docket ROWS were transparent
but `div.docket` behind them still painted `--surface`, so "no fills" was false
and the folder block still read as a tinted slab. The screenshot told me
something was tinted; walking the ancestry told me what.

**One false alarm, recorded so the probe improves:** I flagged a white border on
the first stats cell from `borderLeftColor: rgb(230,237,242)`. Its
`borderLeftWidth` is `0px` — nothing is painted, the colour is just the unset
default. Read the width, not only the colour.

## What c5 cost, and the rule it had to break

**Turning the accent into ink killed the link affordance.** Measured: a tagrow
link came out `rgb(230,227,219)` against body text `rgb(242,240,234)` — the same
colour to any eye — and `textDecorationColor` was `rgba(0,0,0,0)`, because the
site's convention is underline-on-HOVER with colour carrying the link at rest.
Remove the colour and nothing marks a link at rest except the `→` glyph. That is
WCAG 1.4.1, and I introduced it by writing `--accent:#e6e3db`.

Fixed with an always-on underline, which is the one place a concept should break
the site's rule rather than bend to it: a printed plate underlines its
references, so this is the concept, not a compromise with it.

**Small artefact, left alone:** the underline breaks into three runs across
"filed under / Municipal record · Elections / · sample", because `.tlink` is
`inline-flex` and each child underlines separately. Reads fine; worth knowing
before anyone ships it.

**What this concept is FOR.** Every other option in the gallery is emissive. If
the answer to "how much neon" turns out to be "none, but colder and sharper",
this is that answer, and it is the only one here that improves contrast rather
than spending it.

## c2: the number that settled it

A multiply overlay turns a pixel into `base*(1-a)`. The WCAG formula has a `+0.05`
floor, so scaling text AND background down together does not preserve their ratio
— it pushes it toward 1. Computed, for the rail footer (`--muted` 12.5px) and for
ordinary body text (`--ink`) on `--paper`:

| vignette alpha | rail footer | body text |
|---|---|---|
| 0.00 (none) | 6.17:1 | 15.27:1 |
| 0.25 | 3.88:1 | 8.82:1 |
| 0.40 | 2.83:1 | 5.88:1 |
| **0.55 — what I shipped** | **2.03:1** | **3.67:1** |

AA for small text is 4.5:1. So the first version failed AA for BODY TEXT in the
corners, and I had only noticed the rail. Worse, keeping the footer at AA caps the
alpha around 0.15 — invisible. **The effect and the content cannot share the same
pixels**, so the fix had to be structural rather than a tuning:

- the vignette moved from `body` to `.main`, transparent to 78%, reaching only
  .42 at the extreme corner — which is outside the text column at every
  supported width;
- the rail is exempt entirely (its footer is the least robust text on the page and
  sits exactly where a full-screen vignette is darkest);
- the ruling dropped to .10.

After: body text 12.46:1, rail footer 5.18:1, and even the theoretical worst case
of ruling-plus-full-corner-vignette is 4.64:1. All pass.

**But be told plainly:** having retreated that far, this is now the least
assertive concept here. Whether that is "subtle, done right" or the invisible
mistake I already made once with the neon is a judgement call, not a measurement,
and it is the owner's to make.

## c6, and the measure I nearly used wrongly

**What went in, and what did not.** In: amber monospace labels and corner ticks
(01, cost nothing), no panel fills and the ruled stats strip (04, its best single
element and invisible until the docket was captured), the reticle ring (03), the
.10 ruling (02). Out: 02's vignette, which after the AA arithmetic adds least per
unit of risk; 04's tabular figures, retracted; and all glow on chrome.

**The one real conflict, stated rather than fudged.** 01 wants the trace CYAN so
two hues do two jobs — amber is what the page tells you, cyan is what the chain
recorded. 05 wants it INK and wins on contrast: 16.83:1 against paper versus
cyan's 10.27:1. Cyan is chosen anyway, because the semantic split is worth more
than six points of ratio when both are far above AA, and because the trace is a
2px line whose job is to be identifiable rather than readable. If that trade is
wrong, c5 is the version that takes the other side — that is what it is for.

**The methodological catch.** Borrowed straight from 03, the ring was teal, which
put a teal ring at the end of a cyan trace and read as mush. Reaching for WCAG
ratio to choose a replacement gave teal-on-cyan 1.16:1 and amber-on-cyan 1.11:1 —
amber marginally WORSE — which would have kept the broken version. But WCAG ratio
is a LUMINANCE metric for text against a background. Two adjacent graphic marks
of similar lightness are distinguished by HUE: teal is 20 degrees from the trace,
amber is 150. Amber, and the ring now reads unmistakably.

Worth remembering the next time a number is used to settle a design question:
check that the number measures the thing being asked about.

## c7: why a switchboard, and what it cost

Six concepts in, the gallery could answer "which of these six" and nothing else.
But the brief was to pick and choose ELEMENTS, and no page here let you see the
amber labels without also taking the corner ticks. So: thirteen switches, one per
element, over the same captured claim page, with 01–06 as presets and a readout of
what is on.

**Cyan and ink are one switch-slot, not two.** They are two answers to the same
question — what colour is the recorded path — so ticking one unticks the other,
in the class list AND in the checkbox. Verified.

**The rule this file bends, stated plainly.** It carries a control panel and about
thirty lines of script. The CONTENT is still the untouched fragment, which is what
the same-content rule exists to protect; the switches only add and remove classes
on `<html>`.

**The cost it shipped with, and the fix.** The panel was `position:fixed` at 330px
and sat directly over the right-hand ticket column — "Stake the claim" and its
buttons were behind the controls, so the switchboard hid the thing it exists to
help you judge. The content reserves room now (`padding-right` on body while
open), and the panel folds to a tab so the page can be seen whole. Verified: the
ticket no longer intersects the panel, no horizontal scroll, folding restores the
full width.

**Ten behavioural checks, not a screenshot.** For an interactive artefact,
"it renders" is not "it works": unticking changes a computed style, the class
comes off, the exclusion holds in both directions, a preset sets exactly its own
elements, and the readout matches. All pass.

## c7, second pass: a switch that did nothing where you were standing

The tool had a switch for the ruled stats strip and no stats strip to apply it
to — my own notes for c4 said "needs the docket to see", and I built the
switchboard over the claim page anyway. So it now carries BOTH captures and a
claim/docket toggle, and any switch with no visible effect in the current view is
dimmed with a tooltip saying which view to go to. "This does nothing here" is
more useful than a switch that silently does nothing.

Verified rather than asserted: the claim view really has 0 stats cells, the docket
view has 4, the ruled-strip switch produces a 1px border only in the docket view
and is dimmed in the other, exactly one view is displayed at a time, and neither
view scrolls sideways.

**AND IT SHIPPED BROKEN FOR ONE ROUND, which is the lesson.** Adding the view
toggle introduced `TypeError: Cannot read properties of undefined (reading
'classList')`: `view('claim')` ran before `var R = document.documentElement` was
assigned twenty lines lower. `var` hoists the NAME but not the assignment, so R
existed, was undefined, and the exception killed the entire script — every switch
dead, the panel collapsed to zero-size controls.

The page still RENDERED, perfectly. A screenshot would have shown a normal claim
page and told me nothing. This is the second time in this loop that "it renders"
and "it works" came apart (the first was c3's margin ticks painting under the
rail), and it is the argument for the behavioural checks: ten on the switches,
ten more on the views, and they are what caught it.

## The narrow audit — everything above had only ever been judged at 1280px

Ten files, 390px, dark. `documentElement.scrollWidth` was 390/390 on every single
one: no horizontal overflow anywhere. Then one of them turned out to be
destroyed, which is the finding.

**c3 is fine.** The margin ticks land at x=10 and the text starts at 18 — 8px of
clearance with the rail collapsed. Tight, but it holds.

**c7 was catastrophically broken and reported clean.** The panel reserved 368px
of a 390px viewport, leaving `contentWidth: 36`. The claim title measured ZERO
pixels wide and 39 lines tall. And `scrollWidth` still said 390/390, because
nothing overflowed — the content had merely been squeezed out of existence.

    "no horizontal scroll" is not "it works"

That is the third time in this loop the two came apart: c3's ticks painting under
the rail, c7's script dying on a hoisted `var` while the page rendered perfectly,
and now a layout passing the overflow check while having no layout left. The
common shape is that the cheap global check (does it overflow? does it throw?
does it render?) is necessary and never sufficient — the thing has to be measured
where it is supposed to do its job.

Fixed: below 900px the panel overlays instead of reserving, spans the width, and
starts FOLDED so the page is what you see first. Verified at both widths —
390px: content 390, title 354, folded, and opening overlays rather than squeezing;
1440px: content 782, still reserving its 368.

**One probe corrected too.** I had asserted the reading column must exceed 55% of
the viewport, which failed a perfectly correct desktop layout: at 1440 the rail
takes 282 and the panel 368, so 782 is exactly right. A percentage of the viewport
was never the invariant — "the reading column is still a reading column" is.

## The light-theme audit: the whole gallery is dark-only, measured

Every distinctive mark in every concept is a light-on-dark colour, and on the
site's real light paper (`#eaeef1`) all of them are invisible:

| mark | dark value | on dark | light counterpart | on light |
|---|---|---|---|---|
| amber label | `#e8b04b` | 9.24 | `#7d5c0f` | 5.28 |
| cyan trace | `#5fd3e6` | 10.27 | `#0f6b7a` | 5.29 |
| teal reticle | `#7fe3d0` | 11.86 | `#0d6b5a` | 5.51 |
| ink trace | `#f2f0ea` | 15.84 | `#1b1b1f` | 14.71 |
| hairline rule | `#9aa8ee` | 7.92 | `#2e3a7d` | 8.92 |

The dark values on light paper score **1.02–1.95:1**. Three of the counterparts
are already tokens in the live light theme (`--escrow`, `--accent`, `--ink`),
which is the tell that pairing is this site's own discipline rather than a new
one. c1–c6 stay dark-only demonstrations on purpose; the SWITCHBOARD is the thing
that has to survive the toggle, because it is the thing that gets used — so its
concept colours are paired per theme and it has a dark/light switch.

**And the light view was completely blank, which none of the numbers caught.**
Adding a second `.views` row for the theme pair broke the view handler: it was
bound to `#sw .views button` — unscoped — so clicking "light" ran `view(undefined)`,
removed `v-claim` AND `v-docket`, and displayed nothing at all. No console error.
Every contrast assertion still PASSED, because it measured the colour of marks
that were no longer on screen.

That is the fourth "it looks fine / the number is fine, and it is broken" of this
loop, and the sharpest: a passing measurement of an absent thing. The test now
also asserts that a view is still displayed and the page still has width after a
theme change — which is what it should have asserted from the start.

**My probe had the identical bug**, reading all four buttons and expecting two.
Both are scoped to `[data-v]` now, and there is a new check that changing the
view leaves the theme buttons' state alone.

## e-mono, the switch I was offering without ever having looked at it

It sat in the switchboard labelled "untried" for three iterations. Offering a
switch I had not evaluated is worse than not offering it, so: measured.

**Its actual cost is modest.** Characters per line in the longest prose block
37 -> 33 (-11%), that block 8 -> 9 lines, claim page height +4%. The serif title
in mono reads as a records terminal and loses some of the court's gravity, which
is a taste call rather than a defect.

**I nearly blamed it for damage it does not do.** From the screenshot I read the
ticket's value column stacking — "returns 1x, either way" over four lines — as
mono's fault, and was about to reject the switch for it. Measured with the switch
OFF: 4/3/3 lines, identical. The stacking is the SWITCHBOARD PANEL reserving
368px and squeezing the right-hand column. Folding the panel takes those rows
from [4,3,3] to [1,1,1].

Two lessons, both cheap: read a difference by measuring BOTH states, not by
looking at one; and an instrument that changes the specimen will be blamed for
what it did to it.

**A real defect it did have.** The switch was written on `html, body, p`, so it
restyled the switchboard's own controls as well as the page — measured, the
panel's font went `-apple-system` -> `SF Mono`. It is scoped to `.main` now, so
it changes the specimen and not the instrument.

**And the panel now says so**, in the hint where it will be read: fold it to judge
the layout properly, because while it is open the 368px reservation cramps the
ticket. That is the honest caveat on the whole tool, not a property of any
concept in it.

## The third view, and a hand-kept list that was simply wrong

Elements get adopted SITE-WIDE, and every switch had only ever been judged on two
of the site's six routes. So the directory is captured now as well, and it is a
genuinely different shape: 3 stats cells, 2 court rows, and NO panels, NO ticket,
NO chart.

**That immediately exposed a lie the tool was telling.** `e-ruled` was marked
"docket only" from a hand-kept array, and the panel dimmed it on the claim page
saying "switch to the docket". The directory has a stats strip too, so the
guidance was false. Applicability is COMPUTED now: each switch names the selector
it targets, and whether it can bite in the current view is a DOM question asked
fresh every time. It cannot drift out of date, and adding a capture adds a view.

Measured, per view — dimmed means "nothing here for it to affect":

| view | stats cells | panels | chart | dimmed |
|---|---|---|---|---|
| claim | 0 | 3 | 1 | `e-ruled` |
| docket | 4 | 4 | 0 | `e-cyan` `e-ink` `e-ring` |
| directory | 3 | 0 | 0 | `e-ticks` `e-cyan` `e-ink` `e-ring` `e-brackets` |

**And adding the third view broke view switching, exactly as adding the theme row
broke it before.** `view()` was two hard-coded lines — toggle `v-claim`, toggle
`v-docket` — so `view('directory')` set no class at all, nothing displayed, and
the applicability check then had no scope to query and reported EVERY switch as
live. It is generic over the views present in the document now.

The assertion that caught it is the one added last iteration for precisely this
reason: "exactly this view is shown". It is the fifth catch in this loop, and the
second where a broken thing produced a MORE reassuring result than a working one
— every switch looked live because the page was empty.

Two probes were stale rather than wrong-headed and were updated: the dim reason is
no longer "switch to the docket" (that phrasing was the inaccuracy), and there are
three view buttons to track, not two.

## c8, and a defect I predicted that was not there

The idea: every other concept borrows from science fiction, and a court of record
has its own visual language that predates all of it — the stamp, the seal, the
case number. Territory's pile had rolodex cards and microfiche in it for the same
reason: bureaucratic apparatus reads as authority.

**What I expected to have broken.** Setting the breadcrumb as a case number —
gold, mono, uppercase — should have destroyed the link affordance the way c5's
ink accent did, because "Directory" and "Orem Truth Court" are navigation.

**Measured, it did not.** The crumb ANCHORS are still `rgb(163,178,189)` at
8.31:1, identical to every other concept, because `.crumbs a` sets its own colour
and an `!important` on the container does not reach past it. Only the separators
and the current page — which are plain text, not links — went gold.

Which produced something better than the thing I designed: **gold marks where you
are, grey marks where you can go.** That was an accident of specificity, not a
plan, and it is worth saying so rather than claiming the credit.

**One thing deliberately not done.** A rotated stamp is the obvious move and it is
exactly the costume this brief warns against: the only purely decorative element
in the gallery, fighting every alignment on the page, and a court that looks
whimsical about its own record is making a claim it cannot support. The authority
comes from the double rule and the tracking, which cost nothing.

## c8's elements folded into the switchboard

The switchboard is meant to be the single place a decision gets made, and it did
not offer the newest concept's elements at all. Four added — `e-stamp`,
`e-casenum`, `e-formcaps`, `e-seal` — plus an "08 filed" preset. Seventeen
switches now.

**The gold is paired like everything else.** `#c9a227` is 7.46:1 on dark and
2.07:1 on light, so it fails outright in the light theme; the counterpart is the
site's own `--escrow` (`#7d5c0f`, 5.28:1). That is the fourth mark whose light
value turned out to already exist as a site token, which keeps saying the same
thing: pairing is this codebase's existing discipline, not an imposition.

**And the computed applicability paid for itself.** `.seal` exists in exactly one
of the three captures — the docket — so `e-seal` dims itself on claim and
directory and goes live on the docket, with nothing hand-maintained. Verified per
view: 0 seals / dimmed, 1 seal / live, 0 seals / dimmed. Had this still been the
hand-kept array from two iterations ago, I would have had to know that fact and
would probably have got it wrong, exactly as I got `e-ruled` wrong.

**One probe rotted and was fixed properly.** `sw.js` asserted "all 13 elements
have a switch" and "7 are checked" — both hard-coded, both wrong the moment four
switches arrived. They now assert the invariants instead: every switch has a row,
and every checkbox agrees with the class list whatever the default set is. A test
that has to be edited every time the thing it tests grows is a test that will
eventually be edited into agreement with a bug.

## c1 audited at last, and three fixture faults it exposed

c1 was called "strongest so far" after one look at one page, and then left alone
for eight iterations while weaker concepts got audited. Giving it the same
treatment turned up more in the FIXTURES than in the concept:

**The concept itself, corrected.** It now exists on all three views. Its colours
are paired (amber 9.24 dark / 5.28 light, cyan 10.27 / 5.29) — the concept most
likely to be adopted was the only one that could not survive the theme toggle.
And it carried `position:relative` on the stats cells for corner ticks that were
never drawn on them: dead CSS, removed, because a dead rule later reads as intent.

**Worth knowing before adopting it:** c1's cyan is INERT on the docket and the
directory, because neither page has a chart. Two thirds of the site sees only
half of this concept. That is not a fault, but it is a fact about what adopting
it buys.

**The directory capture had no rail.** Claim and docket both include it; the
directory took only `.main`, so that view rendered full-width — not what the site
does — and nothing touching the sidebar could be judged there at all. Recaptured
with the rail, and the switchboard's inlined copy replaced.

**Three elements shared `id="main"`, and the search box ids were doubled.** With
three captures in one document: `main` x3, plus `q`, `qscope`, `qcap`, `qempty`
x2 from the docket's and directory's search widgets. Invalid, and
`getElementById` would always answer with the first — so on a page where only one
view is visible, the site's own wiring would address the wrong one. The ids are
namespaced per view now rather than deleted, which keeps each capture internally
consistent (`for=`, `aria-controls`) instead of merely quiet.

**And my own script matched the stylesheet instead of the markup.** `index('data-view="directory"')`
found `html.v-directory .main[data-view="directory"]{...}` in the CSS, so the
`rindex('<main')` before it threw. Twice, in two different ways, because I also
assumed `<div>` where the fragment uses `<main>`. Anchoring an edit on a string
that appears in both the CSS and the HTML is a trap worth naming.

## c9: a command palette, and what it took to make it honest

The last unused idea from the research and the only *interaction* idea in the
sources: a ⌘K/`/` palette over the real claim page. Built, then looked at, and
the screenshot showed three things the behavioural tests could not:

**The labels were mashed.** `Infrastructure3 claims 3claims folder`. The scraper
took `textContent` off each anchor, and the docket's folder rows carry their
meaning in STRUCTURE — name, count, badge, kind, in nested spans. Flattening
destroyed the meaning and glued the words together in one move. The same class
of bug as the live site's `filed underMunicipal record`: whitespace between
elements is not free. Fixed at the source (`_scrape-targets.py` reads each row
kind for the field that names it), which also revealed the first scrape had
SILENTLY DROPPED a whole court (`#/c/ledger`) and the first folder — 23 targets
where there were 26. A test asserting `n===23` passed against a list missing
three real destinations; it now reads the count from the scrape.

**The titles were truncated in the scraper at 44 characters**, cut mid-word, no
ellipsis: "The bridge inspection rated the north span '". Truncation is the
renderer's job — CSS knows the pixel width and can say a cut happened. Titles
now arrive whole and 7 of 26 ellipsise by pixel.

**And the route column was not a column.** With flex each row sized its own
route cell, so the labels began anywhere in a **106px band** (measured) and the
list could not be scanned. `grid-template-columns: subgrid` on each `<li>` sizes
the route column once, to the longest route, with no magic number: all columns
now measure spread 0.

Two more, both from measuring rather than looking: the list is taller than its
52vh cap (scrollHeight 766 against a 322 viewport), so arrowing down walked the
selection out of sight — `scrollIntoView({block:'nearest'})`. And the targets
held pre-escaped `&amp;` while being injected as HTML, so it rendered right by
luck while the query matched the entity instead of the word a reader can see.
Text in the data, escaped on the way out.

**A thing I retracted.** From the screenshot I judged the list ran past the fold
and buried the hint. Measured: box bottom 483 of a 620 viewport, foot fully on
screen. The screenshot had been taken at a taller viewport and cropped. The eyes
proposed it, the measurement refused it — the same discipline in the other
direction for once.

**And a trade I made and then reversed.** The dim secondary (folder name, claim
count) ran on after the title in the same cell and its left edge wandered over a
478px band, so I gave it its own column. Perfectly aligned — and the title
column went 393px → 230px, taking the ellipsised rows from 7 to 11 of 26. Wrong
trade: the claim titles ARE the content and the folder never disambiguated two
claims. So claims carry no secondary at all and keep the width; courts and
folders keep a count, which is the one thing the route does not already say.
Alignment is worth paying for; alignment of metadata is not.

While measuring the first version I also proved a probe vacuous on the spot: a
CSS-only `display:contents` test of the four-column idea reported `cut:0` and
`titleW:0` — an improvement produced by the measured thing ceasing to exist.
Fifth entry in this log for that failure mode.

## c10: THE SELECTION — the ten that were picked, as one design

    ON: e-ticks e-cyan e-nofill e-ruled e-ring e-brackets e-ruling e-grid
        e-casenum e-seal

Off, and the omissions say as much as the picks: `e-labels` (section heads and
stat labels keep the site's own type), `e-mono`, `e-stamp`, `e-formcaps`,
`e-vignette`, `e-ink`, `e-marks`. Read together the set is coherent, which is
why it is worth building as one design rather than as ten toggles: surfaces lose
their fills and become hairline frames with registration ticks, the chain's data
is the only glowing thing on the page, and gold is spent ONLY on provenance —
the docket reference and the court's seal — never on state.

**FOUR views, one of them new.** Claim, docket, directory, and — asked for by
name — the SHARE CARD, `#/embed/orem/1`, which had never been captured. The most
widely distributed surface on the site was the one surface no concept in this
gallery had ever been judged on. `_capture-embed.js`, at the 400×500 the embed
snippet actually asks for.

### The share card's graph was not the claim page's graph

Four differences, three cosmetic and one a fidelity bug:

1. **The trace was green.** `.eline` is `var(--good)`; the claim page's `.ln` is
   the accent. The card that travels into somebody else's article plotted the
   same series in a different colour from the page it links to.
2. The endpoint was a filled green disc, not the amber ring.
3. The area fill was green at .10.
4. **The geometry was wrong.** The svg carries `preserveAspectRatio="none"` and
   the CSS pinned `height:56px`, so a 300×56 viewBox was stretched to the card's
   width: every slope in the trace flattened by ~13%, and the endpoint "circle"
   was drawn as an ellipse. `aspect-ratio:300/56` makes the scaling uniform —
   measured, the dot's box goes to 11.04×11.04, square. A chart whose slopes
   depend on the width of the iframe is not a chart. Costs 8px of card height;
   the card still fits its frame (400×498 of 400×500).

The BAR needed nothing: `stakeBar()` is literally the same function on both
surfaces and `.sbar`/`.sbout` are the same CSS, so it already matched. Only the
graph had drifted.

Deliberately NOT copied over: the 0/50/100 tick labels, the lifetime reference
line and the event marks. They are in-SVG text and annotations on a 640px chart;
the card is 368px wide and has no elements for them. The dashed rules at 25/50/75
carry the scale. Same instrument, fewer readings — the honest version of "the
same".

### Two things the render refuted

**Ticks instead of borders does not survive a tall panel.** `e-ticks` says it
replaces the border and `e-nofill` draws one anyway; the later rule won in the
switchboard, so nobody was ever shown the choice. I resolved it toward the
stated intent and looked: the claim page's left panel is a grid item stretched to
the ticket's height, so four 10px marks sat ~960px apart with a ~500px void
between the chart and the bottom pair. They read as stray marks outlining
nothing. Hairline restored — and that is also what the switchboard was showing
when these ten were picked, so it is what was actually approved. Third time in
this log the accident has beaten the design.

Also, nested boxes both took marks: the docket nests a buy ticket inside the
"join this court" panel, two amber corners 170px apart. A registration mark marks
the sheet, not every box drawn on it. Only outermost frames are ticked now
(measured: nested 1, nested-and-ticked 0).

**Two of the ten do not cross onto paper.** In light mode the share card came out
striped like fax paper end to end. Every contrast ratio still passed AA — worst
4.86 — and it still looked wrong, which is the entire reason for looking as well
as measuring. The ruling is a CRT artifact; on paper the same 1-in-3 black rows
are a fax artifact, a different metaphor, and 10% black on near-white is a huge
visible step where on near-black it is imperceptible. A glow is light EMITTED,
and ink does not emit — the hard-coded cyan halo was painting a pale ring around
a dark teal line on white. Both are tokens now, `none`/`transparent` in light,
which is already what every neon token in the live light theme does.

That fix then exposed my own measurement as stale: the harness folded a
hard-coded `.10` multiply into the LIGHT ratios too, reporting numbers no reader
would ever see. It reads `--scan` now. Light-theme ratios are 5.22–14.13, not the
4.86–11.7 I had been quoting.

### What adopting the ten actually buys

Measured per view: on the claim page and the share card, `e-ruled` and `e-seal`
do nothing — there is no stats strip and no seal there (`statsCells: 0`,
`seal: false`). `e-grid` is dark-only by the site's own token. So the claim page
sees seven of the ten and the share card six. Not a fault; just less than the
switchboard makes it look like.

Strongest of the set, on the evidence of the renders: `e-ruled` on the docket —
four separate stat cards become one ruled table — and `e-casenum`, where the
crumb links keeping their own colour means gold marks where you *are* and grey
where you can *go*.

## c10 audited: three defects, all of them caused by the ten

The first pass had never been *looked at* in three places — the hover state (and
`e-brackets` is one of the ten, with an effect that exists nowhere else), the
directory, and everything below the claim-page fold. All three hid something.

**`e-brackets` works, and that is worth recording** because it was the one
element I had shipped into the concept sight-unseen: at rest 10px amber at .55,
on hover 14px teal at .85 on both corners, and nested boxes stay dark. First
thing here that measured exactly as designed.

**The aside stopped being an aside.** `.ticket .never` — "Reward = conviction,
not stake", the one paragraph on the page that corrects the thing readers get
wrong — is separated from the ticket's body copy by NOTHING but its `--inset`
fill. `e-nofill` strips fills, so it became indented body text sitting directly
under the payoff table and read as a fourth row of it. `.notice` came through the
same treatment intact because it carries a 4px left border instead of a fill
(measured: still 4px, still gold). So the fix is the site's own answer to the
same problem, in the concept's own language: an aside is marked by a rule in the
margin, not by a panel.

**A doubled hairline in all six docket boxes.** My `e-nofill` separated rows with
`border-bottom`, which is the same line in every position except the last — there
it lands 1px above the container's own bottom border. I proved whose fault it was
rather than guessing, by toggling the concept's stylesheet off in the live page:
base CSS gives the last row `border-bottom: 0px` **deliberately**, and the
`!important` put it back. The site's own idiom is `border-top` cleared on
`:first-child`, which is correct at any row count; the concept uses that now.
Six boxes across the docket and the directory, every one of them wrong before.

**And a frame around nothing.** `.panel` and `.ticket` are equal-height grid
items, so the chart's panel was stretched to the taller ticket: content ended at
575px, the box at 880px. With a fill that void read as a surface; as a hairline
frame it reads as an empty box, and the corner ticks obligingly marked its
corners. `align-self:start` — 604px → 329px, void below content down to 31px
(the panel's own padding), and the ticket does not move (both still start at
y=278).

That last one is the only declaration in c10 that is **layout rather than skin**,
and it is worth flagging as such: it exists only because `e-nofill` turns an
invisible stretch into a visible one. If touching the layout is off the table,
the alternative is to let `.panel` keep its fill and accept that one surface.

## Rejected, and why

- **Neon on everything.** Already learned on the live site: the first neon pass
  was so restrained it was invisible, the second is right. A third step up would
  be a slot machine. Glow belongs on data, never on chrome.
- **Animation of any kind.** A pulsing interface is a toy; this is a record.
- **Cyan as the single accent.** Replacing indigo wholesale is a palette change,
  and the owner likes the palette. Cyan is used only where it means "live data".

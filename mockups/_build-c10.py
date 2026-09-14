#!/usr/bin/env python3
"""c10 — THE SELECTION. The ten switches the owner turned on, baked in.

    ON: e-ticks e-cyan e-nofill e-ruled e-ring e-brackets e-ruling e-grid
        e-casenum e-seal

Off, and worth naming because the omissions are as much of a decision as the
picks: e-labels (so section heads and stat labels keep the site's own type),
e-mono, e-stamp, e-formcaps, e-vignette, e-ink, e-marks.

Read together the set says something coherent, which is why it is worth building
as one design rather than as ten toggles: the surfaces lose their fills and
become hairline frames with registration ticks, the chain's data is the only
glowing thing on the page, and gold is spent ONLY on provenance — the docket
reference and the court's seal — never on state. e-stamp and e-formcaps were
available and were left off, so pills and section heads stay as they are. Gold
means "this is which record you are looking at", not "this is what is happening
to it".

FOUR views, because a design is not adopted on one page: the claim, the court
docket, the directory, and — new, and asked for by name — the SHARE CARD, the
one surface built to travel into somebody else's article.
"""
import sys, pathlib
sys.path.insert(0, str(pathlib.Path(__file__).parent))
from _shell import build

BLURB = """   THE SELECTION — the ten elements picked out of the switchboard, as one design.
     ticks · cyan trace · no fills · ruled stats · amber ring · hover brackets
     · scanline ruling · background grid · gold case number · gold seal

   WHAT IT COSTS. Three things, all measured, none of them free:

   1. The panels lose their fill but KEEP a hairline frame, with the corner ticks
      as reinforcements on it. Built the other way first — ticks instead of a
      border, which is what e-ticks says it does — and the render refuted it: the
      claim page's panel is a grid item stretched to 958px, so the four marks sat
      ~960px apart around a 500px void and read as stray, not as a frame.
   2. The ruling is a multiply layer over everything, so every contrast ratio on
      the page drops. Measured below rather than assumed.
   3. e-seal only exists on the court docket, and e-ruled only where there is a
      stats strip. On the claim page and in the share card two of the ten
      elements do nothing at all. That is not a fault, it is what adopting them
      buys: less than it looks like from the switchboard."""

CSS = r"""
/* ---------------------------------------------------------------- tokens
   Paired dark/light, because the switchboard proved this is the part concepts
   get wrong: the same dark values on light paper score 1.02-1.95:1, invisible.
   Three of the light counterparts are already tokens in the live theme
   (--escrow, --accent, --ink), which is the tell that this is the site's own
   discipline rather than a new one. */
:root[data-theme=dark]{
  --amber:#e8b04b; --cyan:#5fd3e6; --retic:#7fe3d0;
  --stamp:#c9a227; --stamp-2:rgba(201,162,39,.5);
  --rule:rgba(154,168,238,.30); --rule-2:rgba(154,168,238,.16);
  --trace-glow:drop-shadow(0 0 3px rgba(95,211,230,.85)) drop-shadow(0 0 11px rgba(95,211,230,.4));
  --ring-glow:drop-shadow(0 0 4px rgba(232,176,75,.85));
  --scan:rgba(0,0,0,.10);
}
/* TWO OF THE TEN DO NOT CROSS ONTO PAPER, and the stylesheet already had the
   precedent: every neon token in the live light theme is none/transparent, and
   --gridline is transparent there too. Rendered in light mode with them on, the
   share card came out striped like fax paper end to end — every ratio still
   passed AA (worst 4.86) and it still looked wrong, which is the whole reason
   for looking as well as measuring.
     · the ruling is a CRT artifact. On paper the same 1-in-3 black rows are a
       fax artifact, a different metaphor, and at 10% on near-white it is a huge
       visible step where on near-black it is imperceptible.
     · a glow is light EMITTED. Ink does not emit, and the hard-coded cyan halo
       was painting a pale ring around a dark teal line on white. */
:root[data-theme=light]{
  --amber:#7d5c0f; --cyan:#0f6b7a; --retic:#0d6b5a;
  --stamp:#7d5c0f; --stamp-2:rgba(125,92,15,.55);
  --rule:rgba(46,58,125,.34); --rule-2:rgba(46,58,125,.18);
  --trace-glow:none; --ring-glow:none; --scan:transparent;
}

/* ---------------------------------------------------------------- e-nofill
   No fills anywhere. The page becomes rules and type, which is what makes the
   one glowing trace mean something. */
.panel, .ticket, .grid.stats>div, .rawblock, .notice, .never,
.docket, .docket a.crow{
  background:transparent!important; box-shadow:none!important; border-radius:0!important}
/* SEPARATE ROWS THE WAY THE SITE ALREADY DOES: a border on the TOP edge,
   cleared on the first row. The first version put it on the bottom, which is the
   same line in every position but the last one — there it lands 1px above the
   container's own bottom border and draws a doubled hairline. Measured, with the
   concept's stylesheet toggled off to prove whose fault it was: base CSS gives
   the last row `border-bottom: 0px` deliberately, and the !important put it
   back. Six boxes across the docket and the directory, every one of them. */
.docket a.crow{border-bottom:0!important; border-top:1px solid var(--rule-2)!important}
.docket a.crow:first-child{border-top:0!important}
.rail{background:transparent!important; border-right:1px solid var(--rule)}
.nav a.on{background:transparent!important; box-shadow:inset 2px 0 0 var(--accent)!important}

/* THE ASIDE THAT STOPPED BEING AN ASIDE. `.ticket .never` — "Reward =
   conviction, not stake", the one paragraph on the page correcting the thing
   readers get wrong — is separated from the ticket's body copy by NOTHING but
   its --inset fill. e-nofill strips fills, so it became indented body text
   directly under the payoff table and read as a fourth row of it.
   `.notice` survived the same treatment because it carries a 4px left border
   rather than a fill (measured: still there, still gold). So the fix is the
   site's own answer to the same problem, in the concept's own language: an aside
   is marked by a rule in the margin, not by a panel. */
.ticket .never{border-left:2px solid var(--rule)!important; padding-left:11px!important}

/* AND THE FRAME AROUND NOTHING. `.panel` and `.ticket` are equal-height grid
   items, so on the claim page the chart's panel is stretched to the taller
   ticket — content ends at 575px, the box ends at 880px. With a fill that void
   read as a surface; as a hairline frame it reads as an empty box, and the
   corner ticks obligingly mark its corners. This is the one declaration here
   that is LAYOUT rather than skin, and it is only needed because e-nofill turned
   an invisible stretch into a visible one. If touching the layout is not on the
   table, the alternative is to let .panel keep its fill. */
.stakewrap>.panel{align-self:start}

/* ---------------------------------------------------------------- e-ticks
   Corner ticks ON a hairline frame, and this was built the other way first.
   e-ticks says "instead of borders" and e-nofill draws one anyway; the later
   rule won in the switchboard, so nobody was ever shown the choice. I resolved
   it toward the stated intent — ticks only, no border — and the render refuted
   it: the claim page's left panel is 958px tall because it is a grid item
   stretched to the ticket's height, so four 10px marks ended up ~960px apart
   with a 500px void between the chart and the bottom pair. They did not read as
   a frame; they read as stray marks outlining nothing.
   Two ways out. Shrink the panel to its content (align-self:start) — but that
   is a change to the site's LAYOUT, not to its skin, and this is a skin.
   Or put the hairline back and let the tick be what a registration mark
   actually is: a reinforcement at the corner of a real frame. The second, also
   because it is what the switchboard was showing when these ten were picked, so
   it is what was approved. Third time the accident has beaten the design. */
.panel, .ticket{position:relative; border:1px solid var(--rule)!important}
.panel::before, .ticket::before, .panel::after, .ticket::after{
  content:""; position:absolute; width:10px; height:10px; pointer-events:none;
  border:1px solid var(--amber); opacity:.55}
.panel::before, .ticket::before{top:-1px; left:-1px; border-right:0; border-bottom:0}
.panel::after,  .ticket::after{bottom:-1px; right:-1px; border-left:0; border-top:0}
/* ONE FRAME GETS THE MARKS. The court docket nests a buy ticket inside the
   "join this court" panel, and both took ticks — two amber corners 170px apart,
   which reads as clutter rather than as registration. A registration mark marks
   the sheet, not every box drawn on it. */
.panel .panel::before, .panel .panel::after,
.panel .ticket::before, .panel .ticket::after,
.ticket .ticket::before, .ticket .ticket::after,
.ticket .panel::before, .ticket .panel::after{display:none}

/* ---------------------------------------------------------------- e-brackets
   The reticle arrives on hover, in a third hue, and only on the two containers
   that hold numbers. An interface that is always reticulating is a toy. */
.panel:hover::before, .ticket:hover::before,
.panel:hover::after,  .ticket:hover::after{
  width:14px; height:14px; border-color:var(--retic); opacity:.85}
.panel:hover::before, .ticket:hover::before{top:-2px; left:-2px}
.panel:hover::after,  .ticket:hover::after{bottom:-2px; right:-2px}

/* ---------------------------------------------------------------- e-ruled */
.grid.stats{gap:0!important; border:1px solid var(--rule)}
.grid.stats>div{border-left:1px solid var(--rule)!important; border-radius:0!important}
.grid.stats>div:first-child{border-left:0!important}

/* ------------------------------------------------------- e-cyan + e-ring
   Two hues with two jobs: cyan is the chain's own record, amber annotates it.
   The endpoint is a RING, not a dot — a hollow mark reads as a measurement
   taken and a filled one reads as a decoration. */
.bigchart .ln{stroke:var(--cyan)!important;
  filter:var(--trace-glow)!important}
.bigchart .tickL{fill:var(--amber); opacity:.7}
.bigchart .end{fill:none!important; stroke:var(--amber)!important;
  stroke-width:1.75px!important; r:5px!important; filter:var(--ring-glow)}

/* ------------------------------------------------- THE SHARE CARD's graph
   Asked for by name: the viral card's graph should read as the same instrument
   as the claim page's, and it did not. Four differences, three of them the
   card's own fault and one of them a fidelity bug:

   1. THE TRACE WAS GREEN. .eline is var(--good) while the claim page's .ln is
      the accent — so the most-distributed surface on the site plotted the same
      series in a different colour from the page it links to. Cyan, with the
      claim page's own two-layer glow.
   2. The endpoint was a filled green disc. Amber ring, like the big chart.
   3. The fill was green at .10. Cyan at .10.
   4. THE GEOMETRY WAS WRONG, not just differently coloured. The svg carries
      preserveAspectRatio="none" and the CSS pinned height:56px, so a 300x56
      viewBox was stretched to the card's width: every slope in the trace was
      flattened by ~13% and the endpoint "circle" was drawn as an ellipse.
      Giving the box the viewBox's own aspect-ratio makes the scaling uniform
      again, so the shape the reader shares is the shape the chain recorded.
      Costs 8px of card height and is worth it — this is a chart, and a chart
      whose slopes depend on the width of the iframe is not a chart.

   Deliberately NOT copied over: the 0/50/100 tick labels and the event
   annotations. They are in-SVG text, and in a box with non-uniform scaling
   text distorts; more to the point the card is 340px wide and the claim page's
   chart is 640px. The dashed rules at 25/50/75 carry the scale without them.
   Same instrument, fewer readings — which is the honest version of "the same". */
.emb .espark{aspect-ratio:300/56; height:auto}
.emb .espark .eline{stroke:var(--cyan)!important;
  filter:var(--trace-glow)}
.emb .espark .efill{fill:var(--cyan)!important; opacity:.10}
.emb .espark .edot{fill:none!important; stroke:var(--amber)!important;
  stroke-width:1.75px!important; r:4.5px!important; filter:var(--ring-glow)}
.emb .espark .e25, .emb .espark .e50{stroke:var(--rule-2)!important}
/* The card is a surface like any other: no fill, held by ticks, gold head. */
.emb{background:transparent!important; border:1px solid var(--rule)!important;
  border-radius:0!important; position:relative}
.emb::before, .emb::after{content:""; position:absolute; width:10px; height:10px;
  pointer-events:none; border:1px solid var(--amber); opacity:.55}
.emb::before{top:0; left:0; border-right:0; border-bottom:0}
.emb::after{bottom:0; right:0; border-left:0; border-top:0}
.emb .ehead{color:var(--stamp)!important; opacity:.9; letter-spacing:.1em}

/* ---------------------------------------------------------------- e-casenum
   The breadcrumb as a docket reference. It does NOT recolour the crumb
   ANCHORS — .crumbs a sets its own colour, so the links stay link-grey and only
   the separators and the page you are on go gold. Gold = where you are, grey =
   where you can go. That was an accident of specificity and it is better than
   what was designed, so it is kept on purpose. */
.crumbs{font-family:var(--mono)!important; letter-spacing:.1em;
  text-transform:uppercase; font-size:10.5px!important; color:var(--stamp)!important; opacity:.85}

/* ---------------------------------------------------------------- e-seal
   Exists only on the court docket. Inert on the other three views. */
.seal{border:1px solid var(--stamp-2)!important; border-radius:0!important;
  letter-spacing:.16em!important; color:var(--stamp)!important; padding:2px 6px!important}

/* ---------------------------------------------------------------- e-grid
   The background grid comes from the live stylesheet's own --gridline. It is
   only here to be sure nothing suppresses it: on the live site this exact rule
   was a silent no-op for a while because a later `background:` shorthand in the
   same block reset background-image. Declared after everything, so it cannot be. */
body{background-image:
  linear-gradient(to right, var(--gridline) 1px, transparent 1px),
  linear-gradient(to bottom, var(--gridline) 1px, transparent 1px)!important;
  background-size:64px 64px!important; background-attachment:fixed!important}

/* ---------------------------------------------------------------- e-ruling
   LAST, and on top of everything: 1px of black in every 3, multiplied. This is
   the element that taxes every contrast ratio on the page, so the render is
   measured rather than admired. */
body::after{content:""; position:fixed; inset:0; pointer-events:none; z-index:9998;
  background:repeating-linear-gradient(0deg, var(--scan) 0 1px, transparent 1px 3px);
  mix-blend-mode:multiply}
"""

for view, slug in (("claim", "c10-selected"),
                   ("docket", "c10-selected-docket"),
                   ("directory", "c10-selected-directory"),
                   ("embed", "c10-selected-embed")):
    build(slug, "The selection", BLURB, CSS, view=view)

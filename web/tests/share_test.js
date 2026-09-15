// The share panel and the embed routes — Kourt's answer to Polymarket's
// embeddable market and its share card.
//
// WHY THESE ASSERTIONS AND NOT OTHERS. Polymarket gets both halves from
// servers this page does not have: a separate embed host, and a per-market
// og:image minted on request. The equivalents here are a ROUTE in the same
// file and a canvas drawn in the reader's own browser. That substitution has
// exactly three ways to go wrong, and each one is a test below:
//
//   1. A RELATIVE embed src. The snippet is pasted onto someone else's page,
//      where a relative URL resolves against THEIR host and the iframe shows
//      their 404. It must be absolute, and derived from where this page is.
//   2. A DIRTY base. Copy the snippet while sitting on `?theme=dark#/c/x/1`
//      and a naive base carries both, so every embed anyone pastes is pinned
//      to the sharer's theme and route.
//   3. A THEME parameter trusted into the DOM. `?theme=` is attacker-supplied
//      the moment a snippet is pasted anywhere, and it reaches
//      setAttribute("data-theme", …).
const fs = require('fs');
const SRC = require('path').join(__dirname, '..', 'index.html');
const src = fs.readFileSync(SRC, 'utf8');
const { slice, fn } = require("./srcslice");

// A canvas that records instead of painting, so the clip's geometry is
// checkable without a browser. measureText is proportional to the text so the
// title wrap is exercised rather than short-circuited.
let RECT = [], TEXT = [], STROKES = [], DOTS = [], FILLS = [];
function fakeCanvas(){
  return {width:0, height:0, getContext: ()=>({
    set fillStyle(v){ this._f = v; }, get fillStyle(){ return this._f; },
    set font(v){ this._n = v; }, get font(){ return this._n; },
    set strokeStyle(v){ this._s = v; }, get strokeStyle(){ return this._s; },
    set lineWidth(v){ this._w = v; }, get lineWidth(){ return this._w; },
    set lineJoin(v){ this._j = v; }, get lineJoin(){ return this._j; },
    fillRect:(x,y,w,h)=>RECT.push({x,y,w,h}),
    fillText(t,x,y){ TEXT.push({t,x,y,f:this._n}); },
    measureText:t=>({width: t.length * 22}),
    // the chart's stroke path — recorded, not drawn
    setLineDash(d){ this._d = d; }, beginPath(){ this._p = []; },
    moveTo(x,y){ (this._p = this._p || []).push([x,y]); },
    lineTo(x,y){ (this._p = this._p || []).push([x,y]); },
    closePath(){}, arc(x,y,r){ DOTS.push({x,y,r}); },
    stroke(){ STROKES.push({pts:(this._p||[]).slice(), dash:this._d, w:this._w, s:this._s}); },
    fill(){ FILLS.push({pts:(this._p||[]).slice(), s:this._f}); },
  })};
}
global.document = { createElement: t => t === "canvas" ? fakeCanvas() : {} };
global.location = { href: "https://kourt.example/app/index.html?theme=dark#/c/bedford/1" };
global.QP = {};

let code = '';
code += slice('function fmtN(', 'function ugnot(');
code += slice('function ccSym(', 'function ugnot(');
code += slice('function esc(', 'function unesc(');
code += 'function safeInline(s){ return esc(s); }\n';
code += slice('const MON=', 'function sinceWords(');
code += slice('function heightDater(', 'async function claimTimeline(');
code += 'const BLOCK_SECS = 5;\n';
code += slice('function shareDialog(', '/* ================================ embeds');
code += slice('function embedTheme(', 'async function embedClaimView(');
// clipText is a shared helper the lifted region calls — it lives outside the
// slice, so it has to be brought in or the region throws ReferenceError.
// Its own rule is asserted in cliptext_test.js; this is only the definition.
eval(fn("clipText"));
eval(code);

let fail = 0; const ok = (n,c)=>{ if(!c){ fail++; console.log("FAIL:", n); } else console.log("ok:", n); };

// --- 1. the snippet is portable -------------------------------------------
const snip = embedSnippet("bedford", 1, {});
ok("embed src is absolute", /src="https:\/\/kourt\.example\/app\/index\.html#/.test(snip));
ok("embed src carries the route", snip.includes('#/embed/bedford/1"'));
ok("no theme is pinned by default", !/theme=/.test(snip));
ok("iframe declares a title for screen readers", /title="Kourt — bedford #1"/.test(snip));
// NOT Polymarket's 400x400 — that is square because their card holds a chart.
// Ours holds a sentence and a bar, and the height is what measuring every card
// in the sample at 320px wide produced. See tests/browser/embed_layout.js.
ok("a claim card is sized to the card, 400x500", /width="400" height="500"/.test(snip));
ok("iframe cannot outgrow its column", /max-width:100%/.test(snip));

const court = embedSnippet("bedford", null, {});
ok("a court card is shorter than a claim card", /width="400" height="210"/.test(court));
ok("court embed omits the id from the route", court.includes('#/embed/bedford"'));

const themed = embedSnippet("bedford", 1, {theme:"dark"});
ok("an explicit theme reaches the src", themed.includes("#/embed/bedford/1?theme=dark"));

// --- 2. the base is clean -------------------------------------------------
// The page is being viewed at ?theme=dark#/c/bedford/1 (see location above).
ok("base drops the sharer's hash", !/#\/c\/bedford/.test(shareURLBase()));
ok("base drops the sharer's query", !/theme=dark/.test(shareURLBase()));
ok("base is the page itself", shareURLBase() === "https://kourt.example/app/index.html");

// --- 3. the theme parameter is not trusted --------------------------------
for(const bad of ['dark" onload="x', "javascript:x", "DARKX", "", "  dark"]){
  QP.theme = bad;
  ok("rejects theme " + JSON.stringify(bad), embedTheme() === null);
}
QP.theme = "DARK"; ok("accepts DARK case-insensitively", embedTheme() === "dark");
QP.theme = "light"; ok("accepts light", embedTheme() === "light");
QP = {};

// --- the dialog -----------------------------------------------------------
const dlg = shareDialog("bedford", 1, {title:"The county certified 12,412 mail ballots."}, "Bedford Truth Court");
// A CLAIM'S LINK IS THE /s/ PAGE, not the app route. The app route is a
// fragment, so a crawler fetching it reads the HOME page's tags and the post
// previews as the site rather than the claim — which is what "it doesn't show
// the graph, just the default kourt hero" was. /s/ carries the claim's own tags
// and redirects a human into the app, so what a reader sees is unchanged.
ok("a claim's link is the card-bearing /s/ page",
   dlg.includes("https://kourt.example/app/s/bedford/1"));
ok("...and not the fragment route, which previews as the site",
   !dlg.includes("https://kourt.example/app/index.html#/c/bedford/1"));
// A COURT KEEPS THE APP ROUTE: there is no /s/ page for one, and a link to the
// site that previews as the site is correct.
ok("a court's link is still the app route",
   shareDialog("bedford", null, {}, "Bedford Truth Court")
     .includes("https://kourt.example/app/index.html#/c/bedford"));
ok("dialog offers the snippet", dlg.includes('id="emb-snip"'));
ok("dialog offers all three themes",
   ['data-embtheme=""','data-embtheme="light"','data-embtheme="dark"'].every(a=>dlg.includes(a)));
ok("dialog offers both clip actions",
   dlg.includes('data-clip="download"') && dlg.includes('data-clip="copy"'));
ok("copy targets are marked", (dlg.match(/data-copytext/g)||[]).length >= 2);
ok("clip status is announced", dlg.includes('id="clip-say"') && dlg.includes('aria-live="polite"'));
// The snippet is HTML shown as text. Unescaped, the <iframe> would be parsed
// by the very page rendering the dialog.
ok("the snippet is escaped, not live markup", dlg.includes("&lt;iframe") && !/<iframe/.test(dlg));
ok("dialog says why the theme buttons exist", /an iframe cannot see it/.test(dlg));
// THE REASON CHANGED, SO THE SENTENCE DID. Two earlier wordings are recorded
// here because each was true when written and stopped being true later:
//
//   "this page has no server to generate one"  — self-contradictory; the page
//                                                plainly generates one
//   "Your browser draws it, so you attach it   — true until /s/ existed, and
//    yourself … this page has no server"         then false: a server DOES hand
//                                                a card to a crawler now
//
// What is true today: the post's picture comes from the link's own card, served
// by /s/<court>/<id>, and the PNG here is for anywhere that wants a file. Pin
// THAT, because a dialog explaining a limitation the site no longer has is how
// a reader concludes the share button is broken.
ok("dialog says where a post's picture actually comes from",
   /the link's own card/.test(dlg) && /\/s\//.test(dlg));
ok("dialog no longer claims the page has no server", !/no server/.test(dlg));
// THE PREVIOUS ASSERTION IS GONE RATHER THAN REWRITTEN: it pinned the sentence
// that said an automatic preview "would have to be built by a server", and one
// now is. Keeping it would have required the dialog to go on claiming a
// limitation that was lifted. What replaces it is the claim above — that the
// dialog names /s/ as where a post's picture comes from.
ok("dialog no longer claims it cannot generate one", !/no server to generate one/.test(dlg));
// And you can SEE it before you send it: it used to download unseen.
ok("the dialog shows a preview", dlg.includes('id="clip-prev"'));
ok("the preview is painted when the panel opens",
   src.includes(`ev.target.closest('[data-help="share-dlg"]')`) && src.includes("paintClipPreview()"));
ok("the preview and the buttons share one build path",
   src.includes("async function buildClip()")
   && (src.match(/await buildClip\(\)/g) || []).length === 2);
// §7.4, and the reason this list is longer than it was. The first version of
// this sweep checked bet/wager/cash-out and MISSED the word actually in the
// copy: "Live odds, updating in place" — imported from Polymarket along with
// the feature, onto the one surface built to travel to other websites. A sweep
// written after the copy, from the copy, cannot fail. This list now comes from
// web/README.md's own ban list plus the prediction-market vocabulary that ban
// list exists to keep out, and REGULATIONS.md is why: the whole posture is that
// this is not a wager.
const BANNED = /\bodds\b|\bbet\b|\bbets\b|betting|\bwager|gambl|\bpayout|\bmoney\b|backing\b|\bredeem\b|cash out|\bprofit|\bAPR\b|\bmarket\b/i;
ok("§7.4 clean — the dialog", !BANNED.test(dlg), (dlg.match(BANNED) || [""])[0]);
// The sweep is worthless if it only covers the string it was written against,
// so run it over the whole share + embed region of the source, comments and all
// — a comment is where the next person picks up the vocabulary.
const REGION = slice('/* ================================= share ===', 'on(/^\\/about$/');
const CODEONLY = REGION.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
ok("§7.4 clean — all share/embed code", !BANNED.test(CODEONLY), (CODEONLY.match(BANNED) || [""])[0]);
// The sweep must be able to FAIL. A regex that matches nothing would pass every
// assertion above and prove nothing at all.
ok("the sweep is armed", BANNED.test("live odds, updating in place")
   && BANNED.test("cash out") && !BANNED.test("the stake split stays current"));

// --- the clip -------------------------------------------------------------
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"The county certified 12,412 mail ballots on Nov 6, 2025.",
                     yesStake: 300, noStake: 100, statusText: "open — stake YES or NO"},
         "Bedford Truth Court", "light");
const joined = TEXT.map(t=>t.t).join(" | ");
ok("clip is 1200x630, the unfurl crop", (()=>{ const c = fakeCanvas(); return true; })()
   && /const W = 1200, H = 630/.test(src));
ok("clip names the court coin", joined.includes("KOURT:BEDFORD"));
ok("clip names the court and the id", joined.includes("Bedford Truth Court") && joined.includes("#1"));
// Wrapping is the one place a word can silently vanish: the loop breaks at
// y>330 and the tail is only drawn if it still fits. Assert the lines REJOIN
// to the original title, not merely that there is more than one of them.
const TITLE = "The county certified 12,412 mail ballots on Nov 6, 2025.";
// ui-serif, not serif: "ui-sans-serif" contains "serif" and swept up
// the caption lines with the title.
const TITLEF = /ui-serif/;
const tlines = TEXT.filter(t=>TITLEF.test(t.f||"")).map(t=>t.t);
ok("clip wraps the title over more than one line", tlines.length >= 2);
ok("wrapping drops no words", tlines.join(" ") === TITLE);
ok("clip states the split", joined.includes("YES 75.0%") && joined.includes("NO 25.0%"));
ok("clip carries the status", joined.includes("open — stake YES or NO"));
// The bar is the one piece of the card that asserts a quantity by its shape.
const bars = RECT.filter(r=>r.y === 430);
ok("the bar is two segments", bars.length === 2);
ok("the segments are proportional to stake", Math.abs(bars[0].w / (bars[0].w + bars[1].w) - 0.75) < 0.001);
ok("the segments span the same width as the text", Math.abs(bars[0].w + bars[1].w - (1200-112)) < 0.5);

// A title too long for the card must be MARKED as cut. Silent truncation of a
// verbatim claim is the one failure here that changes what the claim says:
// drop a trailing qualifier and a hedged statement reads as a flat assertion.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
const LONG = ("The county certified twelve thousand four hundred and twelve mail ballots on the "
  + "sixth of November two thousand twenty five, according to a preliminary count that the "
  + "clerk has not yet reconciled against the poll books.");
drawClip("bedford", 9, {title: LONG, yesStake:1, noStake:1, statusText:"open"}, "Bedford Truth Court", "light");
const long_lines = TEXT.filter(t=>TITLEF.test(t.f||"")).map(t=>t.t);
ok("a long title fills the space it has", long_lines.length === 3);
ok("a cut title says it was cut", long_lines[2].endsWith("…"));
ok("the cut line still fits the card", long_lines[2].length * 22 <= 1200 - 112);
ok("the shown part is a real prefix of the title",
   LONG.startsWith(long_lines.join(" ").replace(/…$/, "")));

// The clip's own disclosure and address.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:3, noStake:1, statusText:"open"}, "Bedford Truth Court",
         "light", "sample data — these courts exist on no chain",
         "https://kourt.example/app/index.html");
const clipT = TEXT.map(t=>t.t);
ok("the clip discloses its source", clipT.some(t=>/no chain/.test(t)));
// At the TOP, not the footer: a grey line at the bottom is one crop from gone.
ok("the disclosure rides in a band under the identity line",
   TEXT.some(t=>/no chain/.test(t.t) && t.y > 96 && t.y < 160));
ok("the band is full width, so nothing can collide with it",
   RECT.some(r=>r.y === 96 && r.w === 1200 && r.h === 48));
// It must arrive WHOLE. The first version budgeted it a corner and shaved it
// down to "sample data — these", which discloses nothing at all.
ok("the disclosure is not shaved", TEXT.some(t=>t.t === "sample data — these courts exist on no chain"));
ok("the disclosure starts at the text margin",
   TEXT.some(t=>/no chain/.test(t.t) && t.x === 56));
// The band ends at 144. A title drawn at the no-note baseline would sit 8px off
// it; with a note the title drops clear.
ok("the title clears the disclosure band",
   TEXT.filter(t=>/^T$/.test(t.t) && /ui-serif/.test(t.f||"")).every(t=>t.y >= 196));
ok("the clip carries an address to check it",
   clipT.some(t=>t === "kourt.example/app/index.html"));
ok("the address drops the protocol", !clipT.some(t=>/^https?:\/\//.test(t)));
// The card now has one declared vertical rhythm, so the status sits at a fixed
// baseline whether or not a chart pushed the bar down. The first version keyed
// it off H and left the YES/NO labels sitting ON the bar once the chart moved it.
ok("the status sits on its declared baseline", TEXT.some(t=>t.t === "open" && t.y === 586));
// A LOCAL PATH MUST NEVER REACH THE CANVAS. web/README.md invites running from
// file://, where shareURLBase() is a path inside the sharer's home directory —
// and a PNG, once posted, cannot be recalled. drawClip refuses it even when the
// caller passes it, because the call site is one edit away from forgetting.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:1, noStake:1, statusText:"open"}, "C", "light", null,
         "file:///Users/someone/projects/kourt/web/index.html");
ok("a file:// address is refused by drawClip itself",
   !TEXT.some(t=>/Users|file:|\.html/.test(t.t)), JSON.stringify(TEXT.map(t=>t.t)));
ok("the status baseline does not move with the address",
   TEXT.some(t=>t.t === "open" && t.y === 586));
ok("the call site refuses it too", src.includes("shareIsPublic() ? shareURLBase()"));
ok("and says so, instead of handing over a dead local link",
   src.includes("only work on this machine"));
// Nothing to disclose on an honest live chain: no note drawn, status stays put.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:3, noStake:1, statusText:"open"}, "C", "light", null, null);
ok("an honest chain draws no note", !TEXT.some(t=>/chain|sample/.test(t.t)));
ok("and no band is painted for it", !RECT.some(r=>r.y === 96));
ok("and no address line", TEXT.filter(t=>t.y > 630-60).length === 1);

// A single long token on the last line used to empty on the word-shave pass,
// fall back to the un-shortened line, and overflow with the ellipsis attached.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title: "one two three four five six seven eight nine ten eleven twelve "
  + "thirteen fourteen fifteen sixteen " + "z".repeat(120), yesStake:1, noStake:1, statusText:"open"},
  "C", "light", null, null);
const tl2 = TEXT.filter(t=>TITLEF.test(t.f||""));
const lastLine = tl2.pop();
// This case is NOT the cut path — the title is exactly three lines, so `cut` is
// false and nothing in the truncation branch ever runs. The token still has to
// be shortened, which is the bug: it was drawn at full 120-character width.
ok("a token too wide to wrap is shaved", lastLine.t.endsWith("…"));
ok("and every drawn line fits the card",
   tl2.concat([lastLine]).every(t=>t.t.length * 22 <= 1200 - 112),
   `widest=${Math.max(...tl2.concat([lastLine]).map(t=>t.t.length))}`);

// THE CHART'S DETAIL. It was a line, a dashed 50% and a dot — a shape with no
// WHEN and no scale, so a reader could not tell a week from a season nor read
// how far from level the stake sat.
// SELF-CONSISTENT, as a real timeline is: every t/h pair here sits on the same
// 5s-per-block line through now. The first version of this fixture did not, and
// the failure it produced looked like a charting bug rather than a bad fixture.
//   now      4,800,000 @ 1787054400
//   answered 4,790,000 = now - 10,000 blocks = now -    50,000s
//   opened   4,700,000 = now - 100,000 blocks = now -   500,000s
const TL = {opened:{t:1786554400,h:4700000}, answered:{t:1787004400,h:4790000},
            now:{t:1787054400,h:4800000}};
const SER = {pts:[[4700000,60,6,4],[4740000,72,9,4],[4780000,77,10,3]], firstH:4700000};
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:10, noStake:3, statusText:"answered"}, "Bedford Truth Court",
         "light", null, null, SER, TL);
const chartT = TEXT.map(t=>t.t);
ok("the chart is dated at both ends",
   chartT.includes(stampDate(TL.now.t)) && chartT.some(t=>/2026|2025/.test(t)),
   JSON.stringify(chartT.filter(t=>/\d{4}$/.test(t))));
// The domain runs to NOW, not to the last change: change-only samples mean the
// last row can be days old while the value still holds.
ok("the right edge is dated NOW, not the last change",
   chartT.includes(stampDate(TL.now.t)), JSON.stringify(chartT));
ok("the left edge is the first recorded sample",
   chartT.includes(stampDate(1786554400)), JSON.stringify(chartT));
ok("there is a readable scale, not just a midline",
   ["25%","50%","75%"].every(g=>chartT.includes(g)));
// Staking freezes when an answer posts, so a flat tail after that height is the
// realm refusing writes — not a market gone quiet. Unmarked they look identical.
// WHAT HAPPENED TO THE CLAIM, dated — the lane the claim page gives a whole
// section to. Free: ClaimTimeline is already read for the chart's dates.
ok("the clip carries the claim's events",
   chartT.some(t=>/^opened \d+ \w+ {3}·/.test(t)), JSON.stringify(chartT.filter(t=>/opened/.test(t))));
ok("past events and the pending deadline are both named",
   chartT.some(t=>/opened /.test(t) && /answered /.test(t)), JSON.stringify(chartT.filter(t=>/opened/.test(t))));
ok("the trail sits above the status", TEXT.some(t=>/^opened /.test(t.t) && t.y < 586));
ok("the chart marks where the record froze",
   chartT.some(t=>/answered — staking frozen/.test(t)));
ok("and the marker sits on the floor, clear of the line",
   TEXT.some(t=>/staking frozen/.test(t.t) && t.y > 380), 
   JSON.stringify(TEXT.filter(t=>/staking frozen/.test(t.t)).map(t=>t.y)));
// No anchor: say what the axis IS rather than dating it wrongly.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:10, noStake:3, statusText:"open"}, "C",
         "light", null, null, SER, null);
ok("with no timeline it labels blocks rather than inventing dates",
   TEXT.some(t=>/^block /.test(t.t)) && !TEXT.some(t=>/20\d\d$/.test(t.t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/block|20\d\d/.test(t))));
// A claim with no series must say so rather than leaving a hole where a chart
// obviously belongs — and say it in words a reader has been given. "Recorded
// path" was this file's internal name for the series and appeared on no page;
// the card is the surface that travels furthest from here, pasted where none of
// this page's context comes with it, so it is the last place for an in-house
// noun. Reported as "i have no idea what 'recorded path' means".
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:10, noStake:3, statusText:"open"}, "C", "light");
ok("no series says so, in plain words",
   TEXT.some(t=>/no movement recorded yet/.test(t.t)) && !TEXT.some(t=>/recorded path/.test(t.t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/movement|path|stak/.test(t))));
// ONE RECORDED POINT IS STILL A RECORD, and the card and the page have to agree
// about that. The page draws on ser.pts.length — one or more — while the card
// asked for two, so a claim whose whole history is a single stake got a chart on
// kourt.xyz and "no movement recorded yet" on the card shared from it: two
// accounts of one claim, from one series. Reported against covid/28, whose
// hourly and daily rows merge to exactly one point.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("covid", 28, {title:"T", yesStake:5000000, noStake:0, statusText:"open"}, "C", "light",
         null, null, {pts:[[50400, 100, 5000000, 0]], firstH:50400}, {now:{h:51120}});
ok("a single recorded point still draws the chart",
   TEXT.some(t=>/share of stake on YES/.test(t.t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/movement|stake on YES/.test(t))));
ok("...and does not also claim nothing moved",
   !TEXT.some(t=>/no movement recorded yet/.test(t.t)));

// THE LABELS CLEAR THE BAR. A baseline is not a top edge: at 30px the ascent is
// about 22, so the old `BAR_Y + 46` put the glyph tops two pixels inside a bar
// that ends at BAR_Y + 26. Reported as the labels touching it.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:10, noStake:3, statusText:"open"}, "C", "light");
{
  const bar = RECT.filter(r => r.h === 26 && r.x === 56)[0];
  const lab = TEXT.filter(t => /^YES /.test(t.t))[0];
  ok("the bar and its label were both drawn", !!bar && !!lab);
  if (bar && lab) {
    // The label's top edge, not its baseline — a 30px face carries ~22 of ascent.
    const top = lab.y - 22, barBottom = bar.y + bar.h;
    ok(`the label clears the bar by ${Math.round(top - barBottom)}px`, top - barBottom >= 6,
       `bar ends ${barBottom}, label top ${top}`);
  }
}

// AND THE TWO EMPTY CASES ARE DIFFERENT FACTS. Stake that has not moved is not
// the same as nothing staked, and saying "nothing staked yet" on the first would
// be false — the bar beside it is showing 10 against 3.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:0, noStake:0, statusText:"open"}, "C", "light");
ok("...and an unstaked claim says that instead",
   TEXT.some(t=>/nothing staked yet/.test(t.t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/movement|stak/.test(t))));
/* AND IT DRAWS THE EMPTY CHART, which is the opposite of what this arm required.
   It asserted no 25/50/75 axis at all — correct while the empty card was a line
   of prose, and the reported defect once it was: "without a graph and just text
   'nothing staked...' it looks weird". A sentence floating in the space a chart
   occupies reads as something that failed to load; the scale is what says it is
   an empty chart instead. */
ok("...on an axis, so it reads as an empty chart rather than a failure",
   ["25%","50%","75%"].every(t => TEXT.some(x => x.t === t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/%$/.test(t))));
/* AND ONLY WHEN THE CARD WOULD OTHERWISE BE BLANK. A claim with stake but no
   movement already has the bar and its YES/NO labels in that space, so a frame
   adds nothing and lands on top of them — MEASURED: the 25% row ran through the
   bar and the caption sat under the labels. The first drawClip above is that
   case, and TEXT from it is still in scope here only because this arm re-runs it. */
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 1, {title:"T", yesStake:10, noStake:3, statusText:"open"}, "C", "light");
ok("...and not over a bar that is already showing the split",
   !TEXT.some(t=>/^(25|50|75)%$/.test(t.t)),
   JSON.stringify(TEXT.map(t=>t.t).filter(t=>/%$/.test(t))));

// A COURT NAME IS AS UNBOUNDED AS A CLAIM TITLE — whoever creates the court
// types it. The first version of drawClip clamped the title and left the name
// alone, and a 68-character name drew the identity line to 1606px on a 1200px
// canvas: 400px off the edge, silently. The name gives way; the coin symbol and
// the claim number are what a reader needs to find the claim again, so they stay.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
const LONGNAME = "Salt Lake County Consolidated Election Canvass Review Board of Record";
drawClip("bedford", 12345, {title:"T", yesStake:1, noStake:1, statusText:"open"}, LONGNAME,
         "light", null, null);
const idline = TEXT.find(t=>/^KOURT:BEDFORD/.test(t.t));
ok("the identity line is drawn", !!idline);
ok("it fits the canvas", idline.t.length * 22 <= 1200 - 112, `len=${idline.t.length}`);
ok("the shortened name is marked", /…/.test(idline.t), JSON.stringify(idline.t));
ok("the claim number survives", /#12345$/.test(idline.t), JSON.stringify(idline.t));
ok("the coin symbol survives", /^KOURT:BEDFORD/.test(idline.t));
ok("nothing at all lands off the canvas",
   TEXT.every(t=>t.x + t.t.length * 22 <= 1200 - 40),
   JSON.stringify(TEXT.map(t=>Math.round(t.x + t.t.length*22)).filter(w=>w>1160)));
// A pathological SLUG can outgrow the line without the name being long at all,
// so the composed string gets a last-resort pass of its own.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("a".repeat(90), 1, {title:"T", yesStake:1, noStake:1, statusText:"open"}, "C", "light", null, null);
const idline2 = TEXT.find(t=>/^KOURT:A/.test(t.t));
ok("a pathological slug is cut too", idline2 && idline2.t.length * 22 <= 1200 - 112,
   idline2 ? `len=${idline2.t.length}` : "no line");

// A claim nobody has staked must still produce a card, not a divide-by-zero
// stripe or a crash.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 2, {title:"Unstaked.", yesStake:0, noStake:0, statusText:"open"}, "Bedford Truth Court", "dark");
ok("an unstaked claim draws no bar", RECT.filter(r=>r.y === 430).length === 0);
ok("an unstaked claim still draws its title", TEXT.some(t=>t.t === "Unstaked."));

// A court card has no claim and no stake at all.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", null, null, "Bedford Truth Court", "light");
ok("a court clip omits the claim id", !TEXT.map(t=>t.t).join(" ").includes("#"));
ok("a court clip falls back to a truthful status",
   TEXT.some(t=>t.t === "a court of claims of fact"));

// A long status must be cut with an ellipsis rather than run off the canvas —
// there is no layout engine here to catch it.
RECT = []; TEXT = []; STROKES = []; DOTS = []; FILLS = [];
drawClip("bedford", 3, {title:"T", yesStake:1, noStake:1, statusText:"x".repeat(200)}, "C", "light");
ok("a long status is truncated", TEXT.some(t=>t.t.length === 78 && t.t.endsWith("…")));

// --- routes and wiring (asserted against the source) -----------------------
const R_CLAIM = /^\/embed\/([a-z0-9-]+)\/(\d+)$/, R_COURT = /^\/embed\/([a-z0-9-]+)$/;
ok("claim embed route registered", src.includes("on(/^\\/embed\\/([a-z0-9-]+)\\/(\\d+)$/"));
ok("court embed route registered", src.includes("on(/^\\/embed\\/([a-z0-9-]+)$/"));
ok("claim route matches", R_CLAIM.test("/embed/bedford/1"));
ok("court route matches", R_COURT.test("/embed/bedford"));
ok("court route does not swallow a claim", !R_COURT.test("/embed/bedford/1"));
ok("embed routes do not shadow the ordinary claim page", !R_CLAIM.test("/c/bedford/1"));

// The embed must actually be chrome-free; the class is what hides the rail.
ok("the embed head cannot wrap", /flex-wrap:nowrap/.test(
   slice('.emb .ehead{', '.emb .etitle{')));
ok("and the court name is the part that gives way",
   src.includes('.emb .ehead .ename{overflow:hidden; text-overflow:ellipsis'));
ok("the claim card tags the name for it", src.includes('"elink ename"'));
// The head's children are no longer all <span> — the coin symbol is a link —
// so a span-only rule would have left it shrinkable next to the name.
ok("everything but the name is held at its natural width",
   src.includes(".emb .ehead > :not(.ename){flex:0 0 auto}"));
ok("embed hides the rail and the nav",
   /html\.embed \.rail, html\.embed #nav\{display:none ?!important\}/.test(src));
// The mark is DRIVEN BY THE ROUTE, not switched on once. embedOpen() only ever
// added it and nothing removed it, so an embed URL opened in a top-level tab
// ("Open frame in new tab" is in every iframe's right-click menu) left the whole
// app with no rail and no nav — a dead end, and a
// permanently hidden test-clock disclosure. Same for a sticky ?theme=.
ok("the mark is recomputed from the route",
   src.includes('document.documentElement.classList.toggle("embed", on)')
   && /const on = \/\^\\\/embed\\\//.test(src));
ok("the router sets it before the route paints",
   /setNav\(hash\);\s*\n\s*embedMark\(hash\);/.test(src));
ok("leaving an embed restores the reader's own theme, not nothing",
   src.includes("applyTheme(CFG.theme)"));
// AN EMBED'S LINKS OPEN A NEW TAB, and this replaced target="_top". _top would
// have replaced the article the reader was in — they clicked a card inside
// somebody's piece and the piece went away. Checked against a live
// embed.polymarket.com card: all six of its links back are _blank.
ok("no link takes the reader's page away from them", !/target="_top"/.test(src));
ok("embed links open a new tab", (src.match(/target="_blank"/g)||[]).length >= 5);
ok("and never hand the opener over", !/target="_blank"(?![^>]*rel="noopener")/.test(
   slice('function embedLink(', 'on(/^\\/about$/')));
ok("embed links declare where they came from", src.includes('"?from=embed"'));
// One link back in the footer meant a reader who wanted the claim had to find
// the small print. The court, the title and the footer each lead to the thing
// they name.
ok("there is one helper for every link that leaves the card",
   src.includes("function embedLink(path, inner, cls)"));
ok("the claim card links from its court, its title and its footer",
   (slice('async function embedClaimView(', 'async function embedCourtView(')
     .match(/embedLink\(|target="_blank"/g) || []).length >= 4);
ok("the links inherit rather than shouting", src.includes(".emb .elink{color:inherit"));

// The three handlers the buttons declare must exist, or the panel is decoration.
ok("theme buttons rebuild the snippet", src.includes('ev.target.closest("[data-embtheme]")')
   && src.includes('snip.textContent = embedSnippet('));
ok("copy blocks are wired", src.includes('ev.target.closest("pre[data-copytext]")')
   && src.includes("navigator.clipboard.writeText"));
// The Clipboard API needs a secure context; plain http:// on a real hostname
// leaves navigator.clipboard UNDEFINED, and the first version of this handler
// then did nothing at all — no copy, no message, cursor:copy still promising.
// The rest of this file selects the text and says so; now this does too.
ok("copy degrades instead of failing silently",
   src.includes("if(!navigator.clipboard){ select(); return; }")
   && src.includes("Selected — press Ctrl/⌘-C"));
ok("copy reports success too", src.includes('say("Copied ✓")'));
// A <pre> takes no focus, so click-to-copy alone is mouse-only.
ok("there is a keyboard path to each copy block",
   (dlg.match(/data-copyfor="/g) || []).length === 2
   && src.includes('ev.target.closest("[data-copyfor]")'));
ok("every copy block has a button pointed at it",
   (dlg.match(/<pre [^>]*data-copytext[^>]*id="([a-z-]+)"/g) || []).length
   + (dlg.match(/<pre [^>]*id="([a-z-]+)"[^>]*data-copytext/g) || []).length === 2);
// LOW: three identical buttons with no pressed state left the reader guessing.
ok("the theme buttons carry a pressed state",
   (dlg.match(/aria-pressed/g) || []).length === 3
   && dlg.includes('data-embtheme="" aria-pressed="true"')
   && src.includes('b.setAttribute("aria-pressed", String(b === th))'));
ok("download is wired", src.includes('data-clip") === "download"') && src.includes('a.download = name'));
ok("copy-image is wired", src.includes("new ClipboardItem({\"image/png\": blob})"));
// Both image paths can fail for reasons the user can act on; silence would
// read as a broken button.
ok("a browser without ClipboardItem is told so", src.includes("this browser cannot copy images"));
ok("a refused clipboard permission is reported", src.includes("the browser refused the clipboard"));
ok("the share trigger is on the claim page", src.includes('data-help="share-dlg"'));

// --- the action, in this realm's words -------------------------------------
// Polymarket's card ends in two priced outcome buttons. A claim here is not
// bought, not sold and not priced — principal always returns 1x — so the number
// under each side is the SHARE OF STAKE on it, and the verb is the one the
// realm actually has.
ok("the card offers the two sides", src.includes("function embedActions(d, slug, id)"));
ok("with this realm's verb", src.includes('one("yes", "Stake YES"') && src.includes('one("no", "Stake NO"'));
ok("only while the claim is open", src.includes('if(!d || d.phase !== "open") return "";'));
ok("carrying the side, like tid=0/tid=1", src.includes('&side=${side}'));
const ACT = slice('function embedActions(', 'function embedSpark(');
ok("§7.4 clean — the action", !BANNED.test(ACT), (ACT.match(BANNED) || [""])[0]);
ok("no price, no cents, no buying", !/\bbuy\b|\bsell\b|¢|price/i.test(ACT));
// The side must land somewhere or the button is a fake affordance.
ok("the claim page honours ?side=", src.includes("function focusStakeSide(side)")
   && src.includes("if(QP.side) focusStakeSide(QP.side);"));
ok("landing marks, it does not stake", src.includes('b.classList.add("picked")')
   && !/focusStakeSide[\s\S]{0,400}(submit|broadcast|signTx)/.test(src));
ok("and a frozen claim has nothing to point at", src.includes(
   'if(!t) return;   // the claim moved on'));

// --- the disclosure that has to travel with the card ----------------------
// A PNG leaves with no banner, no rail and no URL bar; an embed hides the rail
// that carries "Demo data". Demo is the default for any first-time reader, so
// without these both surfaces present the offline sample as a chain record.
ok("there is one source note for both surfaces", src.includes("async function sourceNote()"));
ok("demo mode is disclosed", src.includes("sample data — these courts exist on no chain"));
// The test-clock disclosure is gone from every surface — the page renders as
// production whatever the node's clock has been told to do. What remains is the
// one about invented CONTENT, which is a different claim: a card carrying sample
// courts to somebody else's website still has to say they exist on no chain.
ok("a live chain discloses nothing at all",
   /if\(!isLive\(\)\) return "sample data[^"]*";\s*\n\s*return null;/.test(src));
ok("no test-clock disclosure survives anywhere",
   !src.includes("test chain") && !src.includes("tcbanner") && !src.includes("tcbar"));
ok("all four embed paths carry it", (src.match(/\$\{esrc\(note\)\}/g) || []).length === 4);
ok("the clip is handed the note, the address, the series and the timeline",
   src.includes('drawClip(slug, id, d, cs ? cs.name : slug, isDark ? "dark" : "light", note, url, ser, tl)'));

// --- link previews, and the promise not to fake one ------------------------
/* THE TAGLINE MUST AGREE WITH ITSELF, IN FOUR PLACES.
   MEASURED DRIFT, which is why this exists: the link-preview card in
   scripts/make-og-card.js drew "Let Truth be told." while <title>, og:title and
   twitter:title all still said "the ledger of record". Anyone who pasted a link
   saw one tagline in the picture and a different one in the text beside it, and
   nothing in the suite objected — the arm below only asked whether the tags
   EXISTED.
   THE CARD IS THE SOURCE OF TRUTH here, because it is the artefact a reader
   actually looks at, and it is a generated PNG whose text cannot be checked by
   reading the page. So the card's own words are read out of the generator and
   the three head tags are required to carry them. */
{
  const cardSrc = fs.readFileSync(require("path").join(__dirname, "..", "..",
    "scripts", "make-og-card.js"), "utf8");
  const said = (cardSrc.match(/<p>([^<]+)<\/p>/) || [])[1] || "";
  const tag = said.replace(/\.\s*$/, "").trim();
  ok(`the card states a tagline ("${tag}")`, tag.length > 3, JSON.stringify(said));
  for (const [what, re] of [
    ["<title>", /<title>([^<]+)<\/title>/],
    ["og:title", /property="og:title" content="([^"]+)"/],
    ["twitter:title", /name="twitter:title" content="([^"]+)"/],
  ]) {
    const got = (src.match(re) || [])[1] || "";
    ok(`...and ${what} carries it, not some earlier one — ${JSON.stringify(got)}`,
       got.includes(tag), JSON.stringify({got, tag}));
  }
  /* AND THE ONE IT REPLACED IS GONE FROM THE SHIPPED PAGE. Named explicitly
     rather than checked as "not stale", because a general test for staleness is
     not a thing that can be written. */
  ok("...and the page no longer says the tagline it replaced",
     !/ledger of record/i.test(src), "the old tagline is still in index.html");
}
ok("site-level og tags exist", src.includes('property="og:title"') && src.includes('property="og:description"'));
// A large-image card, because there is now an image worth showing: the site's
// own drawing, which is true of every URL here in a way a claim picture is not.
ok("twitter card declared", src.includes('name="twitter:card" content="summary_large_image"'));
ok("both preview images name an absolute https URL — a crawler follows no data: URI",
   src.includes('property="og:image" content="https://kourt.xyz/og.png"')
   && src.includes('name="twitter:image" content="https://kourt.xyz/og.png"'));
ok("the card declares its size and its alt text",
   src.includes('property="og:image:width" content="1200"')
   && src.includes('property="og:image:height" content="630"')
   && src.includes('property="og:image:alt"') && src.includes('name="twitter:image:alt"'));
{
  // and the file exists, at the size the tags claim. A card that 404s is worse
  // than no card: the platforms cache the miss.
  const p = require("path").join(__dirname, "..", "og.png");
  const b = require("fs").existsSync(p) ? require("fs").readFileSync(p) : null;
  ok("web/og.png exists", !!b && b.length > 2000);
  // PNG IHDR: width and height are big-endian uint32 at bytes 16 and 20.
  ok("web/og.png really is 1200x630",
     !!b && b.readUInt32BE(16) === 1200 && b.readUInt32BE(20) === 630);
  ok("and it is small enough for a link unfurl", !!b && b.length < 300000);
}
// WHAT MAY NOT HAPPEN, now that there IS an image. A per-claim og:image is not
// something a hash-routed static file can mint: the crawler never runs the
// router, so every claim would unfurl as whichever one was baked in. The site's
// own drawing is the one picture true of every URL here. So: exactly one image,
// never interpolated, and shipped by the same script that ships the page — a
// card that 404s is worse than no card, because the platforms cache the miss.
{
  const imgs = src.match(/(?:property="og:image"|name="twitter:image")\s+content="[^"]*"/g) || [];
  ok("exactly one preview image, declared twice (og + twitter)", imgs.length === 2);
  ok("the image is never interpolated per route", !imgs.some(t => t.includes("${")));
  ok("and is a file, not a data: URI", !imgs.some(t => t.includes("data:")));
  const dep = require("fs").readFileSync(
    require("path").join(__dirname, "..", "..", "deploy", "deploy.sh"), "utf8");
  ok("deploy.sh uploads the file the tags name", dep.includes("og.png"));
}
ok("the reason is written down where the tags are", /a per-claim og:title is not something/.test(src));
ok("README records what a server would need", fs.readFileSync(
   require('path').join(__dirname,'..','README.md'), 'utf8').includes("og:image"));

console.log(fail ? "\n" + fail + " FAILURES" : "\nALL PASS");
process.exit(fail ? 1 : 0);

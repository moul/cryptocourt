// The court page's Discord slot: the endpoint gate, the sample, and the sentence
// that has to travel with the link.
//
// THE FAILURE CASES ARE THE POINT, the same way joinheld_test.js's are. A court
// with no server is the ordinary case, and the two ways to get it wrong are both
// silent: painting an empty panel under a heading, and — worse — painting the
// button without the sentence that says what being listed does not mean.
const fs = require('fs');
const src = fs.readFileSync(require('path').join(__dirname, '..', 'index.html'), 'utf8');
const { slice } = require("./srcslice");

global.document = { getElementById: () => null };
global.location = { protocol: 'https:', origin: 'https://kourt.xyz', host: 'kourt.xyz' };
global.CFG = { mode: 'demo', chainid: 'kourt-1' };
global.QP = {};
global.isLive = () => CFG.mode === 'live';

let code = '';
code += slice('function esc(', '\n');
// guildEndpoint reuses siteHost(), and both sit in this run of small
// origin helpers, so the slice takes the run rather than one function.
code += slice('function defaultChatBase(', '\nconst store =');
code += slice('function discordSlotHtml(', '\nasync function fillDiscord(');
eval(code);

let fail = 0;
const ok = (n, c) => { if (!c) { fail++; console.log("FAIL:", n); } else console.log("ok:", n); };

// --- the endpoint gate ---------------------------------------------------
// One function returns "", so no call site has to remember. The alternative was
// tried for chat and produced a 404 in the console on every demo court page.
CFG.mode = 'demo';
ok("demo has no endpoint", guildEndpoint() === "");
CFG.mode = 'live';
ok("live derives the endpoint from the origin", guildEndpoint() === "https://kourt.xyz");
global.location = { protocol: 'file:', origin: 'null', host: '' };
ok("file:// has no endpoint either", guildEndpoint() === "");
global.location = { protocol: 'https:', origin: 'https://kourt.xyz', host: 'kourt.xyz' };

// It must not be a CFG field: LOCKED drops every stored field naming a chain or
// a host, so a settable endpoint would work in development and silently not in
// production.
ok("the endpoint is not read from CFG",
   !/CFG\.guild|CFG\["guild"\]/.test(slice('function guildEndpoint(', '\nconst store =')));

// --- the slot ------------------------------------------------------------
ok("the slot is empty and carries its slug",
   discordSlotHtml("bedford") === '<section id="discord" class="discord" data-slug="bedford"></section>');
ok("the slot escapes its slug", !discordSlotHtml('a"b').includes('data-slug="a"b"'));
ok("the court page emits the slot", src.includes("+ discordSlotHtml(slug)"));
ok("and fills it", src.includes("fillDiscord(slug);"));

// --- nothing to show -----------------------------------------------------
// A court with no server draws NOTHING — not a heading, not an empty panel.
ok("no server renders nothing at all", discordHtml("meta", null) === "");
ok("...not even a heading", !discordHtml("meta", null).includes("<h2"));
ok("...and not for undefined either", discordHtml("meta", undefined) === "");

// --- a real listing ------------------------------------------------------
const live = discordHtml("covid", { invite: "https://discord.gg/abc123", guild_id: "1478455953715236886" });
ok("a listing renders the invite", live.includes('href="https://discord.gg/abc123"'));
// deploy.sh refuses any external href without rel="noopener", so this is a
// ship-time gate as well as a security one.
ok("the external link carries rel=noopener", /rel="noopener"/.test(live));
ok("...and opens in a new tab", /target="_blank"/.test(live));

// THE SENTENCE TRAVELS WITH THE ANCHOR. This is the assertion worth having: a
// future edit that keeps the button and drops the disclosure fails here.
ok("the disclosure says who chose the server", live.includes("current moderators chose this"));
ok("the disclosure says what it does not mean", live.includes("does not mean the court is legitimate"));
ok("the disclosure disclaims the contents", live.includes("anything said"));
ok("the disclosure says who does not stand behind it",
   live.includes("Kourt does not run it"));

// --- the sample ----------------------------------------------------------
// Some courts have a server and most do not; a sample where every court had one
// would teach the wrong shape.
ok("the sample court has one", discordDemoServer("bedford") !== null);
ok("other demo courts do not", discordDemoServer("meta") === null && discordDemoServer("ledger") === null);

const demo = discordHtml("bedford", discordDemoServer("bedford"));
ok("the sample renders", demo.includes("<h2>Discord</h2>"));
ok("the sample says it is a sample", demo.includes("sample data"));
// An inert control, not a live link to nowhere: a href="" would navigate.
ok("the sample button is not a link", !demo.includes("<a class=\"btn\""));
ok("the sample button is marked disabled", demo.includes('aria-disabled="true"'));
ok("the sample carries the disclosure too", demo.includes("current moderators chose this"));

// --- determinism ---------------------------------------------------------
// Two loads of the demo must be identical — chatDemoThread's rule, and the
// reason it uses ages rather than a clock.
ok("the sample is deterministic",
   discordHtml("bedford", discordDemoServer("bedford")) === demo);

// --- no network in demo --------------------------------------------------
const fillSrc = slice('async function fillDiscord(', '\n}');
ok("the fill returns before fetching when there is no endpoint",
   fillSrc.indexOf("if(!base)") < fillSrc.indexOf("fetch("));
ok("the fill has exactly one fetch", (fillSrc.match(/fetch\(/g) || []).length === 1);
ok("the fill's only base is guildEndpoint()", fillSrc.includes("const base = guildEndpoint()")
   && !/location\.origin/.test(fillSrc));

// --- listed, but with no door ---------------------------------------------
// The moderators published it and the chain agreed; the bot could not mint an
// invite. A greyed button with no reason reads as the page being broken rather
// than the server being shut.
const shut = discordHtml("covid", { invite: "", guild_id: "1478455953715236886" });
ok("a listing with no invite still renders", shut.includes("<h2>Discord</h2>"));
ok("...with no link to nowhere", !shut.includes("<a class=\"btn\""));
ok("...and says why", shut.includes("cannot open a door to it"));
ok("...and says whose setting it is", shut.includes("server's setting to change"));
ok("...and keeps the disclosure", shut.includes("current moderators chose this"));
// The sample must NOT carry that sentence: nothing is shut there, it is a sample.
ok("the sample does not claim a permissions problem",
   !discordHtml("bedford", discordDemoServer("bedford")).includes("cannot open a door"));

// --- coming back from Discord -------------------------------------------
// The redirect lands here with ?discord=<outcome>. Until this existed the reader
// arrived on an unchanged page with no acknowledgement of what they had just
// done — a silent dead end at the moment they most needed telling.
ok("an added bot is acknowledged", discordOutcomeHtml("added").includes("not published here yet"));
ok("...and says what is left, without sending them to a repo",
   discordOutcomeHtml("added").includes("sign for it") &&
   !discordOutcomeHtml("added").includes("GUILD.md"));
ok("a taken server explains itself", discordOutcomeHtml("taken").includes("different court"));
ok("an expired link says nothing changed", discordOutcomeHtml("expired").includes("Nothing was changed"));
ok("a reused link says nothing changed", discordOutcomeHtml("already-used").includes("Nothing was changed"));
ok("declining is not a failure", discordOutcomeHtml("cancelled").includes("Nothing was changed"));
// An unknown value is somebody editing the URL, not an outcome to narrate.
ok("an invented outcome says nothing", discordOutcomeHtml("pwned") === "");
ok("no outcome says nothing", discordOutcomeHtml(undefined) === "" && discordOutcomeHtml("") === "");
// It is escaped like anything else that reaches the page from a URL.
ok("the outcome table is fixed text, never the URL's", !discordOutcomeHtml("added").includes("<script"));

// --- a court with no server, live ---------------------------------------
// Saying nothing left the feature undiscoverable: a moderator had no way to learn
// from the court's own page that a server was possible.
// The copy is wrapped for the source, so assertions normalise whitespace rather
// than the copy being reflowed to suit a test.
const flat = h => h.replace(/\s+/g, " ");
const absent = flat(discordAbsentHtml("meta"));
ok("an unlisted court says so", absent.includes("no Discord server"));
ok("...names who may publish one", absent.includes("moderators"));
ok("...and says front-running does not work",
   absent.includes("getting there first"));
ok("...without promising a control this page does not have",
   absent.includes("same key they moderate with") && !absent.includes("from here"));
ok("...and offers no dead-end link", !absent.includes("<a "));

console.log(fail ? "\n" + fail + " FAILURES" : "\nALL PASS");
process.exit(fail ? 1 : 0);

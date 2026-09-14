// The address "Your positions" and "What needs you" read, and whether it
// survives a reload.
//
// WHY IT IS ITS OWN HARNESS. The box on those two pages was write-only: you
// typed forty characters, the page read them once, and a reload — an accidental
// one included — put the empty state back and asked again. The wallet's address
// was remembered and a typed one was not, which is backwards from who needs it:
// a reader with no extension, or one looking at an account the extension does
// not hold, has nothing BUT the box.
//
// WHAT A HARNESS CATCHES THAT A BROWSER CHECK WOULD NOT. Persistence is a
// round trip through JSON and cleanCfg, and every interesting failure is on the
// far side of it: a field cleanCfg does not copy is written and then dropped on
// the next load, and the LOCKED build — the DEPLOYED one — drops everything not
// named in its keep-list. Both are silent, both look like "it just doesn't
// remember", and neither is visible from one page load. So the store is faked
// and the trip is taken for real, with the overlay's own cleanCfg and saveCfg.
const { slice, fn, src } = require("./srcslice");

let fail=0; const ok=(n,c)=>{ if(!c){fail++; console.log("FAIL:",n);} else console.log("ok:",n); };

// ------------------------------------------------------------------ the stubs
// A localStorage that is a plain object, so an assertion can look at the BYTES
// that would have been written rather than at the live CFG — the live one is
// right in every one of these cases and is not what a reload reads.
let DISK = {};
global.store = { get:k=>(k in DISK? DISK[k] : null), set:(k,v)=>{DISK[k]=v;}, del:k=>{delete DISK[k];} };
// chainCacheClear is absent on purpose: saveCfg guards it with typeof, and a
// config write must not require the chain layer to exist.

// ------------------------------------------------------------- the real source
// CFG_DEFAULTS through cleanCfg in one evaluation, so cleanCfg closes over the
// real ADDR_RE instead of one this file invented.
const CFGMOD = eval(slice("const CFG_DEFAULTS = {", "\n/* THE ONE ORIGIN THAT IS NOT A GUESS.")
  + "\n({cleanCfg, ADDR_RE, CFG_DEFAULTS})");
global.cleanCfg = CFGMOD.cleanCfg;
global.ADDR_RE = CFGMOD.ADDR_RE;
global.saveCfg = eval(slice("const saveCfg = () =>", "\n\n/* What Adena is asked to CALL")
  + "\n(saveCfg)");
// NOT DESTRUCTURED. `function rememberAddr` inside a direct eval is hoisted into
// this module's scope, so a `const rememberAddr` beside it is a redeclaration and
// the file will not parse. The projection is what the eval returns; the names it
// is bound to here have to be different ones.
const YOU = eval(slice("const youAddr = ", "\n/* --- your positions --- */")
  + "\n({youAddr, rememberAddr})");
const youAddr = YOU.youAddr, remember = YOU.rememberAddr;
global.reflectWallet = () => {};   // the rail, which no source harness draws
global.render = () => {};

const A = "g1n8843p9cm7pyjgvvx34wwdul8t5qjnqzmdwr6e";   // the wallet's
const B = "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5";   // one somebody typed
// Reload: exactly what the page does on load — parse the key, clean it, and
// that is the CFG the routes then ask.
const reload = () => { let s={}; try{ s=JSON.parse(DISK["cc.cfg"]||"{}"); }catch(_){}
  return CFGMOD.cleanCfg(s); };
const fresh = (over) => { DISK = {}; global.CFG = Object.assign({mode:"live"}, over||{}); };

// ------------------------------------------------- cleanCfg carries the field
fresh();
ok("a well-formed remembered address survives cleanCfg",
   CFGMOD.cleanCfg({readaddr:B}).readaddr === B);
ok("a malformed one is dropped rather than flowed downstream",
   CFGMOD.cleanCfg({readaddr:"not-an-address"}).readaddr === undefined);
ok("so is a non-string, which is what a hand-edited key looks like",
   CFGMOD.cleanCfg({readaddr:{}}).readaddr === undefined);
ok("absent stays absent — there is no default address",
   CFGMOD.cleanCfg({}).readaddr === undefined);
// The two fields are separate FACTS and cleanCfg must not conflate them: one
// can sign, the other is only ever read.
const both = CFGMOD.cleanCfg({addr:A, readaddr:B});
ok("the wallet's address and the typed one are kept apart",
   both.addr === A && both.readaddr === B);

// ------------------------------------------------------------- ONE SHAPE, ONCE
// If a second copy of the bech32 pattern ever appears, the two fields can start
// accepting different things, and the one that accepts more is the one whose
// value reaches adenaSign's `caller:`.
ok("the address shape is written exactly once in the overlay",
   (src.match(/\/\^g1\[0-9a-z\]/g) || []).length === 1);

// --------------------------------------------------------------- the precedence
// A connected wallet is a fact about now; a remembered address is a note about
// last time. Connecting must always change what the page reads.
fresh({addr:A, readaddr:B});
ok("a connected wallet outranks the remembered address", youAddr() === A);
fresh({readaddr:B});
ok("with no wallet, the remembered address is read", youAddr() === B);
fresh();
ok("with neither, the page is told nothing rather than an empty string",
   youAddr() === null);

// ------------------------------------------------------- the round trip itself
// THE POINT OF THE WHOLE CHANGE, and the assertion is on the reloaded CFG, not
// on the one still in memory.
fresh();
remember(B);
ok("pressing the button writes the address to the store",
   JSON.parse(DISK["cc.cfg"]).readaddr === B);
ok("and a reload comes back to it", youAddrOn(reload()) === B);

// CLEARING THE BOX IS THE WAY OUT. adenaDisconnect is the wallet's; this is the
// typed address's, and a value that persists with no way to clear it is not a
// preference.
remember("");
ok("clearing the field forgets the address", global.CFG.readaddr === undefined);
ok("and the forgetting is what gets written, not just what is in memory",
   JSON.parse(DISK["cc.cfg"]).readaddr === undefined);
ok("so the reload has nothing to read", youAddrOn(reload()) === null);

// A TYPO MUST NOT EVICT A WORKING ADDRESS. The page still tries to read what was
// typed — calling it wrong before the chain has been asked is a guess — but only
// a well-formed address is written down.
fresh();
remember(B);
remember("g1oops");
ok("a malformed entry leaves the remembered address standing",
   youAddrOn(reload()) === B);

// DEMO MODE REMEMBERS NOTHING: its address is a fixture, and persisting it would
// follow the reader into live mode as a real lookup of an account on no chain.
fresh({mode:"demo"});
remember(B);
ok("demo mode writes no address", DISK["cc.cfg"] === undefined);
ok("and leaves CFG alone", global.CFG.readaddr === undefined);

// ------------------------------------------------------------ disconnect means it
// The obvious press of "Read positions" is on the address already prefilled —
// the wallet's own. Left behind, that note would outlive the link, and the rail
// would say "Connect" while the page went on reading the unlinked account.
eval(fn("adenaDisconnect"));
fresh({addr:A});
remember(A);               // pressed the button on the prefilled wallet address
adenaDisconnect();
ok("disconnecting drops the wallet's own address from the remembered one",
   youAddrOn(reload()) === null);
// But an address typed FOR somebody else is a different note and survives.
fresh({addr:A});
remember(B);
adenaDisconnect();
ok("an address typed for another account survives the disconnect",
   youAddrOn(reload()) === B);

// -------------------------------------------------------- the LOCKED keep-list
// THE DEPLOYED PAGE IS THE LOCKED ONE. A field missing from this list is written
// by saveCfg and dropped on the next load — remembering nothing, in the only
// build where anybody would notice.
const keptUnderLock = (s) => {
  global.LOCKED = true; global.stored = s;
  eval(slice("if(LOCKED){\n  const keep = {};", "\nconst CFG = cleanCfg(stored);"));
  return global.stored;
};
const kept = keptUnderLock({theme:"dark", addr:A, readaddr:B, rpc:"http://127.0.0.1:26657", chainid:"dev"});
ok("a locked build keeps the remembered address", kept.readaddr === B);
ok("and still keeps the wallet and the theme", kept.addr === A && kept.theme === "dark");
ok("while the fields that name a chain are still dropped",
   kept.rpc === undefined && kept.chainid === undefined);

// ----------------------------------------------------------------- the routes
// Both pages ask the same question about the same account; remembering on one
// and not the other would make the two disagree about who is reading.
ok("/me asks youAddr()",
   /on\(\/\^\\\/me\$\/,[^\n]*youAddr\(\)\)/.test(src));
ok("/needs asks the same",
   /on\(\/\^\\\/needs\$\/,[^\n]*youAddr\(\)\)/.test(src));
// Demo still wins on both, so a fixture address cannot be overridden by a
// remembered real one.
ok("demo mode still takes precedence on both routes",
   (src.match(/CFG\.mode==="demo"\? DEMO_ME : youAddr\(\)/g) || []).length === 2);
// And both boxes write what they were pressed with. `rememberAddr(a)` alone
// would count three: the declaration's own parameter list reads the same.
ok("both address boxes remember before they read",
   (src.match(/rememberAddr\(a\); show\(a\)/g) || []).length === 2);

// --------------------------------------------- and the one mark that says "you"
// THE HOLDERS TABLE IS THE OTHER PLACE THE PAGE NAMES THE READER, and it was
// left on CFG.addr when the two routes moved to youAddr(). Its own note says
// what that costs — "without it the page is a list of strangers and a reader has
// to compare a truncated address against their wallet by eye" — and the reader
// who cannot do that comparison is precisely the one with no wallet, who now has
// a remembered address and was still being shown strangers.
ok("the holders table marks the row for whichever address is being read",
   /const mine = \(youAddr\(\)\|\|""\)\.toString\(\);/.test(src));

// AND THE SIGNING FIGURE STAYS ON CFG.addr, which is the same distinction the
// readaddr field exists for: fillVoteCommitment prints what a vote WOULD commit,
// immediately before somebody signs, and a remembered address cannot sign. This
// arm is what stops a later sweep from "finishing the job" by changing it too.
ok("the vote-commitment panel still asks the account that can sign",
   /if\(!qv \|\| !CFG\.addr \|\| !isLive\(\)\) return;/.test(src));

// youAddr reads the live global CFG; this asks it about a reloaded one.
function youAddrOn(cfg){ const prev = global.CFG; global.CFG = cfg;
  try { return youAddr(); } finally { global.CFG = prev; } }

console.log(fail? `\n${fail} FAILURES` : "\nALL PASS");
process.exit(fail?1:0);

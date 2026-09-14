// Capture the SHARE CARD — #/embed/orem/1, the view built to travel into
// somebody else's article. It is the surface with the widest distribution and
// the only one nobody had captured, so nothing in the gallery had ever been
// judged on it.
//
// Two things make it its own capture rather than a crop of the claim page:
// html.embed hides the rail and the test-clock banner, and the card carries a
// REDUCED chart (embedSpark, 300x56) rather than the claim page's bigChart. If a
// concept is going to be adopted, the card has to survive it too.
const puppeteer = require('/Users/jk/node_modules/puppeteer'), fs = require('fs');
const OUT = '/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/_fragment-embed.html';
(async () => {
  const b = await puppeteer.launch({ headless: 'new' }); const p = await b.newPage();
  await p.setCacheEnabled(false);
  await p.evaluateOnNewDocument(() => {
    localStorage.setItem("cc.cfg", JSON.stringify({ mode: "demo", theme: "dark", chat: "" }));
    localStorage.setItem("cc.intro", "1");
  });
  // The size the embed snippet actually asks for: 400x500 for a claim.
  await p.setViewport({ width: 400, height: 500 });
  await p.goto('http://127.0.0.1:8788/index.html#/embed/orem/1', { waitUntil: 'domcontentloaded' });
  await new Promise(r => setTimeout(r, 2200));
  const out = await p.evaluate(() => {
    const card = document.querySelector('.emb');
    return { card: card ? card.outerHTML : null, cls: document.documentElement.className };
  });
  if (!out.card) { console.error('no .emb card — is the static server on 8788?'); process.exit(1); }
  if (!/\bembed\b/.test(out.cls)) console.error('WARNING: html has no .embed class: ' + out.cls);
  fs.writeFileSync(OUT,
    "<!-- Captured from the live SHARE CARD (#/embed/orem/1, demo source, dark theme).\n" +
    "     The card the embed snippet drops into someone else's article, at the 400x500\n" +
    "     the snippet asks for. Regenerate with mockups/_capture-embed.js. Not committed. -->\n" +
    out.card + "\n");
  console.log("embed fragment:", Math.round(fs.statSync(OUT).size / 1024) + "KB",
    "· html class:", out.cls);
  await b.close();
})();

// A court's Discord slot, as it actually reaches a reader.
//
// WHY A BROWSER CHECK. discord_test.js calls discordHtml directly and proves the
// WORDS; nothing there proves the slot is ever filled. fillDiscord is an async
// fill into a section the court page renders empty, so every way it can fail is
// invisible to a source harness: the slot never mounted, the court page stopped
// calling the fill, the fill threw before painting, the demo branch never ran.
// franchise_panel.js records the same gap and the two occasions it bit.
//
// AND THE DEMO BRANCH IS THE ONE THAT MATTERS HERE. Demo is the repo's default
// mode and what most readers see, and this is the one fill on the page whose
// demo path draws something rather than returning early — so "the sample never
// appeared" and "the sample appeared on every court" both look like a working
// feature from the source.
//
// THE OTHER HALF IS SILENCE. guildEndpoint() returns "" in demo precisely so no
// request is made, after chatBase() defaulting to the origin put a 404 in the
// console on every demo court route. A network request from this page in demo
// mode is a regression of that fix, so it is asserted here rather than trusted.
const {PAGE, demoPage} = require('./harness');

(async () => {
  const {browser, page, errs} = await demoPage({width: 1440, height: 1000});
  let fail = 0;
  const ok = (m, c, d) => { if (!c) { fail++; console.log("FAIL: " + m + (d ? "  " + d : "")); } else console.log("ok: " + m); };

  // Every request the page makes, so "demo is silent" is measured rather than
  // asserted about the source.
  const asked = [];
  page.on('request', r => asked.push(r.url()));

  const slot = async slug => {
    await page.goto(PAGE + '#/c/' + slug, {waitUntil: 'networkidle0'});
    return page.evaluate(() => {
      const el = document.getElementById('discord');
      if (!el) return {mounted: false};
      const a = el.querySelector('a.btn');
      return {
        mounted: true,
        text: (el.textContent || '').replace(/\s+/g, ' ').trim(),
        html: el.innerHTML,
        filled: el.innerHTML.trim().length > 0,
        heading: !!el.querySelector('h2'),
        anchors: el.querySelectorAll('a').length,
        rel: a ? a.getAttribute('rel') : null,
        disabled: !!el.querySelector('[aria-disabled="true"]'),
      };
    });
  };

  // --- the court the sample gives a server ------------------------------
  const bedford = await slot('bedford');
  ok("the slot mounts on a court page", bedford.mounted);
  ok("and the sample fills it", bedford.filled, JSON.stringify(bedford).slice(0, 200));
  ok("it is headed Discord", bedford.heading && /Discord/.test(bedford.text));
  ok("the sample says it is a sample", /sample data/.test(bedford.text), bedford.text);
  ok("the sample offers no link to click", bedford.anchors === 0, "anchors=" + bedford.anchors);
  ok("...and says so rather than looking broken", bedford.disabled);

  // THE SENTENCE, ON THE PAINTED PAGE. A source harness can prove the string is
  // in the function; only this can prove a reader sees it.
  ok("the reader is told who chose the server",
     /current moderators chose this server/.test(bedford.text), bedford.text);
  ok("the reader is told what it does not mean",
     /does not mean the court is legitimate/.test(bedford.text));

  // --- a court the sample gives none ------------------------------------
  // In DEMO this stays empty: the sample is not a live court and an invitation to
  // publish a server on a court that exists on no chain is an invitation to
  // nothing. The live branch says something instead — discord_test.js holds that
  // copy; only a live service could paint it here.
  const meta = await slot('meta');
  ok("a court with no server mounts the slot anyway", meta.mounted);
  ok("...and in demo leaves it empty", !meta.filled, meta.html);
  ok("...with no heading promising something absent", !meta.heading);

  // --- demo makes no request --------------------------------------------
  const guildCalls = asked.filter(u => u.includes('/api/guild/'));
  ok("demo asks the service for nothing", guildCalls.length === 0, guildCalls.join(" "));

  // --- and nothing threw -------------------------------------------------
  const real = errs.filter(e => !/favicon/i.test(e));
  ok("no console errors on either court", real.length === 0, real.join(" | "));

  await browser.close();
  console.log(fail ? "\n" + fail + " FAILURES" : "\nALL PASS");
  process.exit(fail ? 1 : 0);
})();

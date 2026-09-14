const puppeteer=require('puppeteer'), fs=require('fs');
const OUT='/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/_fragment-docket.html';
(async()=>{
  const b=await puppeteer.launch({headless:'new'}); const p=await b.newPage();
  await p.setCacheEnabled(false);
  await p.evaluateOnNewDocument(()=>{localStorage.setItem("cc.cfg",JSON.stringify({mode:"demo",theme:"dark",chat:""}));localStorage.setItem("cc.intro","1");});
  await p.setViewport({width:1280,height:1600});
  await p.goto('http://127.0.0.1:8788/index.html#/c/orem',{waitUntil:'domcontentloaded'});
  await new Promise(r=>setTimeout(r,2600));
  const out=await p.evaluate(()=>{
    const rail=document.querySelector('.rail').outerHTML;
    const m=document.querySelector('.main').cloneNode(true);
    m.querySelectorAll('dialog').forEach(d=>d.remove());
    return {rail, main:m.outerHTML,
      stats:document.querySelectorAll('.grid.stats>div').length,
      rows:[...document.querySelectorAll('.docket a.crow')].filter(e=>e.dataset.id!=null).length};
  });
  fs.writeFileSync(OUT,
    "<!-- The COURT page (#/c/orem, demo, dark). Captured because the claim page cannot\n"+
    "     demonstrate a table: no stats strip and no stacked column of multi-digit\n"+
    "     numbers, so tabular figures and ruled cells were untestable against it.\n"+
    "     Regenerate with mockups/_capture-docket.js. Not committed. -->\n"+
    out.rail+"\n"+out.main+"\n");
  console.log(`docket fragment: ${Math.round(fs.statSync(OUT).size/1024)}KB, stats cells=${out.stats}, claim rows=${out.rows}`);
  await b.close();
})();

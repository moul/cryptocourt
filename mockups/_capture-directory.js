const puppeteer=require('puppeteer'), fs=require('fs');
const OUT='/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/_fragment-directory.html';
(async()=>{
  const b=await puppeteer.launch({headless:'new'}); const p=await b.newPage();
  await p.setCacheEnabled(false);
  await p.evaluateOnNewDocument(()=>{localStorage.setItem("cc.cfg",JSON.stringify({mode:"demo",theme:"dark",chat:""}));localStorage.setItem("cc.intro","1");});
  await p.setViewport({width:1280,height:1600});
  await p.goto('http://127.0.0.1:8788/index.html#/',{waitUntil:'domcontentloaded'});
  await new Promise(r=>setTimeout(r,2400));
  const out=await p.evaluate(()=>{
    const m=document.querySelector('.main').cloneNode(true);
    m.querySelectorAll('dialog').forEach(d=>d.remove());
    // what structures does the directory actually have that the switches touch?
    const q=s=>document.querySelectorAll('.main '+s).length;
    return {rail:document.querySelector('.rail').outerHTML, main:m.outerHTML, has:{panel:q('.panel'), ticket:q('.ticket'), stats:q('.grid.stats>div'),
      crow:q('.docket a.crow'), secH:q('.sec-h'), bigchart:q('.bigchart'), sbar:q('.sbar'),
      cards:q('.card'), courtrow:q('a.courtrow, a.crow')}};
  });
  fs.writeFileSync(OUT,
    "<!-- The DIRECTORY (#/, demo, dark). Third capture: elements get adopted\n"+
    "     site-wide, and until now every switch had only been judged on a claim\n"+
    "     page and a docket. Regenerate with mockups/_capture-directory.js.\n"+
    "     Includes the RAIL, like the other two captures: without it nothing that\n"+
    "     touches the sidebar could be judged on this view, and the page rendered\n"+
    "     full-width, which is not what the site does. -->\n"+out.rail+"\n"+out.main+"\n");
  console.log("directory captured:", Math.round(fs.statSync(OUT).size/1024)+"KB");
  console.log("structures the switches touch:", JSON.stringify(out.has));
  await b.close();
})();

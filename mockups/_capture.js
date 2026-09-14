const puppeteer=require('puppeteer'), fs=require('fs');
(async()=>{
  const b=await puppeteer.launch({headless:'new'}); const p=await b.newPage();
  await p.setCacheEnabled(false);
  await p.evaluateOnNewDocument(()=>{localStorage.setItem("cc.cfg",JSON.stringify({mode:"demo",theme:"dark",chat:""}));localStorage.setItem("cc.intro","1");});
  await p.setViewport({width:1280,height:1400});
  await p.goto('http://127.0.0.1:8788/index.html#/c/orem/1',{waitUntil:'domcontentloaded'});
  await new Promise(r=>setTimeout(r,2200));
  const out=await p.evaluate(()=>{
    // the real page's rail + main, with the share dialog dropped (it is modal)
    const rail=document.querySelector('.rail').outerHTML;
    const m=document.querySelector('.main').cloneNode(true);
    m.querySelectorAll('dialog').forEach(d=>d.remove());
    return {rail, main:m.outerHTML};
  });
  fs.writeFileSync('/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/_fragment.html',
    "<!-- Captured from the live claim page (#/c/orem/1, demo source, dark theme) so every\n"+
    "     concept is judged against REAL content: real numbers, real labels, the real\n"+
    "     chart, the real ticket. Regenerate with mockups/_capture.js. Not committed. -->\n"+
    out.rail+"\n"+out.main+"\n");
  console.log("fragment:", Math.round(fs.statSync('/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/_fragment.html').size/1024)+"KB");
  await b.close();
})();

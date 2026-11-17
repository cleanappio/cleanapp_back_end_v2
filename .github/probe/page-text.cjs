// Read-only: opens one of CleanApp's own public pages and prints its text.
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  let status = 'ok';
  try {
    const resp = await page.goto('https://www.cleanapp.io/clustersamples', { waitUntil: 'load', timeout: 60000 });
    status = String(resp && resp.status());
  } catch (e) { status = String(e.message).slice(0, 120); }
  await page.waitForTimeout(6000);
  const info = await page.evaluate(() => {
    const ids = Array.from(document.querySelectorAll('[id]')).map((e) => e.id).filter((id) => id && !id.startsWith('__') && id.length < 60);
    const heads = Array.from(document.querySelectorAll('h1,h2,h3')).map((h) => h.tagName + ': ' + (h.innerText || '').replace(/\s+/g, ' ').slice(0, 140));
    const target = document.getElementById('google-ads');
    const links = (root) => Array.from((root || document).querySelectorAll('a[href]')).map((a) => (a.innerText || '').replace(/\s+/g, ' ').slice(0, 60) + ' -> ' + a.href).slice(0, 60);
    return {
      title: document.title, url: location.href, ids: ids.slice(0, 80), heads: heads.slice(0, 80),
      target: target ? target.innerText : null, targetLinks: target ? links(target) : [],
      body: document.body.innerText,
    };
  });
  console.log('PG status=' + status + ' title=' + info.title + ' url=' + info.url);
  console.log('PGIDS ' + info.ids.join(', '));
  for (const h of info.heads) console.log('PGH ' + h);
  const dump = (tag, text) => { const t = String(text || ''); for (let i = 0; i < t.length; i += 3500) console.log(tag + ' ' + JSON.stringify(t.slice(i, i + 3500))); };
  if (info.target) { dump('PGT', info.target); for (const l of info.targetLinks) console.log('PGL ' + l); }
  else console.log('PGT none');
  dump('PGB', info.body.slice(0, 60000));
  await browser.close();
})().catch((e) => { console.log('PG failed: ' + e.message); process.exit(1); });

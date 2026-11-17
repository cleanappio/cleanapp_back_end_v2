// Read-only: opens one of CleanApp's own public pages and prints the text of one tab.
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.goto('https://www.cleanapp.io/clustersamples#google-ads', { waitUntil: 'load', timeout: 60000 });
  await page.waitForTimeout(4000);
  const tab = page.locator('#tab-google-ads');
  if (await tab.count()) { await tab.click().catch(() => {}); }
  await page.waitForTimeout(2500);
  // Open every collapsed section inside the panel so its wording is included.
  await page.evaluate(() => { document.querySelectorAll('details').forEach((d) => { d.open = true; }); });
  for (const label of ['Read the preserved wording', 'Show all', 'Show more', 'All 12 reports']) {
    const buttons = page.getByText(label, { exact: false });
    const n = Math.min(await buttons.count(), 20);
    for (let i = 0; i < n; i++) { await buttons.nth(i).click({ timeout: 1500 }).catch(() => {}); }
  }
  await page.waitForTimeout(1500);
  const info = await page.evaluate(() => {
    const panel = document.getElementById('panel-google-ads') || document.querySelector('[aria-labelledby="tab-google-ads"]');
    const visible = Array.from(document.querySelectorAll('[role="tabpanel"]')).filter((p) => p.offsetParent !== null).map((p) => p.id);
    const root = panel || document.body;
    const links = Array.from(root.querySelectorAll('a[href]')).map((a) => (a.innerText || '').replace(/\s+/g, ' ').slice(0, 70) + ' -> ' + a.href).slice(0, 80);
    return { url: location.href, hasPanel: !!panel, visible, text: panel ? (panel.innerText || panel.textContent) : document.body.innerText, links };
  });
  console.log('PG2 url=' + info.url + ' hasPanel=' + info.hasPanel + ' visiblePanels=' + info.visible.join(','));
  const t = String(info.text || '');
  for (let i = 0; i < t.length; i += 3500) console.log('PG2T ' + JSON.stringify(t.slice(i, i + 3500)));
  for (const l of info.links) console.log('PG2L ' + l);
  await browser.close();
})().catch((e) => { console.log('PG2 failed: ' + e.message); process.exit(1); });

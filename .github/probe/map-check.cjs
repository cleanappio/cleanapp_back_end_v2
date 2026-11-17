// Read-only: opens the public map in headless Chromium and reports which
// report-data requests it makes. Changes nothing.
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ args: ['--enable-unsafe-swiftshader', '--use-gl=angle', '--use-angle=swiftshader'] });
  for (const [name, url, viewport] of [
    ['web', 'https://www.cleanapp.io/', { width: 1280, height: 800 }],
    ['embed (mobile app map)', 'https://embed.cleanapp.io/', { width: 390, height: 844 }],
  ]) {
    const page = await browser.newPage({ viewport });
    const seen = { tiles: 0, tilesOk: 0, tileBytes: 0, points: 0, hotspots: 0, brands: 0 };
    const errors = [];
    page.on('pageerror', (e) => errors.push(String(e.message).slice(0, 200)));
    page.on('response', async (r) => {
      const u = r.url();
      if (u.includes('/api/v4/reports/tiles/')) {
        seen.tiles++;
        if (r.status() === 200 || r.status() === 204) seen.tilesOk++;
        try { seen.tileBytes += (await r.body()).length; } catch {}
      }
      if (u.includes('/api/v4/reports/points')) seen.points++;
      if (u.includes('/api/v4/reports/hotspots')) seen.hotspots++;
      if (u.includes('/api/v4/brands/summary')) seen.brands++;
    });
    const started = Date.now();
    let loaded = 'ok';
    try { await page.goto(url, { waitUntil: 'load', timeout: 60000 }); } catch (e) { loaded = String(e.message).slice(0, 120); }
    await page.waitForTimeout(25000);
    const canvas = await page.locator('canvas.mapboxgl-canvas').count();
    const session = await page.context().newCDPSession(page);
    await session.send('HeapProfiler.collectGarbage');
    const heap = Math.round((await session.send('Runtime.getHeapUsage')).usedSize / 1e6);
    console.log(`MAPCHECK ${name}: load=${loaded} mapCanvas=${canvas} tileRequests=${seen.tiles} tilesOk=${seen.tilesOk} tileKB=${Math.round(seen.tileBytes / 1024)} allPointsRequests=${seen.points} hotspotRequests=${seen.hotspots} brandRequests=${seen.brands} jsHeapMB=${heap} pageErrors=${errors.length} seconds=${Math.round((Date.now() - started) / 1000)}`);
    for (const e of errors.slice(0, 3)) console.log(`MAPCHECK ${name} error: ${e}`);
    await page.close();
  }
  await browser.close();
})().catch((e) => { console.log('MAPCHECK failed: ' + e.message); process.exit(1); });

// Read-only: opens the mobile app's map page the way the app's WebView does
// and reports what it requests and whether it renders. Changes nothing.
const { chromium, webkit, devices } = require('playwright');
(async () => {
  for (const [name, engine, device, launch] of [
    ['android-webview', chromium, devices['Pixel 7'], { args: ['--enable-unsafe-swiftshader', '--use-gl=angle', '--use-angle=swiftshader'] }],
    ['iphone-webview', webkit, devices['iPhone 14'], {}],
  ]) {
    for (const withNative of [true, false]) {
      const label = `${name}${withNative ? '+app' : ''}`;
      let browser;
      try {
        browser = await engine.launch(launch);
        const context = await browser.newContext({ ...device });
        const page = await context.newPage();
        if (withNative) {
          await page.addInitScript(() => {
            window.__posted = [];
            window.ReactNativeWebView = { postMessage: (m) => window.__posted.push(String(m).slice(0, 120)) };
          });
        }
        const seen = { tiles: 0, tilesOk: 0, points: 0, hotspots: 0, brands: 0, mapbox: 0, mapboxBad: 0 };
        const errors = [], failed = [], bad = [], logs = [];
        page.on('pageerror', (e) => errors.push(String(e.message).slice(0, 300)));
        page.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') logs.push(`${m.type()}: ${m.text().slice(0, 240)}`); });
        page.on('requestfailed', (r) => failed.push(`${r.url().slice(0, 110)} ${r.failure() && r.failure().errorText}`));
        page.on('response', (r) => {
          const u = r.url(), s = r.status();
          if (u.includes('/api/v4/reports/tiles/')) { seen.tiles++; if (s === 200 || s === 204) seen.tilesOk++; }
          if (u.includes('/api/v4/reports/points')) seen.points++;
          if (u.includes('/api/v4/reports/hotspots')) seen.hotspots++;
          if (u.includes('/api/v4/brands/summary')) seen.brands++;
          if (u.includes('mapbox.com')) { seen.mapbox++; if (s >= 400) seen.mapboxBad++; }
          if (s >= 400) bad.push(`${s} ${u.slice(0, 110)}`);
        });
        let loaded = 'ok';
        try { await page.goto('https://embed.cleanapp.io/', { waitUntil: 'load', timeout: 60000 }); } catch (e) { loaded = String(e.message).slice(0, 160); }
        await page.waitForTimeout(25000);
        const dom = await page.evaluate(() => {
          const canvas = document.querySelector('canvas.mapboxgl-canvas');
          let gl = 'n/a';
          try { const c = document.createElement('canvas'); gl = (c.getContext('webgl2') ? 'webgl2' : c.getContext('webgl') ? 'webgl' : 'none'); } catch (e) { gl = 'error'; }
          return {
            canvas: canvas ? `${canvas.width}x${canvas.height}` : 'none',
            gl,
            spinners: document.querySelectorAll('[class*="animate-spin"], [class*="spinner"], [class*="loading"]').length,
            text: (document.body.innerText || '').replace(/\s+/g, ' ').slice(0, 260),
            posted: window.__posted || [],
            title: document.title,
          };
        }).catch((e) => ({ evalError: String(e.message).slice(0, 200) }));
        console.log(`MAPCHECK ${label}: load=${loaded} ${JSON.stringify(seen)} dom=${JSON.stringify(dom)}`);
        for (const e of errors.slice(0, 6)) console.log(`MAPCHECK ${label} pageerror: ${e}`);
        for (const e of [...new Set(logs)].slice(0, 12)) console.log(`MAPCHECK ${label} console ${e}`);
        for (const e of [...new Set(failed)].slice(0, 8)) console.log(`MAPCHECK ${label} failed: ${e}`);
        for (const e of [...new Set(bad)].slice(0, 8)) console.log(`MAPCHECK ${label} http: ${e}`);
      } catch (e) {
        console.log(`MAPCHECK ${label} crashed: ${String(e.message).slice(0, 300)}`);
      } finally {
        if (browser) await browser.close().catch(() => {});
      }
    }
  }
})();

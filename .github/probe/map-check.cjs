// Read-only: opens the mobile app's map page as the app's WebView does and
// prints small screenshots (base64) to the log. Changes nothing.
const { chromium, webkit, devices } = require('playwright');
(async () => {
  for (const [name, engine, device, launch] of [
    ['android', chromium, devices['Pixel 7'], { args: ['--enable-unsafe-swiftshader', '--use-gl=angle', '--use-angle=swiftshader'] }],
    ['iphone', webkit, devices['iPhone 14'], {}],
  ]) {
    let browser;
    try {
      browser = await engine.launch(launch);
      const context = await browser.newContext({ ...device, deviceScaleFactor: 1 });
      const page = await context.newPage();
      await page.addInitScript(() => { window.ReactNativeWebView = { postMessage() {} }; });
      const logs = [];
      page.on('console', (m) => logs.push(`${m.type()}: ${m.text().slice(0, 200)}`));
      page.on('pageerror', (e) => logs.push(`pageerror: ${String(e.message).slice(0, 200)}`));
      const t0 = Date.now();
      await page.goto('https://embed.cleanapp.io/', { waitUntil: 'load', timeout: 60000 });
      console.log(`SHOT ${name} loadEventMs=${Date.now() - t0}`);
      const snap = async (tag) => {
        const state = await page.evaluate(() => ({
          spinner: !!document.querySelector('.animate-spin'),
          canvas: !!document.querySelector('canvas.mapboxgl-canvas'),
          url: location.href,
        }));
        const shot = await page.screenshot({ type: 'jpeg', quality: 38 });
        console.log(`SHOT ${name} ${tag} t=${Date.now() - t0}ms state=${JSON.stringify(state)}`);
        const b64 = shot.toString('base64');
        for (let i = 0; i < b64.length; i += 4000) console.log(`IMG ${name}-${tag} ${b64.slice(i, i + 4000)}`);
      };
      await page.waitForTimeout(3000);
      await snap('3s');
      await page.evaluate(() => {
        const message = JSON.stringify({ type: 'NATIVE_LOCATION', lat: 47.3769, lng: 8.5417, accuracy: 20 });
        window.localStorage.setItem('cleanapp_user_location', JSON.stringify({ latitude: 47.3769, longitude: 8.5417, accuracy: 20, timestamp: Date.now() }));
        window.localStorage.setItem('cleanapp_location_permission', 'granted');
        window.dispatchEvent(new MessageEvent('message', { data: message }));
      });
      await page.waitForTimeout(9000);
      await snap('12s');
      await page.waitForTimeout(18000);
      await snap('30s');
      for (const l of [...new Set(logs)].slice(0, 25)) console.log(`SHOT ${name} log ${l}`);
    } catch (e) {
      console.log(`SHOT ${name} crashed: ${String(e.message).slice(0, 300)}`);
    } finally {
      if (browser) await browser.close().catch(() => {});
    }
  }
})();

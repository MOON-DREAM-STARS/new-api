'use strict';

const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer-core');

function parseArgs(argv) {
  const out = {};
  for (let i = 2; i < argv.length; i += 1) {
    const key = argv[i];
    if (!key.startsWith('--')) throw new Error(`unexpected argument: ${key}`);
    const value = argv[i + 1];
    if (value === undefined || value.startsWith('--')) throw new Error(`missing value for ${key}`);
    out[key.slice(2)] = value;
    i += 1;
  }
  return out;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function readRxBytes() {
  const candidates = ['/sys/class/net/eth0/statistics/rx_bytes', '/sys/class/net/enp0s3/statistics/rx_bytes'];
  for (const candidate of candidates) {
    try { return Number(fs.readFileSync(candidate, 'utf8').trim()); } catch (_) {}
  }
  const net = fs.readdirSync('/sys/class/net');
  for (const name of net) {
    if (name === 'lo') continue;
    const candidate = `/sys/class/net/${name}/statistics/rx_bytes`;
    try { return Number(fs.readFileSync(candidate, 'utf8').trim()); } catch (_) {}
  }
  throw new Error('no network receive counter found');
}

async function waitForCanvas(page, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const metric = await page.evaluate(() => {
      const canvases = Array.from(document.querySelectorAll('canvas'));
      let best = null;
      for (const canvas of canvases) {
        const rect = canvas.getBoundingClientRect();
        const area = rect.width * rect.height;
        if (!best || area > best.area) {
          best = { area, width: rect.width, height: rect.height, attrWidth: canvas.width, attrHeight: canvas.height };
        }
      }
      return best;
    });
    if (metric && metric.width >= 300 && metric.height >= 200 && metric.attrWidth > 0 && metric.attrHeight > 0) {
      return metric;
    }
    await sleep(500);
  }
  throw new Error('no usable canvas appeared');
}

async function markLargestCanvas(page) {
  return page.evaluate(() => {
    document.querySelectorAll('[data-spike-canvas]').forEach((el) => el.removeAttribute('data-spike-canvas'));
    const canvases = Array.from(document.querySelectorAll('canvas'));
    let best = null;
    for (const canvas of canvases) {
      const rect = canvas.getBoundingClientRect();
      const area = rect.width * rect.height;
      if (!best || area > best.area) best = { canvas, area };
    }
    if (!best) return null;
    best.canvas.setAttribute('data-spike-canvas', '1');
    const rect = best.canvas.getBoundingClientRect();
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, attrWidth: best.canvas.width, attrHeight: best.canvas.height };
  });
}

async function remotePoint(page, remoteWidth, remoteHeight, x, y) {
  const box = await markLargestCanvas(page);
  if (!box) throw new Error('canvas disappeared');
  return {
    x: box.x + (x / remoteWidth) * box.width,
    y: box.y + (y / remoteHeight) * box.height,
    box,
  };
}

async function screenshotCanvas(page, outputPath) {
  const element = await page.$('[data-spike-canvas]');
  if (!element) throw new Error('cannot find marked canvas for screenshot');
  await element.screenshot({ path: outputPath });
}

async function waitForRemoteReady(page, renderer) {
  await waitForCanvas(page, 45000);
  await sleep(renderer === 'kasmvnc' ? 10000 : 7000);
}

async function scenario(page, name, durationMs, action) {
  let startMs = Date.now();
  let rxStart = readRxBytes();
  let actionError = null;
  try {
    if (action) await action();
  } catch (error) {
    actionError = String(error && error.stack ? error.stack : error);
  }
  const remaining = durationMs - (Date.now() - startMs);
  if (remaining > 0) await sleep(remaining);
  const rxEnd = readRxBytes();
  return {
    name,
    startMs,
    endMs: Date.now(),
    durationMs: Date.now() - startMs,
    serverToClientBytes: rxEnd - rxStart,
    actionError,
  };
}

async function main() {
  const args = parseArgs(process.argv);
  const renderer = args.renderer || 'baseline';
  const url = args.url;
  const width = Number(args.width || 1280);
  const height = Number(args.height || 720);
  const idleMs = Number(args.idleMs || 120000);
  const scrollMs = Number(args.scrollMs || 60000);
  const navMs = Number(args.navMs || 12000);
  const outDir = args.out || '/out';
  if (!url) throw new Error('--url is required');
  fs.mkdirSync(outDir, { recursive: true });

  const browser = await puppeteer.launch({
    executablePath: '/usr/bin/chromium',
    headless: 'new',
    args: [
      '--no-sandbox',
      '--disable-gpu',
      '--disable-dev-shm-usage',
      '--ignore-certificate-errors',
      '--disable-features=TranslateUI,PasswordManager',
      `--window-size=${width},${height}`,
    ],
    defaultViewport: { width, height, deviceScaleFactor: 1 },
  });
  const page = await browser.newPage();
  page.on('console', (msg) => process.stdout.write(`[client-console] ${msg.type()}: ${msg.text()}\n`));
  page.on('pageerror', (err) => process.stdout.write(`[client-pageerror] ${err.message}\n`));

  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });
  await waitForRemoteReady(page, renderer);
  await markLargestCanvas(page);

  const screenshotPath = path.join(outDir, `${renderer}-${width}x${height}-clarity.png`);
  await screenshotCanvas(page, screenshotPath);
  const canvas = await markLargestCanvas(page);

  const scenarios = [];
  scenarios.push(await scenario(page, 'idle_120s', idleMs, async () => {
    await sleep(1000);
  }));

  scenarios.push(await scenario(page, 'active_scroll_60s', scrollMs, async () => {
    const point = await remotePoint(page, width, height, Math.round(width * 0.55), 430);
    await page.mouse.move(point.x, point.y);
    await page.mouse.click(point.x, point.y);
    const deadline = Date.now() + scrollMs;
    let step = 0;
    while (Date.now() < deadline) {
      const direction = Math.floor(step / 12) % 2 === 0 ? 1 : -1;
      await page.mouse.wheel({ deltaY: direction * 640 });
      step += 1;
      await sleep(90);
    }
  }));

  const diagnostics = {};
  {
    const reload = await remotePoint(page, width, height, 78, 34);
    await page.mouse.click(reload.x, reload.y);
    await sleep(3000);

    const editor = await remotePoint(page, width, height, 220, 170);
    await page.mouse.click(editor.x, editor.y);
    await page.keyboard.type('ASCII-keyboard-probe ');
    await sleep(1000);
    diagnostics.keyboardScreenshot = path.join(outDir, `${renderer}-${width}x${height}-keyboard.png`);
    await screenshotCanvas(page, diagnostics.keyboardScreenshot);

    const fileInput = await remotePoint(page, width, height, Math.round(width * 0.515 + 30), 170);
    await page.mouse.click(fileInput.x, fileInput.y);
    await sleep(2500);
    diagnostics.fileChooserScreenshot = path.join(outDir, `${renderer}-${width}x${height}-filechooser.png`);
    await screenshotCanvas(page, diagnostics.fileChooserScreenshot);
    await page.keyboard.press('Escape');
    await sleep(1000);
  }
  scenarios.push(await scenario(page, 'page_load_and_navigation', navMs, async () => {
    const reload = await remotePoint(page, width, height, 78, 34);
    await page.mouse.click(reload.x, reload.y);
    await sleep(3000);
    const next = await remotePoint(page, width, height, 258, 34);
    await page.mouse.click(next.x, next.y);
    await sleep(6000);
    await screenshotCanvas(page, path.join(outDir, `${renderer}-${width}x${height}-navigation.png`));
  }));

  const result = {
    renderer,
    url,
    width,
    height,
    clientViewport: { width, height },
    canvas,
    screenshotPath,
    diagnostics,
    scenarios,
    finishedAtMs: Date.now(),
  };
  const resultPath = path.join(outDir, `${renderer}-${width}x${height}.json`);
  fs.writeFileSync(resultPath, `${JSON.stringify(result, null, 2)}\n`);
  process.stdout.write(`SPIKE_RESULT ${resultPath}\n${JSON.stringify(result)}\n`);
  await browser.close();
}

main().catch((error) => {
  process.stderr.write(`${error && error.stack ? error.stack : error}\n`);
  process.exitCode = 1;
});

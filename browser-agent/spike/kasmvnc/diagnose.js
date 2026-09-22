'use strict';

const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer-core');

function arg(name, fallback) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : fallback;
}
function sleep(ms) { return new Promise((resolve) => setTimeout(resolve, ms)); }

async function markLargestCanvas(page) {
  return page.evaluate(() => {
    document.querySelectorAll('[data-spike-canvas]').forEach((el) => el.removeAttribute('data-spike-canvas'));
    const canvases = Array.from(document.querySelectorAll('canvas'));
    let best = null;
    for (const canvas of canvases) {
      const rect = canvas.getBoundingClientRect();
      const area = rect.width * rect.height;
      if (!best || area > best.area) best = { canvas, area, rect };
    }
    if (!best) return null;
    best.canvas.setAttribute('data-spike-canvas', '1');
    return { x: best.rect.x, y: best.rect.y, width: best.rect.width, height: best.rect.height, attrWidth: best.canvas.width, attrHeight: best.canvas.height };
  });
}

async function point(page, width, height, x, y) {
  const box = await markLargestCanvas(page);
  if (!box) throw new Error('no canvas');
  return { x: box.x + (x / width) * box.width, y: box.y + (y / height) * box.height };
}

async function shot(page, file) {
  const canvas = await page.$('[data-spike-canvas]');
  if (!canvas) throw new Error('no marked canvas');
  await canvas.screenshot({ path: file });
}

async function main() {
  const renderer = arg('renderer', 'baseline');
  const url = arg('url');
  const width = Number(arg('width', '1280'));
  const height = Number(arg('height', '720'));
  const out = arg('out', '/out');
  if (!url) throw new Error('--url required');
  fs.mkdirSync(out, { recursive: true });
  const browser = await puppeteer.launch({
    executablePath: '/usr/bin/chromium',
    headless: 'new',
    args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage', '--ignore-certificate-errors'],
    defaultViewport: { width, height, deviceScaleFactor: 1 },
  });
  const page = await browser.newPage();
  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    const box = await markLargestCanvas(page);
    if (box && box.width > 300 && box.height > 200) break;
    await sleep(500);
  }
  await sleep(renderer === 'kasmvnc' ? 10000 : 7000);

  const initial = path.join(out, `diagnose-${renderer}-${width}x${height}-initial.png`);
  await shot(page, initial);

  const editor = await point(page, width, height, 220, 190);
  await page.mouse.click(editor.x, editor.y);
  await page.keyboard.type('ASCII-keyboard-probe');
  await sleep(1000);
  const keyboard = path.join(out, `diagnose-${renderer}-${width}x${height}-keyboard.png`);
  await shot(page, keyboard);

  const fileInput = await point(page, width, height, 690, 170);
  await page.mouse.click(fileInput.x, fileInput.y);
  await sleep(2500);
  const fileChooser = path.join(out, `diagnose-${renderer}-${width}x${height}-filechooser.png`);
  await shot(page, fileChooser);
  await page.keyboard.press('Escape');

  console.log(JSON.stringify({ renderer, initial, keyboard, fileChooser }));
  await browser.close();
}
main().catch((error) => { console.error(error.stack || String(error)); process.exit(1); });

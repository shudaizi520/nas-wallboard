import {mkdir, readFile, writeFile} from 'node:fs/promises';
import {dirname, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

import {chromium} from 'playwright';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const source = resolve(root, 'web/icon.svg');
const destination = resolve(root, 'windows/NASWallboard.Desktop/Assets/NASWallboard.ico');
const sizes = [16, 24, 32, 48, 64, 128, 256];

const svg = await readFile(source, 'utf8');
const dataUrl = `data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}`;
const browser = await chromium.launch({headless: true});
const page = await browser.newPage();
const images = [];

try {
  for (const size of sizes) {
    await page.setViewportSize({width: size, height: size});
    await page.setContent(`<!doctype html><style>*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent}img{display:block;width:100%;height:100%}</style><img alt="" src="${dataUrl}">`);
    await page.locator('img').waitFor({state: 'visible'});
    images.push(await page.screenshot({omitBackground: true}));
  }
} finally {
  await browser.close();
}

const directorySize = 6 + images.length * 16;
const directory = Buffer.alloc(directorySize);
directory.writeUInt16LE(0, 0);
directory.writeUInt16LE(1, 2);
directory.writeUInt16LE(images.length, 4);

let offset = directorySize;
for (let index = 0; index < images.length; index += 1) {
  const entry = 6 + index * 16;
  const size = sizes[index];
  directory[entry] = size === 256 ? 0 : size;
  directory[entry + 1] = size === 256 ? 0 : size;
  directory[entry + 2] = 0;
  directory[entry + 3] = 0;
  directory.writeUInt16LE(1, entry + 4);
  directory.writeUInt16LE(32, entry + 6);
  directory.writeUInt32LE(images[index].length, entry + 8);
  directory.writeUInt32LE(offset, entry + 12);
  offset += images[index].length;
}

await mkdir(dirname(destination), {recursive: true});
await writeFile(destination, Buffer.concat([directory, ...images]));

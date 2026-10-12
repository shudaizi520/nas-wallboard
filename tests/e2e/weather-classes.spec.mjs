import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';
const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const icons=['weather-sunny','weather-cloudy','weather-overcast','weather-rain','weather-heavy-rain','weather-storm','weather-hail','weather-freezing-rain','weather-sleet','weather-snow','weather-heavy-snow','weather-fog','weather-haze','weather-dust','weather-hot','weather-cold','weather-unknown'];

for(const width of [300,360])test(`all classified icons render without changing weather geometry at ${width}px`,async({page})=>{
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:{...fixture,width}});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  for(const icon of icons){
    const hours=Array.from({length:6},(_,i)=>({at:`2026-10-13T0${i}:00:00+08:00`,label:`次日${i}时`,icon,condition:'原始天气',temperature:'20°',rain:''}));
    const days=Array.from({length:5},(_,i)=>({date:`2026-10-${13+i}`,label:['今天','周三','周四','周五','周六'][i],icon,condition:'原始天气',low:'20°',high:'30°',rain:'',detail:'白天原始天气'}));
    await page.evaluate(async raw=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,raw);},{...fixture,width,activities:fixture.activities.map(a=>a.id==='weather'?{...a,icon,forecast_hours:hours,forecast_days:days}:a)});
    const row=page.locator('[data-key="weather"]');
    await expect(row).toHaveAttribute('data-icon',icon);
    const rendered=await row.locator('.weather-hour svg,.weather-day svg').evaluateAll(nodes=>nodes.map(svg=>{
      const reference=svg.querySelector('use').getAttribute('href');
      return {reference,exists:!!document.querySelector(reference),width:svg.getBoundingClientRect().width,overflow:svg.parentElement.scrollWidth>svg.parentElement.clientWidth+1};
    }));
    expect(rendered).toHaveLength(11);
    for(const r of rendered){expect(r.reference).toBe('#icon-'+icon);expect(r.exists).toBe(true);expect(r.width).toBe(20);expect(r.overflow).toBe(false);}
    const heights=await row.locator('.weather-day').evaluateAll(nodes=>nodes.map(n=>({height:n.getBoundingClientRect().height,font:getComputedStyle(n.querySelector('[data-field="day"]')).fontSize})));
    for(const h of heights){expect(h.height).toBeLessThanOrEqual(42.5);expect(h.font).toBe('12px');}
  }
  // Rasterize the actual shipped storm artwork. The lower-left region must
  // contain rain strokes, not just the cloud and central lightning bolt.
  const rainPixels=await page.evaluate(async()=>{
    const symbol=document.getElementById('icon-weather-storm');
    const markup=`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="240" viewBox="0 0 24 24"><g fill="none" stroke="white" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">${symbol.innerHTML}</g></svg>`;
    const url=URL.createObjectURL(new Blob([markup],{type:'image/svg+xml'}));
    try {
      const picture=new Image();picture.src=url;await picture.decode();
      const canvas=document.createElement('canvas');canvas.width=canvas.height=240;
      const context=canvas.getContext('2d');context.drawImage(picture,0,0);
      const pixels=context.getImageData(50,180,35,50).data;
      let count=0;for(let i=3;i<pixels.length;i+=4)if(pixels[i]>20)count++;
      return count;
    } finally {URL.revokeObjectURL(url);}
  });
  expect(rainPixels).toBeGreaterThan(20);
  // Missing weather has no question mark and never masquerades as cloud/rain.
  const unknownPixels=await page.evaluate(async()=>{
    const symbol=document.getElementById('icon-weather-unknown');
    const markup=`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="240" viewBox="0 0 24 24"><g fill="none" stroke="white" stroke-width="1.4">${symbol.innerHTML}</g></svg>`;
    const url=URL.createObjectURL(new Blob([markup],{type:'image/svg+xml'}));
    try {
      const picture=new Image();picture.src=url;await picture.decode();
      const canvas=document.createElement('canvas');canvas.width=canvas.height=240;
      const context=canvas.getContext('2d');context.drawImage(picture,0,0);
      const pixels=context.getImageData(0,0,240,240).data;
      let count=0;for(let i=3;i<pixels.length;i+=4)if(pixels[i]>20)count++;
      return count;
    } finally {URL.revokeObjectURL(url);}
  });
  expect(unknownPixels).toBe(0);
});

import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const hours=['现在','14时','16时','17时','18时','19时'].map((label,i)=>({at:`2026-10-11T${[12,14,16,17,18,19][i]}:00:00+08:00`,label,icon:i<4?'weather-sunny':'weather-rain',condition:i<4?'晴':'小雨',temperature:i<4?'32°':'28°',rain:i<4?'':'65%'}));
const days=Array.from({length:5},(_,i)=>({date:`2026-10-${11+i}`,label:['今天','周一','周二','周三','周四'][i],icon:'weather-cloudy',condition:'多云',low:'24°',high:'32°',rain:'',detail:'白天多云'}));

for(const width of [300,360])test(`fixed hourly strip fits ${width}px and retains daily fonts and warning text`,async({page})=>{
  const dashboard={...fixture,width,activities:fixture.activities.map(a=>a.id==='weather'?{...a,detail:'18–19时可能有雨',forecast_hours:hours,forecast_days:days}:a)};
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:dashboard});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  const row=page.locator('[data-key="weather"]'),strip=row.locator('.weather-hourly');
  await expect(strip.locator('.weather-hour')).toHaveCount(6);
  const geometry=await strip.evaluate(n=>({height:n.getBoundingClientRect().height,display:getComputedStyle(n).display,overflow:n.scrollWidth>n.clientWidth+1,rows:[...n.children].map(h=>({box:h.getBoundingClientRect().toJSON(),icon:h.querySelector('svg').getBoundingClientRect().toJSON(),timeSize:getComputedStyle(h.querySelector('[data-field="hour"]')).fontSize,overflow:h.scrollWidth>h.clientWidth+1}))}));
  expect(geometry.display).toBe('grid'); expect(geometry.overflow).toBe(false); expect(geometry.height).toBeLessThanOrEqual(85);
  for(const [i,h] of geometry.rows.entries()) {expect(h.icon.width).toBeLessThanOrEqual(21);expect(h.timeSize).toBe('12px');expect(h.overflow).toBe(false);expect(h.box.top).toBeCloseTo(geometry.rows[0].box.top,1);if(i)expect(h.box.left).toBeGreaterThanOrEqual(geometry.rows[i-1].box.right);}
  await expect(strip.locator('button')).toHaveCount(0);
  await expect(strip.locator('[data-field="hour"]')).toHaveText(['现在','14时','16时','17时','18时','19时']);
  await expect(strip.locator('[data-field="rain"]').last()).toHaveText('65%');
  const daily=await row.locator('.weather-day').evaluateAll(nodes=>nodes.map(n=>({height:n.getBoundingClientRect().height,font:getComputedStyle(n.querySelector('[data-field="day"]')).fontSize})));
  for(const d of daily){expect(d.height).toBeLessThanOrEqual(42.5);expect(d.font).toBe('12px');}
  const warning={...dashboard,activities:dashboard.activities.map(a=>a.id==='weather'?{...a,detail:'暴雨橙色预警\n约半小时后可能有雨\n18–19时可能有雨',detail_parts:[{text:'暴雨橙色预警',color:'orange'},{text:'\n约半小时后可能有雨\n18–19时可能有雨'}]}:a)};
  await page.evaluate(async raw=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,raw);},warning);
  await expect(row.locator('[data-warning-color="orange"]')).toBeVisible();
  await expect(row.locator('[data-field="detail"]').first()).toContainText('约半小时后可能有雨');
  const bounds=await row.evaluate(n=>({detail:n.querySelector('p').getBoundingClientRect().toJSON(),hourly:n.querySelector('.weather-hourly').getBoundingClientRect().toJSON(),daily:n.querySelector('.weather-strip').getBoundingClientRect().toJSON()}));
  expect(bounds.detail.bottom).toBeLessThanOrEqual(bounds.hourly.top);expect(bounds.hourly.bottom).toBeLessThanOrEqual(bounds.daily.top);
  await page.evaluate(async raw=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,raw);},{...dashboard,activities:dashboard.activities.map(a=>a.id==='weather'?{...a,forecast_hours:[],detail:'小时预报暂不可用'}:a)});
  await expect(strip).toBeHidden();await expect(row.locator('.weather-day')).toHaveCount(5);
});

test('next-day labels and rain probabilities fit the narrow fixed panel without smaller fonts',async({page})=>{
  const midnight=hours.map((h,i)=>({...h,at:`2026-10-${i?12:11}T${i?String((i-1)*2).padStart(2,'0'):22}:00:00+08:00`,label:i?`次日${(i-1)*2}时`:'现在',rain:i?'65%':''}));
  const dashboard={...fixture,width:300,activities:fixture.activities.map(a=>a.id==='weather'?{...a,forecast_hours:midnight,forecast_days:days}:a)};
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:dashboard});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  const strip=page.locator('[data-key="weather"] .weather-hourly');
  await expect(strip.locator('[data-field="hour"]')).toHaveText(['现在','次日0时','次日2时','次日4时','次日6时','次日8时']);
  const geometry=await strip.evaluate(n=>({overflow:n.scrollWidth>n.clientWidth+1,height:n.getBoundingClientRect().height,slots:[...n.children].map(h=>({overflow:h.scrollWidth>h.clientWidth+1,font:getComputedStyle(h.querySelector('[data-field="hour"]')).fontSize,top:h.querySelector('svg').getBoundingClientRect().top}))}));
  expect(geometry.overflow).toBe(false);expect(geometry.height).toBeLessThanOrEqual(102);
  for(const slot of geometry.slots){expect(slot.overflow).toBe(false);expect(slot.font).toBe('12px');expect(slot.top).toBeCloseTo(geometry.slots[0].top,1);}
});

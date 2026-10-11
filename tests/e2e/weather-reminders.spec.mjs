import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const ordinary=[{text:'紫外线很强 · 指数8',pinned:false},{text:'18–19时可能有雨',pinned:false}];
function dashboard(messages=ordinary){return {...fixture,activities:fixture.activities.map(a=>a.id==='weather'?{...a,detail:messages.map(m=>m.text).join('\n'),weather_reminders:messages}:a)};}
async function render(page,raw){await page.evaluate(async v=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,v);},raw);}
async function setup(page,raw=dashboard()){
  await page.clock.install({time:new Date('2026-10-11T04:00:00Z')});
  await page.clock.pauseAt(new Date('2026-10-11T04:00:01Z'));
  await page.addInitScript(()=>{window.reminderTimers=new Set();const start=window.setInterval.bind(window),stop=window.clearInterval.bind(window);window.setInterval=(cb,delay,...args)=>{const id=start(cb,delay,...args);window.reminderTimers.add(id);return id;};window.clearInterval=id=>{window.reminderTimers.delete(id);stop(id);};});
  const counts={dashboard:0};
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard'){counts.dashboard++;return route.fulfill({json:raw});}
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  await expect(page.locator('.glass-panel')).toHaveAttribute('data-ready','true');
  return counts;
}

test('ordinary weather reminders rotate every six seconds without extra requests or height jumps',async({page})=>{
  const counts=await setup(page);
  const detail=page.locator('[data-key="weather"] .activity-copy p');
  const current=detail.locator('.weather-reminder.is-current');
  await expect(current).toHaveText('紫外线很强 · 指数8');
  await expect(detail.locator('[role="group"]')).toHaveAccessibleName('天气提醒');
  const height=await page.locator('.glass-panel').evaluate(n=>n.getBoundingClientRect().height);
  await page.clock.runFor(5999);await expect(current).toHaveText('紫外线很强 · 指数8');
  await page.clock.runFor(1);await expect(current).toHaveText('18–19时可能有雨');
  expect(counts.dashboard).toBe(1);
  expect(await page.locator('.glass-panel').evaluate(n=>n.getBoundingClientRect().height)).toBeCloseTo(height,1);
  await render(page,dashboard());
  await page.clock.runFor(6000);await expect(current).toHaveText('紫外线很强 · 指数8');
  expect(counts.dashboard).toBe(1);
  expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(1);
});

test('official warning stays visible with its color throughout ordinary reminder rotation',async({page})=>{
  const messages=[{text:'暴雨橙色预警',pinned:true,detail_parts:[{text:'暴雨橙色预警',color:'orange'}]},...ordinary];
  await setup(page,dashboard(messages));
  const detail=page.locator('[data-key="weather"] .activity-copy p');
  const pinned=detail.locator('.weather-reminder-fixed');
  await expect(pinned).toContainText('暴雨橙色预警');
  await expect(pinned.locator('[data-warning-color="orange"]')).toHaveCSS('color','rgb(255, 173, 85)');
  await page.clock.runFor(6000);await expect(detail.locator('.is-current')).toHaveText('18–19时可能有雨');
  await expect(pinned).toBeVisible();
});

test('stale updates immediately evict future messages and single-message states have no rotation timer',async({page})=>{
  await setup(page);await page.clock.runFor(6000);
  await render(page,dashboard([{text:'小时预报暂不可用',pinned:true},ordinary[0]]));
  const detail=page.locator('[data-key="weather"] .activity-copy p');
  await expect(detail).not.toContainText('18–19');
  await expect(detail.locator('.weather-reminder-fixed')).toHaveText('小时预报暂不可用');
  expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(0);
  await render(page,dashboard([]));await expect(detail).toBeHidden();
  expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(0);
});

test('removing weather and failed dashboard fetches stop timers without hiding known official warnings',async({page})=>{
  const warning={text:'暴雨橙色预警',pinned:true,detail_parts:[{text:'暴雨橙色预警',color:'orange'}]};
  const raw=dashboard([warning,...ordinary]);
  await setup(page,raw);
  await page.evaluate(async()=>{const {renderFetchError}=await import('/view.js');renderFetchError(document);});
  const detail=page.locator('[data-key="weather"] .activity-copy p');
  await expect(detail.locator('.weather-reminder-fixed').filter({hasText:'暴雨橙色预警'})).toBeVisible();
  await expect(detail).toContainText('提醒数据过期');
  await expect(detail.locator('.weather-reminder-carousel')).toBeHidden();
  expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(0);
  await render(page,raw);expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(1);
  await render(page,{...raw,activities:raw.activities.filter(a=>a.id!=='weather')});
  expect(await page.evaluate(()=>window.reminderTimers.size)).toBe(0);
});

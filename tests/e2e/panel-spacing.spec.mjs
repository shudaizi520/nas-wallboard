import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const days=Array.from({length:5},(_,i)=>({date:`2026-10-${11+i}`,label:['今天','周一','周二','周三','周四'][i],icon:'weather-cloudy',condition:'多云',low:'24°',high:'32°',rain:'',detail:'白天多云'}));
const hours=['现在','14时','16时','17时','18时','19时'].map((label,i)=>({at:`2026-10-11T${[12,14,16,17,18,19][i]}:00:00+08:00`,label,icon:i<4?'weather-sunny':'weather-rain',condition:i<4?'晴':'小雨',temperature:'28°',rain:i<4?'':'65%'}));
const weather={id:'weather',icon:'weather-sunny',value:'32° · 晴',detail:'18–19时可能有雨',forecast_days:days,forecast_hours:hours};

async function setup(page,dashboard){
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:dashboard});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
}

for(const width of [300,360])test(`desktop at ${width}px keeps its compact bottom edge for every final activity`,async({page})=>{
  await page.setViewportSize({width,height:950});
  const dashboard={...fixture,width,activities:[weather]};
  await setup(page,dashboard);
  await page.goto('http://wallboard.test/?desktop=1');
  const activities=[null,{id:'fan',icon:'fan',title:'风扇',value:'66% · 直吹风'}, {id:'plex',icon:'play',title:'Plex',value:'影片',detail:'播放 · 客厅电视'}, {id:'downloads',icon:'download',title:'下载',value:'1 个 · 828.1 KB/s',progress:[8]}, {id:'alert',icon:'alert',title:'设备提醒',value:'测试通知'}];
  for(const last of activities){
    await page.evaluate(async raw=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,raw);},{...dashboard,activities:last?[weather,last]:[weather]});
    const edge=await page.locator('.glass-panel').evaluate(panel=>{
      const last=panel.querySelector('.activity-list').lastElementChild;
      return {padding:parseFloat(getComputedStyle(panel).paddingBottom),tail:panel.getBoundingClientRect().bottom-last.getBoundingClientRect().bottom,overflow:panel.scrollHeight>panel.clientHeight+2};
    });
    expect(edge.padding).toBeLessThanOrEqual(4.1);
    expect(edge.tail).toBeGreaterThanOrEqual(4);
    expect(edge.tail).toBeLessThanOrEqual(5.1);
    expect(edge.overflow).toBe(false);
  }
});

test('hourly-to-daily gap is compact while the six slots and existing date fonts remain unchanged',async({page})=>{
  await page.setViewportSize({width:360,height:950});
  await setup(page,{...fixture,width:360,activities:[weather]});
  await page.goto('http://wallboard.test/?desktop=1');
  const row=page.locator('[data-key="weather"]');
  await expect(row.locator('.weather-hour')).toHaveCount(6);
  const geometry=await row.evaluate(n=>{
    const hourly=n.querySelector('.weather-hourly'),daily=n.querySelector('.weather-strip');
    const probability=hourly.querySelector('.weather-hour:last-child [data-field="rain"]');
    return {gap:daily.getBoundingClientRect().top-hourly.getBoundingClientRect().bottom,visibleContentGap:daily.getBoundingClientRect().top-probability.getBoundingClientRect().bottom,hours:[...hourly.children].map(h=>({font:getComputedStyle(h.querySelector('[data-field="hour"]')).fontSize,icon:h.querySelector('svg').getBoundingClientRect().width})),days:[...daily.children].map(d=>({font:getComputedStyle(d.querySelector('[data-field="day"]')).fontSize,height:d.getBoundingClientRect().height}))};
  });
  expect(geometry.gap).toBeGreaterThanOrEqual(3);
  expect(geometry.gap).toBeLessThanOrEqual(5);
  expect(geometry.visibleContentGap).toBeLessThanOrEqual(7);
  for(const h of geometry.hours){expect(h.font).toBe('12px');expect(h.icon).toBe(20);}
  for(const d of geometry.days){expect(d.font).toBe('12px');expect(d.height).toBe(42);}
});

test('the real mobile browser retains its separate comfortable panel padding',async({page})=>{
  await page.setViewportSize({width:375,height:950});
  await setup(page,{...fixture,width:300,activities:[weather]});
  await page.goto('http://wallboard.test/');
  await expect(page.locator('.weather-hour')).toHaveCount(6);
  expect(await page.locator('.glass-panel').evaluate(n=>parseFloat(getComputedStyle(n).paddingBottom))).toBe(16);
});

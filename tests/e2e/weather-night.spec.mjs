import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));

test('night moon renders with the existing icon sizes and negative temperatures remain readable',async({page})=>{
  await page.setViewportSize({width:300,height:1000});
  const forecast_hours=['现在','次日0时','次日2时','次日4时','次日6时','次日8时'].map((label,i)=>({at:`2026-10-${i?12:11}T${i?String((i-1)*2).padStart(2,'0'):22}:00:00+08:00`,label,icon:i<5?'weather-moon':'weather-sunny',condition:'晴',temperature:'-5°',rain:''}));
  const forecast_days=Array.from({length:5},(_,i)=>({date:`2026-10-${11+i}`,label:['今天','周一','周二','周三','周四'][i],icon:'weather-cloudy',condition:'多云',low:'-8°',high:'2°',rain:'',detail:'白天多云'}));
  const raw={...fixture,width:300,activities:[{id:'weather',icon:'weather-moon',value:'-5° · 晴',detail:'',forecast_hours,forecast_days}]};
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:raw});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  const row=page.locator('[data-key="weather"]');
  await expect(row.locator('.activity-icon use')).toHaveAttribute('href','#icon-weather-moon');
  await expect(page.locator('#icon-weather-moon path')).toHaveCount(1);
  await expect(row.locator('.weather-hour')).toHaveCount(6);
  await expect(row.locator('.weather-hour [data-field="temperature"]')).toHaveText(Array(6).fill('-5°'));
  await expect(row.locator('.weather-day [data-field="low"]')).toHaveText(Array(5).fill('-8°'));
  const geometry=await row.evaluate(n=>({overflow:n.scrollWidth>n.clientWidth+1,current:n.querySelector('.activity-icon svg').getBoundingClientRect().width,hours:[...n.querySelectorAll('.weather-hour')].map(h=>({width:h.querySelector('svg').getBoundingClientRect().width,href:h.querySelector('use').getAttribute('href')})),days:[...n.querySelectorAll('.weather-day')].map(d=>({height:d.getBoundingClientRect().height,font:getComputedStyle(d.querySelector('[data-field="day"]')).fontSize}))}));
  expect(geometry.overflow).toBe(false);expect(geometry.current).toBeCloseTo(22.4,1);
  for(const [i,h] of geometry.hours.entries()){expect(h.width).toBe(20);expect(h.href).toBe(i<5?'#icon-weather-moon':'#icon-weather-sunny');}
  for(const day of geometry.days){expect(day.height).toBe(42);expect(day.font).toBe('12px');}
});

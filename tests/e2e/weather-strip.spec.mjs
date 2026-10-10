import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const days=[
  {date:'2026-10-10',label:'今天',icon:'weather-sunny',condition:'晴',high:'32°',low:'24°',rain:'',detail:'白天晴 · 夜间少云'},
  {date:'2026-10-11',label:'周日',icon:'weather-cloudy',condition:'少云',high:'33°',low:'25°',rain:'<1%',detail:'白天少云 · 夜间少云 · 白天降水<1%'},
  {date:'2026-10-12',label:'周一',icon:'weather-rain',condition:'阵雨',high:'29°',low:'23°',rain:'65%',detail:'白天少云 · 夜间阵雨 · 夜间降水65%'},
  {date:'2026-10-13',label:'周二',icon:'weather-rain',condition:'小雨',high:'28°',low:'22°',rain:'80%',detail:'白天小雨 · 夜间小雨 · 白天降水80%'},
  {date:'2026-10-14',label:'周三',icon:'weather-cloudy',condition:'多云',high:'30°',low:'23°',rain:'20%',hazard:'强风6级',detail:'白天多云 · 预报强风6级'},
];

for(const width of [300,360]) {
  test(`five-day weather at ${width}px preserves warnings and other rows without clipping`,async({page})=>{
    await page.setViewportSize({width:600,height:1000});
    await page.addInitScript(()=>{
      window.hostSizes=[];
      window.chrome={webview:{postMessage(message){window.hostSizes.push(message);},addEventListener(){}}};
    });
    const dashboard={...fixture,width,activities:fixture.activities.map(a=>a.id==='weather'?{...a,value:'32° · 晴',detail:'',forecast_days:days}:a)};
    await page.route('http://wallboard.test/**',async route=>{
      const path=new URL(route.request().url()).pathname;
      if(path==='/api/dashboard')return route.fulfill({json:dashboard});
      if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
      const file=path==='/'?'index.html':path.slice(1);
      return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.png')?'image/png':'text/html'});
    });
    await page.goto('http://wallboard.test/?desktop=1');
    const current=page.locator('[data-key="weather"]');
    await expect(current.locator('.weather-day')).toHaveCount(5);
    await expect(page.locator('[data-list="activities"] > .activity-row')).toHaveCount(4);
    await expect(current.locator('.weather-temperature')).toHaveText('32°');
    const sizes=await current.locator('.weather-temperature').evaluate(n=>({temperature:parseFloat(getComputedStyle(n).fontSize),normal:parseFloat(getComputedStyle(document.querySelector('[data-key="cpu"] [data-field="value"]')).fontSize)}));
    expect(sizes.temperature).toBeGreaterThan(sizes.normal*1.5);
    await expect(current.locator('[data-field="detail"]')).toBeHidden();
    await expect(current.locator('.weather-day').first().locator('[data-field="rain"]')).toBeHidden();
    await expect(current.locator('.weather-day').nth(1)).toContainText('<1%');
    await expect.poll(()=>page.evaluate(()=>window.hostSizes.findLast(message=>message.ready)?.width)).toBe(width);
    const compactHeight=await page.evaluate(()=>window.hostSizes.findLast(message=>message.ready).height);
    await current.locator('.weather-day').nth(2).click();
    await expect(current.locator('.weather-day-detail')).toContainText('夜间降水65%');
    await expect.poll(()=>page.evaluate(()=>window.hostSizes.findLast(message=>message.ready)?.height)).toBeGreaterThan(compactHeight);
    await current.locator('.weather-day').nth(2).click();
    await expect.poll(()=>page.evaluate(()=>window.hostSizes.findLast(message=>message.ready)?.height)).toBe(compactHeight);
    await current.locator('.weather-day').nth(2).click();
    const warning={...dashboard,activities:dashboard.activities.map(a=>a.id==='weather'?{...a,value:'28° · 晴间多云',tone:'bad',value_tone:'neutral',detail:'暴雨橙色预警\n雷电黄色预警\n约半小时后可能有雨',detail_parts:[{text:'暴雨橙色预警',color:'orange'},{text:'\n',color:''},{text:'雷电黄色预警',color:'yellow'},{text:'\n约半小时后可能有雨',color:''}]}:a)};
    await page.evaluate(async value=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,value);},warning);
    await expect(current.locator('[data-warning-color="orange"]')).toHaveCSS('color','rgb(255, 173, 85)');
    await expect(current.locator('[data-warning-color="yellow"]')).toHaveCSS('color','rgb(245, 211, 93)');
    await expect(current.locator('[data-field="detail"]')).toContainText('约半小时后可能有雨');
    await expect(current.locator('.weather-condition')).toHaveText('晴间多云');
    await expect(current.locator('.weather-day').nth(2)).toHaveAttribute('aria-pressed','true');
    const bounds=await current.evaluate(row=>{
      const panel=row.closest('.glass-panel').getBoundingClientRect();
      const nodes=[...row.querySelectorAll('.weather-day,[data-field="detail"],.weather-day-detail,.weather-condition')];
      return nodes.map(n=>{const r=n.getBoundingClientRect();return {left:r.left,right:r.right,scrollWidth:n.scrollWidth,width:r.width,scrollHeight:n.scrollHeight,height:r.height,panelLeft:panel.left,panelRight:panel.right};});
    });
    for(const r of bounds){expect(r.left).toBeGreaterThanOrEqual(r.panelLeft);expect(r.right).toBeLessThanOrEqual(r.panelRight+1);expect(r.scrollWidth).toBeLessThanOrEqual(r.width+1);expect(r.scrollHeight).toBeLessThanOrEqual(r.height+1);}
    await expect(page.locator('[data-list="activities"] > .activity-row')).toHaveText([/28°.*晴间多云/s,/风扇/,/Plex/,/SMART/]);
    await page.evaluate(async value=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,value);}, {...fixture,width});
    await expect(current.locator('.weather-strip')).toBeHidden();
    await expect(page.locator('[data-key="weather:forecast:0"]')).toBeVisible();
    await expect(current.locator('.weather-temperature')).toHaveText('29°');
    expect(await current.locator('.weather-temperature').evaluate(n=>parseFloat(getComputedStyle(n).fontSize))).toBeGreaterThan(sizes.normal*1.5);
  });
}

import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

const root=new URL('../..',import.meta.url).pathname;
const fixture=JSON.parse(await readFile(join(root,'tests/e2e/fixture.json'),'utf8'));
const days=[
  {date:'2026-10-10',label:'今天',icon:'weather-sunny',condition:'晴',high:'32°',low:'24°',rain:'',hazard:'强紫外线',detail:'白天晴 · 夜间少云 · 预报紫外线很强(9)'},
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
    await expect(current.locator('.weather-day').first()).not.toContainText('强紫外线');
    await expect(current.locator('.weather-day').last()).not.toContainText('强风');
    await expect(current.locator('.weather-range')).toHaveCount(5);
    await expect(current.locator('.weather-now')).toHaveCount(1);
    const dayLayout=await current.locator('.weather-day').evaluateAll(rows=>rows.map(row=>{
      const rect=selector=>row.querySelector(selector).getBoundingClientRect();
      const day=row.querySelector('[data-field="day"]'),low=row.querySelector('[data-field="low"]');
      const alpha=n=>Number(getComputedStyle(n).color.match(/[\d.]+/g)?.[3] ?? 1);
      const rain=row.querySelector('[data-field="rain"]');
      return {row:row.getBoundingClientRect().toJSON(),day:rect('[data-field="day"]').toJSON(),low:rect('[data-field="low"]').toJSON(),track:rect('.weather-range-track').toJSON(),high:rect('[data-field="high"]').toJSON(),icon:rect('.weather-day-icon svg').toJSON(),rain:rect('[data-field="rain"]').toJSON(),hasRain:!rain.hidden,rainSize:parseFloat(getComputedStyle(rain).fontSize),dateWeight:Number(getComputedStyle(day).fontWeight),dateSize:parseFloat(getComputedStyle(day).fontSize),dateAlpha:alpha(day),lowAlpha:alpha(low)};
    }));
    for(let i=0;i<dayLayout.length;i++){
      const {row,day,low,track,high,icon,rain,hasRain,rainSize,dateWeight,dateSize,dateAlpha,lowAlpha}=dayLayout[i];
      expect(row.height).toBeGreaterThanOrEqual(40);
      expect(row.height).toBeLessThanOrEqual(42.5);
      expect(icon.width).toBeGreaterThanOrEqual(19);
      expect(icon.width).toBeLessThanOrEqual(21);
      expect(icon.top).toBeGreaterThanOrEqual(row.top);
      expect(icon.bottom).toBeLessThanOrEqual(row.bottom);
      expect(dateWeight).toBeGreaterThanOrEqual(500);
      expect(dateSize).toBeGreaterThanOrEqual(13);
      expect(dateAlpha).toBeGreaterThanOrEqual(0.82);
      expect(dateAlpha-lowAlpha).toBeGreaterThanOrEqual(0.16);
      if(hasRain){expect(rain.top).toBeGreaterThanOrEqual(icon.bottom);expect(rain.bottom).toBeLessThanOrEqual(row.bottom);expect(rainSize).toBeGreaterThanOrEqual(11);}
      if(i)expect(row.top).toBeGreaterThanOrEqual(dayLayout[i-1].row.bottom-1);
      expect(Math.abs(row.height-dayLayout[0].row.height)).toBeLessThan(1);
      expect(day.right).toBeLessThanOrEqual(low.left);
      expect(low.right).toBeLessThanOrEqual(track.left);
      expect(track.right).toBeLessThanOrEqual(high.left);
      expect(Math.abs((day.top+day.bottom)/2-(high.top+high.bottom)/2)).toBeLessThan(2);
    }
    await current.locator('.weather-day').first().click();
    await expect(current.locator('.weather-day-detail')).toContainText('紫外线很强(9)');
    await current.locator('.weather-day').first().click();
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
    const header=await current.evaluate(row=>{
      const value=row.querySelector('[data-field="value"]').getBoundingClientRect();
      const detail=row.querySelector('[data-field="detail"]').getBoundingClientRect();
      const firstDay=row.querySelector('.weather-day').getBoundingClientRect();
      return {valueRight:value.right,detailLeft:detail.left,detailBottom:detail.bottom,dayTop:firstDay.top};
    });
    expect(header.detailLeft).toBeGreaterThanOrEqual(header.valueRight);
    expect(header.detailBottom).toBeLessThanOrEqual(header.dayTop);
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

test('daily columns fit supported decimal Fahrenheit and keep invalid range labels aligned',async({page})=>{
  const temperatures=[['68°F','86°F'],['-20.5°F','100.5°F'],['70.5°F','99.5°F'],['100.5°F','100.5°F'],['90.5°F','85.5°F']];
  const forecast=days.map((day,i)=>({...day,low:temperatures[i][0],high:temperatures[i][1],detail:'白天晴 · 夜间多云'}));
  const dashboard={...fixture,width:300,activities:fixture.activities.map(a=>a.id==='weather'?{...a,value:'77°F · 晴',detail:'',forecast_days:forecast}:a)};
  await page.route('http://wallboard.test/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/dashboard')return route.fulfill({json:dashboard});
    if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
    const file=path==='/'?'index.html':path.slice(1);
    return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
  });
  await page.goto('http://wallboard.test/?desktop=1');
  const current=page.locator('[data-key="weather"]');
  await expect(current.locator('.weather-day')).toHaveCount(5);
  await expect(current.locator('.weather-day').last().locator('.weather-range-track')).toBeHidden();
  const geometry=await current.locator('.weather-day').evaluateAll(rows=>rows.map(row=>{
    const rect=s=>row.querySelector(s).getBoundingClientRect().toJSON();
    return {row:row.getBoundingClientRect().toJSON(),low:rect('[data-field="low"]'),high:rect('[data-field="high"]'),track:rect('.weather-range-track'),bar:rect('.weather-range'),visible:!row.querySelector('.weather-range-track').hidden,lowOverflow:row.querySelector('[data-field="low"]').scrollWidth>row.querySelector('[data-field="low"]').clientWidth+1,highOverflow:row.querySelector('[data-field="high"]').scrollWidth>row.querySelector('[data-field="high"]').clientWidth+1};
  }));
  for(const r of geometry){
    expect(r.high.right).toBeCloseTo(geometry[0].high.right,0);
    expect(r.high.right).toBeLessThanOrEqual(r.row.right+1);
    expect(r.lowOverflow).toBe(false);expect(r.highOverflow).toBe(false);
    if(r.visible){expect(r.track.width).toBeGreaterThan(25);expect(r.low.right).toBeLessThanOrEqual(r.track.left);expect(r.bar.left).toBeGreaterThanOrEqual(r.track.left);expect(r.bar.right).toBeLessThanOrEqual(r.track.right+0.2);}
  }
  await current.locator('.weather-day').last().click();
  await expect(current.locator('.weather-day-detail')).toContainText('强风');
  const missingCurrent={...dashboard,activities:dashboard.activities.map(a=>a.id==='weather'?{...a,value:'不可用',detail:'天气暂不可用'}:a)};
  await page.evaluate(async value=>{const {renderDashboard}=await import('/view.js');renderDashboard(document,value);},missingCurrent);
  await expect(current.locator('.weather-now')).toHaveCount(0);
});

test('touch forecasts retain comfortable targets while keeping the smaller icons',async({browser})=>{
  const context=await browser.newContext({hasTouch:true,viewport:{width:375,height:900}});
  try{
    const page=await context.newPage();
    const dashboard={...fixture,width:300,activities:fixture.activities.map(a=>a.id==='weather'?{...a,forecast_days:days}:a)};
    await page.route('http://wallboard.test/**',async route=>{
      const path=new URL(route.request().url()).pathname;
      if(path==='/api/dashboard')return route.fulfill({json:dashboard});
      if(path==='/api/setup/status')return route.fulfill({json:{setup_required:false}});
      const file=path==='/'?'index.html':path.slice(1);
      return route.fulfill({body:await readFile(join(root,'web',file)),contentType:file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html'});
    });
    await page.goto('http://wallboard.test/?desktop=1');
    expect(await page.evaluate(()=>matchMedia('(pointer: coarse)').matches)).toBe(true);
    await expect(page.locator('.weather-day')).toHaveCount(5);
    const rows=await page.locator('.weather-day').evaluateAll(nodes=>nodes.map(n=>({height:n.getBoundingClientRect().height,icon:n.querySelector('svg').getBoundingClientRect().width})));
    for(const row of rows){expect(row.height).toBeGreaterThanOrEqual(44);expect(row.icon).toBeLessThanOrEqual(21);}
  }finally{await context.close();}
});

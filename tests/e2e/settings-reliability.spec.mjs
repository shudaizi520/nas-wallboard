import {test, expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {extname} from 'node:path';

const base='http://settings.test';
const initial={revision:'one',width:360,widgets:[{id:'plex-1',definition_id:'plex',integration_id:'plex-main',enabled:true,order:0,config:{limit:3}}]};
const catalog=[{id:'plex',integration_type:'plex',placement:'activity',label:'Plex 播放',defaults:{limit:3},fields:[{key:'limit',kind:'integer',label:'数量'}]},{id:'cpu',integration_type:'truenas',placement:'metric',label:'处理器',defaults:{}}];
async function harness(page,{layoutFailure=false,holdSave=false,conflict=false}={}) {
 let stored=structuredClone(initial), release, sent, layoutGets=0, reauth=0, saves=0;
 const wait=holdSave ? new Promise(resolve=>{release=resolve}) : Promise.resolve();
 await page.route(`${base}/**`,async route=>{
  const path=new URL(route.request().url()).pathname;
  const json=(body,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)});
  if(path==='/api/auth/session') return json({csrf:'test',username:'admin'});
  if(path==='/api/manage/layout') {
   if(route.request().method()==='PUT') {sent=route.request().postDataJSON();saves++;await wait;if(conflict&&saves===1){stored={...stored,width:420,revision:'two'};return json({error:'layout_conflict'},409);}stored={...sent,revision:'saved'};}
   else {layoutGets++;if(layoutFailure)return json({error:'failed'},500);}
   return json({catalog,sources:[{id:'plex-main',type:'plex',enabled:true},{id:'truenas-main',type:'truenas',enabled:true}],layout:stored});
  }
  if(path==='/api/manage/integrations') return json({catalog:[{id:'plex',metadata:{name:'Plex',category:'媒体'},fields:[]}],instances:[{id:'plex-main',type:'plex',enabled:true,config:{},secrets:{}}]});
  // A concurrent source addition arrives with this integration mutation notification.
  if(path==='/api/manage/integrations/plex-main/disable') {stored={...stored,revision:'two',widgets:[...stored.widgets,{id:'cpu-1',definition_id:'cpu',integration_id:'truenas-main',enabled:true,order:1,config:{}}]};return json({enabled:false});}
  if(path==='/api/manage/integrations/plex-main'&&route.request().method()==='DELETE'){stored={...stored,revision:'removed',widgets:[]};return route.fulfill({status:204});}
  if(path==='/api/manage/reauth') {reauth++;return json({error:'invalid_credentials'},401);}
  if(path.startsWith('/api/')) return json({disabled:true});
  const file=path==='/manage' ? 'manage.html' : path.slice(1);
  try {return route.fulfill({contentType:extname(file)==='.js'?'text/javascript':extname(file)==='.css'?'text/css':'text/html',body:await readFile(new URL(`../../web/${file}`,import.meta.url))});}catch{return route.fulfill({status:404,body:''});}
 });
 await page.goto(`${base}/manage`);
 return {release:()=>release?.(),sent:()=>sent,gets:()=>layoutGets,reauth:()=>reauth};
}
test('layout failure still initializes account recovery controls',async({page})=>{
 const h=await harness(page,{layoutFailure:true});
 await page.locator('[data-page-target="settings-page"]').click();
 await page.locator('details').filter({has:page.locator('#backup-form')}).locator('summary').click();
 await page.locator('#backup-form [name="administrator"]').fill('correct password');
 await page.locator('#backup-form [name="backup"]').fill('long backup password');
 await page.locator('#backup-form button[type="submit"]').click();
 await expect.poll(h.reauth).toBe(1);
});
test('pending layout save locks every editor control and retains submitted edits',async({page})=>{
 const h=await harness(page,{holdSave:true});
 await page.locator('[data-page-target="desktop-page"]').click();
 await page.locator('#width').fill('500');
 await page.locator('#save').click();
 await expect(page.locator('#width')).toBeDisabled();
 await expect(page.locator('#layout-items input[name="limit"]')).toBeDisabled();
 await expect(page.locator('#reset-layout')).toBeDisabled();
 h.release();
 await expect(page.locator('#width')).toBeEnabled();
 await expect(page.locator('#width')).toHaveValue('500');
});
test('integration mutation synchronizes layout while retaining unsaved width',async({page})=>{
 const h=await harness(page);
 await page.locator('[data-page-target="desktop-page"]').click();
 await page.locator('#width').fill('500');
 await page.locator('[data-page-target="integrations-page"]').click();
 const previousGets=h.gets();
 await page.getByRole('button',{name:'停用',exact:true}).click();
 await expect.poll(h.gets).toBeGreaterThan(previousGets);
 await page.locator('[data-page-target="desktop-page"]').click();
 await expect(page.locator('#width')).toHaveValue('500');
 await page.locator('#save').click();
 await expect.poll(()=>h.sent()?.widgets.length).toBe(2);
 await expect.poll(()=>h.sent()?.revision).toBe('two');
});
test('stale-save conflict preserves draft and offers explicit resynchronization',async({page})=>{
 const h=await harness(page,{conflict:true});
 await page.locator('[data-page-target="desktop-page"]').click();
 await page.locator('#width').fill('500');
 await page.locator('#save').click();
 await expect(page.locator('#desktop-page #status')).toContainText('草稿已保留');
 await expect(page.locator('#width')).toHaveValue('500');
 await expect(page.locator('#width')).toBeEnabled();
 await page.locator('[data-page-target="settings-page"]').click();
 await page.locator('[data-page-target="desktop-page"]').click();
 await expect(page.locator('#width')).toHaveValue('500');
 await page.locator('#save').click();
 await expect.poll(()=>h.sent()?.revision).toBe('two');
});
test('deleted integration removes its widget while retaining unsaved width',async({page})=>{
 const h=await harness(page);
 await page.locator('[data-page-target="desktop-page"]').click();
 await page.locator('#width').fill('500');
 await page.locator('[data-page-target="integrations-page"]').click();
 page.once('dialog',dialog=>dialog.accept());
 const previousGets=h.gets();
 await page.getByRole('button',{name:'删除',exact:true}).click();
 await expect.poll(h.gets).toBeGreaterThan(previousGets);
 await page.locator('[data-page-target="desktop-page"]').click();
 await expect(page.locator('#layout-items .layout-item')).toHaveCount(0);
 await expect(page.locator('#width')).toHaveValue('500');
 await page.locator('#save').click();
 await expect.poll(()=>h.sent()?.widgets.length).toBe(0);
});

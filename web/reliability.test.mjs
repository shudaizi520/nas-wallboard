import test from 'node:test';
import assert from 'node:assert/strict';
import {validateRestore} from './settings.js';
import {entityOptions} from './integrations.js';
import {collectionRows} from './overview.js';

test('restore requires file, separate passwords and explicit overwrite phrase', () => {
 assert.ok(Object.keys(validateRestore({})).length >= 3);
 assert.deepEqual(validateRestore({administrator:'private', backup:'backup password here', file:{name:'backup.age',size:100}, confirmation:'恢复 NAS WALLBOARD'}),{});
 assert.ok(validateRestore({administrator:'private',backup:'backup password here',file:{size:21<<20},confirmation:'恢复 NAS WALLBOARD'}).file);
});
test('entity choices separate fans from real wattage sensors and keep manual entries',()=>{
 const list=[{id:'fan.room',name:'客厅',kind:'fan'},{id:'sensor.power',name:'NAS',kind:'power'}];
 assert.deepEqual(entityOptions(list,'fan','fan.manual').map(x=>x.id),['fan.manual','fan.room']);
 assert.deepEqual(entityOptions(list,'power','').map(x=>x.id),['sensor.power']);
});
test('collection details show last real success and never substitute response time',()=>{
 const rows=collectionRows([{type:'plex',healthy:false,running:true,message:'采集失败',last_success:'2026-09-30T00:00:00Z'}]);
 assert.equal(rows[0].name,'Plex');assert.equal(rows[0].status,'采集失败');assert.match(rows[0].lastSuccess,/2026/);
 assert.equal(collectionRows([{type:'plex',running:true,healthy:false}])[0].lastSuccess,'尚无成功采集');
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeDashboard} from './view.js';

test('structured weather reminders preserve whole UV text and future rain as separate messages',()=>{
  const weather_reminders=[{text:'紫外线很强 · 指数8',pinned:false},{text:'18–19时可能有雨',pinned:false}];
  const got=normalizeDashboard({activities:[{id:'weather',detail:weather_reminders.map(m=>m.text).join('\n'),weather_reminders}]}).activities[0];
  assert.deepEqual(got.weatherReminders,weather_reminders);
});

test('mismatched or malformed reminder payload cannot hide plain warning text',()=>{
  for(const weather_reminders of [[],[{text:'晴',pinned:false}],[{text:'暴雨预警',pinned:'false'}],[{text:null,pinned:false}]]){
    const got=normalizeDashboard({activities:[{id:'weather',detail:'暴雨预警',weather_reminders}]}).activities[0];
    assert.equal(got.detail,'暴雨预警');assert.equal(got.weatherReminders,undefined);
  }
});

test('warning message colors require exact matching text and use only allowed color names',()=>{
  const got=normalizeDashboard({activities:[{id:'weather',detail:'暴雨橙色预警',weather_reminders:[{text:'暴雨橙色预警',pinned:true,detail_parts:[{text:'暴雨橙色预警',color:'orange'}]}]}]}).activities[0];
  assert.deepEqual(got.weatherReminders,[{text:'暴雨橙色预警',pinned:true,detailParts:[{text:'暴雨橙色预警',color:'orange'}]}]);
});

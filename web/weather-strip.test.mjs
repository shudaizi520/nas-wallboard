import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeDashboard} from './view.js';

test('structured forecast replaces only legacy forecast rows and keeps configured activity order',()=>{
  const days=[{date:'2026-10-10',label:'今天',icon:'weather-sunny',condition:'晴',high:'32°',low:'24°',rain:''}];
  const got=normalizeDashboard({activities:[{id:'plex',title:'Plex'},{id:'weather',value:'32° · 晴',forecast_days:days},{id:'weather:forecast:0',value:'多云'},{id:'fan',title:'风扇'}]});
  assert.deepEqual(got.activities.map(a=>a.id),['plex','weather','fan']);
  assert.deepEqual(got.activities[1].forecastDays,[{...days[0],detail:'',hazard:''}]);
});

test('missing or malformed forecast payload retains old rows rather than hiding them',()=>{
  for(const forecast_days of [null,[],[{label:'今天',high:'32°'}]]) {
    const got=normalizeDashboard({activities:[{id:'weather',forecast_days},{id:'weather:forecast:0'}]});
    assert.deepEqual(got.activities.map(a=>a.id),['weather','weather:forecast:0']);
  }
});

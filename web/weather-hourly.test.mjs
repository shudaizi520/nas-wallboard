import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeDashboard} from './view.js';

test('hourly slots retain exact nonconsecutive labels, probabilities and safe icons',()=>{
  const forecast_hours=[{at:'2026-10-11T12:00:00+08:00',label:'现在',icon:'weather-sunny',condition:'晴',temperature:'32°',rain:''},{at:'2026-10-11T18:00:00+08:00',label:'18时',icon:'weather-rain',condition:'小雨',temperature:'28°',rain:'65%'}];
  const got=normalizeDashboard({activities:[{id:'weather',forecast_hours}]}).activities[0];
  assert.deepEqual(got.forecastHours,forecast_hours);
});

test('malformed or duplicate hourly slots are not rendered and limit remains six',()=>{
  const good={at:'2026-10-11T18:00:00+08:00',label:'18时',icon:'weather-rain',condition:'小雨',temperature:'28°',rain:'65%'};
  const forecast_hours=[good,good,{...good,at:'bad'},{...good,at:'2026-10-11T19:00:00+08:00',temperature:'NaN°'},...Array.from({length:8},(_,i)=>({...good,at:`2026-10-12T0${i}:00:00+08:00`,label:`次日${i}时`,rain:''}))];
  const got=normalizeDashboard({activities:[{id:'weather',forecast_hours}]}).activities[0];
  assert.equal(got.forecastHours?.length,6);
  assert.equal(new Set(got.forecastHours.map(h=>h.at)).size,6);
});

test('validated moon icons survive normalization for current and hourly clear skies',()=>{
  const hour={at:'2026-10-12T00:00:00+08:00',label:'次日0时',icon:'weather-moon',condition:'晴',temperature:'-5°',rain:''};
  const got=normalizeDashboard({activities:[{id:'weather',icon:'weather-moon',value:'-5° · 晴',forecast_hours:[hour]}]}).activities[0];
  assert.equal(got.icon,'weather-moon');assert.equal(got.forecastHours[0].icon,'weather-moon');
});

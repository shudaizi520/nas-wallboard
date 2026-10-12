import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeDashboard} from './view.js';

test('new weather classes survive normalization in current, hourly and daily views',()=>{
  const icons=['weather-overcast','weather-heavy-rain','weather-hail','weather-freezing-rain','weather-sleet','weather-heavy-snow','weather-haze','weather-dust','weather-hot','weather-cold','weather-unknown'];
  for(const icon of icons){
    const raw={activities:[{id:'weather',icon,value:'20° · 原始天气',detail:'',forecast_hours:[{at:'2026-10-13T02:00:00+08:00',label:'次日2时',icon,condition:'原始天气',temperature:'20°',rain:'7%'}],forecast_days:[{date:'2026-10-13',label:'周二',icon,condition:'原始天气',low:'20°',high:'30°',rain:'7%',detail:'白天原始天气'}]}]};
    const w=normalizeDashboard(raw).activities[0];
    assert.equal(w.icon,icon);
    assert.equal(w.forecastHours[0].icon,icon);
    assert.equal(w.forecastDays[0].icon,icon);
  }
});

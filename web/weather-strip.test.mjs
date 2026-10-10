import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeDashboard} from './view.js';
import * as view from './view.js';

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

test('daily temperature bars share a scale and only today gets a current-temperature marker',()=>{
  assert.equal(typeof view.weatherTemperatureRanges,'function');
  const days=[
    {label:'今天',low:'26°',high:'32°'},
    {label:'周日',low:'24°',high:'34°'},
    {label:'周一',low:'25°',high:'33°'},
  ];
  assert.deepEqual(view.weatherTemperatureRanges(days,'29° · 晴'),[
    {start:20,width:60,current:50},
    {start:0,width:100,current:null},
    {start:10,width:80,current:null},
  ]);
});

test('temperature bars support negative Celsius and Fahrenheit without mixing units',()=>{
  assert.equal(typeof view.weatherTemperatureRanges,'function');
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'-10°',high:'0°'},{label:'周日',low:'-5°',high:'10°'}],'-5° 多云'),[
    {start:0,width:50,current:25},{start:25,width:75,current:null},
  ]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'68°F',high:'86°F'}],'77°F · 晴'),[{start:0,width:100,current:50}]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'68°F',high:'86°F'}],'25° · 晴'),[{start:0,width:100,current:null}]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'20°',high:'30°'},{label:'周日',low:'68°F',high:'86°F'}],'25° · 晴'),[null,null]);
});

test('invalid ranges and out-of-forecast observations never invent a marker or bar',()=>{
  assert.equal(typeof view.weatherTemperatureRanges,'function');
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'26°',high:'32°'}],'34° · 晴'),[{start:0,width:100,current:null}]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'26°',high:'32°'}],'天气不可用'),[{start:0,width:100,current:null}]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'26°',high:'26°'}],'26° · 晴'),[{start:50,width:0,current:50}]);
  assert.deepEqual(view.weatherTemperatureRanges([{label:'今天',low:'32°',high:'26°'},{low:'NaN°',high:'32°'},{low:'20°',high:'30°F'}],'29° · 晴'),[null,null,null]);
  assert.deepEqual(view.weatherTemperatureRanges([], '29° · 晴'),[]);
});

test('separate forecast hazards remain in details without repeating already explained risks',()=>{
  assert.equal(typeof view.weatherForecastDetail,'function');
  assert.equal(view.weatherForecastDetail({condition:'晴',detail:'白天晴 · 夜间多云',hazard:'强风'}),'白天晴 · 夜间多云 · 预报强风');
  assert.equal(view.weatherForecastDetail({condition:'晴',detail:'白天晴 · 预报紫外线很强(9)',hazard:'强风/强紫外线'}),'白天晴 · 预报紫外线很强(9) · 预报强风');
  assert.equal(view.weatherForecastDetail({condition:'晴',detail:'白天晴 · 阵风20米/秒 · 紫外线很强(9)',hazard:'强风/强紫外线'}),'白天晴 · 阵风20米/秒 · 紫外线很强(9)');
  assert.equal(view.weatherForecastDetail({condition:'晴',detail:'',hazard:'强紫外线'}),'晴 · 预报强紫外线');
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {MASKED_SECRET, orderedCatalog, validateField} from './integrations.js';

test('schema validation covers every built-in field kind', () => {
  const cases = [
    [{kind:'url',label:'地址',required:true}, 'http://user:pass@nas.local'],
    [{kind:'integer',label:'数量',minimum:1,maximum:5}, 9],
    [{kind:'duration',label:'间隔'}, 'soon'],
    [{kind:'boolean',label:'开关'}, 'true'],
    [{kind:'select',label:'模式',options:[{value:'a'}]}, 'b'],
    [{kind:'entity_id',label:'实体'}, 'living room'],
  ];
  for (const [field, value] of cases) assert.notEqual(validateField(field, value), '');
  assert.equal(validateField({kind:'text',label:'名称',required:true}, '客厅'), '');
  assert.equal(validateField({kind:'secret',label:'令牌',required:true}, MASKED_SECRET), '');
});

test('catalog ordering is deterministic by category, name, and id', () => {
  const values = [{id:'z',metadata:{category:'媒体',name:'Plex'}},{id:'a',metadata:{category:'存储',name:'磁盘'}},{id:'b',metadata:{category:'媒体',name:'Jellyfin'}}];
  assert.deepEqual(orderedCatalog(values).map((item) => item.id), ['a','b','z']);
  assert.deepEqual(values.map((item) => item.id), ['z','a','b']);
});

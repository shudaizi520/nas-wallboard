import test from 'node:test';
import assert from 'node:assert/strict';
import {MASKED_SECRET, orderedCatalog, partitionFields, validateField, integrationRows, runIntegrationRowAction} from './integrations.js';

test('advanced integration fields are separated without changing their order', () => {
  const fields = [
    {key: 'url', label: '服务地址'},
    {key: 'call_timeout', label: '请求超时', advanced: true},
    {key: 'token', label: '访问令牌'},
    {key: 'insecure_skip_verify', label: '自签名证书', advanced: true},
  ];
  assert.deepEqual(partitionFields(fields), {
    common: [fields[0], fields[2]],
    advanced: [fields[1], fields[3]],
  });
});

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

test('integration rows expose status and valid actions without secret values', () => {
  const rows = integrationRows({
    catalog: [
      {id: 'truenas', metadata: {category: '系统', name: 'TrueNAS', description: '系统数据来源', required: true}, fields: [{key: 'url', kind: 'url'}, {key: 'api_key', kind: 'secret', configured: true}]},
      {id: 'plex', metadata: {category: '媒体', name: 'Plex', description: '媒体播放状态', required: false}, fields: [{key: 'token', kind: 'secret', configured: false}]},
    ],
    instances: [{id: 'truenas-main', type: 'truenas', enabled: true, config: {url: 'wss://nas.local/api/current'}, secrets: {api_key: true}}],
    health: [{instance_id: 'truenas-main', type: 'truenas', running: true, healthy: true, message: '运行中', updated_at: '2026-09-28T10:00:00Z'}],
  });

  assert.equal(rows[0].id, 'plex');
  assert.equal('description' in rows[0], false);
  assert.equal(rows[0].configured, '未配置');
  assert.equal(rows[0].enabled, '未启用');
  assert.deepEqual(rows[0].actions, ['configure']);
  assert.equal(rows[1].id, 'truenas');
  assert.equal('description' in rows[1], false);
  assert.equal(rows[1].configured, 'wss://nas.local/api/current');
  assert.equal(rows[1].enabled, '已启用');
  assert.equal(rows[1].error, '');
  assert.deepEqual(rows[1].actions, ['configure']);
  assert.equal(JSON.stringify(rows).includes('api-key-secret'), false);
});

test('optional configured integration exposes enable and remove actions', () => {
  const [row] = integrationRows({
    catalog: [{id: 'plex', metadata: {category: '媒体', name: 'Plex', description: '媒体播放状态', required: false}, fields: []}],
    instances: [{id: 'plex-main', type: 'plex', enabled: false, config: {url: 'http://plex.local:32400'}, secrets: {token: true}}],
    health: [{instance_id: 'plex-main', type: 'plex', running: false, healthy: false, message: '已停止', updated_at: '2026-09-28T10:00:00Z'}],
  });
  assert.equal(row.enabled, '已停用');
  assert.equal(row.error, '');
  assert.deepEqual(row.actions, ['configure', 'enable', 'remove']);
});

test('only an enabled unhealthy integration exposes a visible error', () => {
  const [row] = integrationRows({
    catalog: [{id: 'plex', metadata: {category: '媒体', name: 'Plex', description: '媒体播放状态', required: false}, fields: []}],
    instances: [{id: 'plex-main', type: 'plex', enabled: true, config: {}, secrets: {}}],
    health: [{instance_id: 'plex-main', healthy: false, message: '连接超时'}],
  });
  assert.equal(row.error, '连接超时');
  assert.equal(row.tone, 'bad');
});

test('row actions prevent duplicate requests, report failures, and restore buttons', async () => {
  const buttons = [{disabled: false}, {disabled: false}];
  const status = {textContent: '', dataset: {}};
  let calls = 0;
  let rejectTask;
  const task = () => {
    calls += 1;
    return new Promise((resolve, reject) => { rejectTask = reject; });
  };
  const first = runIntegrationRowAction({buttons, status, task, reload: async () => {}});
  const second = runIntegrationRowAction({buttons, status, task, reload: async () => {}});
  assert.equal(calls, 1);
  assert.equal(buttons.every((button) => button.disabled), true);
  assert.equal(await second, false);
  rejectTask(new Error('offline'));
  assert.equal(await first, false);
  assert.equal(status.textContent, '操作失败，请重试');
  assert.equal(status.dataset.tone, 'bad');
  assert.equal(buttons.every((button) => !button.disabled), true);
});

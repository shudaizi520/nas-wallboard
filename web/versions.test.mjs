import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeReleaseVersion, clientVersionFromSearch, clientVersionStatus, managementAddress, updateMessage} from './versions.js';

test('release identities remove commit metadata but reject unknown and unsafe values', () => {
  assert.equal(normalizeReleaseVersion('1.0.10+abc123'), 'v1.0.10');
  assert.equal(normalizeReleaseVersion('v1.0.10-beta.2+abc'), 'v1.0.10-beta.2');
  for (const value of [undefined, null, '', 'dev', 'unknown', 'v01.0.10', 'v1.0', '<img src=x>', ' v1.0.10 ']) {
    assert.equal(normalizeReleaseVersion(value), null);
  }
});

test('ordinary browsers do not infer the installed client version from the NAS server', () => {
  const status = clientVersionStatus('v1.0.10', null);
  assert.equal(status.value, '已安装版本未知 · 可下载 v1.0.10');
  assert.equal(status.tone, 'neutral');
  assert.match(status.detail, /无法读取.*已安装/);
});

test('a client older than the bundled release gets replacement guidance rather than refresh guidance', () => {
  const status = clientVersionStatus('v1.0.10', 'v1.0.9');
  assert.equal(status.value, '当前客户端 v1.0.9 · 可下载 v1.0.10');
  assert.equal(status.tone, 'warn');
  assert.match(status.detail, /重新下载.*替换/);
  assert.match(clientVersionStatus('v1.0.10', 'v1.0.10').detail, /刷新/);
  assert.equal(clientVersionStatus('v1.0.9', 'v1.0.10').tone, 'neutral');
  assert.doesNotMatch(clientVersionStatus('v1.0.9', 'v1.0.10').detail, /重新下载.*替换/);
  assert.equal(clientVersionStatus('v1.0.10', 'v1.0.10-beta.2').tone, 'warn');
  assert.equal(clientVersionStatus('v1.0.10-beta.10', 'v1.0.10-beta.2').tone, 'warn');
  assert.equal(clientVersionStatus('v1.0.10-beta-z', 'v1.0.10-beta-a').tone, 'warn');
});

test('only a validated self-reported client release survives the login path', () => {
  assert.equal(clientVersionFromSearch('?client_version=v1.0.10'), 'v1.0.10');
  assert.equal(clientVersionFromSearch('?client_version=%3Cscript%3E'), null);
  assert.equal(managementAddress('/login', '?client_version=v1.0.10&next=https://evil.example'), '/login?client_version=v1.0.10');
  assert.equal(managementAddress('/manage', '?client_version=unknown'), '/manage');
  assert.equal(managementAddress('/manage', '?client_version=v1.0.10%0aevil'), '/manage');
});

test('update checks distinguish a disabled or unavailable check from verified latest status', () => {
  assert.equal(updateMessage({disabled:true,available:false}), '未启用更新检查，无法确认最新版本');
  assert.equal(updateMessage({error:'暂时无法检查更新'}), '暂时无法检查更新');
  assert.match(updateMessage({available:true,latest:'v1.0.10'}), /NAS 服务端.*v1.0.10/);
  assert.match(updateMessage({available:true,latest:'v1.0.10'}), /客户端.*单独/);
  assert.equal(updateMessage({available:false,latest:'v1.0.10'}), 'NAS 服务端已是最新版本；桌面客户端请看下方版本信息');
});

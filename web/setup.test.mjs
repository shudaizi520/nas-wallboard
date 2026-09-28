import test from 'node:test';
import assert from 'node:assert/strict';
import {
  initialSetupState,
  reduceSetup,
  validateCurrentStep,
  normalizeTrueNASURL,
  buildCompletionPayload,
  redactSecretsAfterSubmit,
} from './setup.js';

test('compatibility acknowledgement is required before leaving welcome', () => {
  const state = initialSetupState({setup_required: true, migration: false});
  assert.deepEqual(validateCurrentStep(state), {compatibility: '请先确认 TrueNAS SCALE 版本'});
  const acknowledged = reduceSetup(state, {type: 'acknowledge', value: true});
  assert.deepEqual(validateCurrentStep(acknowledged), {});
  assert.equal(reduceSetup(acknowledged, {type: 'next'}).step, 1);
});

test('TrueNAS URLs are normalized to the JSON-RPC WebSocket endpoint', () => {
  assert.equal(normalizeTrueNASURL('https://nas.local'), 'wss://nas.local/api/current');
  assert.equal(normalizeTrueNASURL('http://10.0.0.99/'), 'ws://10.0.0.99/api/current');
  assert.equal(normalizeTrueNASURL('wss://nas.local/api/current'), 'wss://nas.local/api/current');
  assert.equal(normalizeTrueNASURL('nas.local:443'), 'wss://nas.local/api/current');
  assert.throws(() => normalizeTrueNASURL('https://user:secret@nas.local'));
  assert.throws(() => normalizeTrueNASURL('ftp://nas.local'));
});

test('wizard navigation is bounded and preserves safe state', () => {
  let state = initialSetupState({setup_required: true});
  state = reduceSetup(state, {type: 'acknowledge', value: true});
  for (let index = 0; index < 10; index += 1) state = reduceSetup(state, {type: 'next'});
  assert.equal(state.step, 5);
  state = reduceSetup(state, {type: 'field', name: 'username', value: 'wallboard'});
  assert.equal(reduceSetup(state, {type: 'back'}).step, 4);
  for (let index = 0; index < 10; index += 1) state = reduceSetup(state, {type: 'back'});
  assert.equal(state.step, 0);
  assert.equal(state.candidate.username, 'wallboard');
});

test('candidate and administrator password validation is field-specific', () => {
  let state = {...initialSetupState({setup_required: true}), step: 1, compatibility: true};
  assert.deepEqual(Object.keys(validateCurrentStep(state)).sort(), ['apiKey', 'url', 'username']);
  state = {
    ...state,
    step: 3,
    password: 'correct horse battery staple',
    passwordConfirmation: 'different password value',
  };
  assert.deepEqual(validateCurrentStep(state), {passwordConfirmation: '两次输入的密码不一致'});
});

test('submission payload is complete while secrets are immediately discarded', () => {
  const state = {
    ...initialSetupState({setup_required: true}),
    candidate: {url: 'wss://nas.local/api/current', username: 'wallboard', apiKey: 'api-secret', insecureSkipVerify: true},
    password: 'correct horse battery staple',
    passwordConfirmation: 'correct horse battery staple',
  };
  assert.deepEqual(buildCompletionPayload(state), {
    url: 'wss://nas.local/api/current', username: 'wallboard', api_key: 'api-secret', insecure_skip_verify: true,
    password: 'correct horse battery staple', password_confirmation: 'correct horse battery staple', use_imported: false,
  });
  const redacted = redactSecretsAfterSubmit(state);
  assert.equal(redacted.candidate.apiKey, '');
  assert.equal(redacted.password, '');
  assert.equal(redacted.passwordConfirmation, '');
  assert.equal(redacted.candidate.username, 'wallboard');
});

test('server errors and final review data are represented without secrets', () => {
  let state = initialSetupState({setup_required: true});
  state = reduceSetup(state, {type: 'serverErrors', errors: {url: '无法连接', apiKey: '无权限'}});
  assert.deepEqual(state.errors, {url: '无法连接', apiKey: '无权限'});
  state = reduceSetup({...state, step: 2}, {type: 'probeSucceeded', result: {
    ok: true, version: '25.10.1', pools: [{id: '1', name: 'tank'}], disks: [{id: 'sda', name: 'sda'}],
    interfaces: [{id: 'eno1', name: 'eno1'}], apps: [{id: 'plex', name: 'Plex'}], permissions: [],
  }});
  state = reduceSetup({...state, step: 4}, {type: 'next'});
  assert.equal(state.step, 5);
  assert.equal(state.probe.version, '25.10.1');
  assert.equal(JSON.stringify(state).includes('api-secret'), false);
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {initialLoginState, reduceLogin, validateLogin, buildLoginPayload} from './login.js';

test('login requires a password and builds the minimal request', () => {
  assert.deepEqual(validateLogin(initialLoginState()), {password: '请输入管理员密码'});
  const ready = reduceLogin(initialLoginState(), {type: 'password', value: 'correct horse battery staple'});
  assert.deepEqual(validateLogin(ready), {});
  assert.deepEqual(buildLoginPayload(ready), {password: 'correct horse battery staple'});
});

test('login reducer clears credentials on both success and failure', () => {
  const ready = reduceLogin(initialLoginState(), {type: 'password', value: 'temporary secret'});
  const pending = reduceLogin(ready, {type: 'submit'});
  assert.equal(pending.password, '');
  assert.equal(pending.pending, true);
  const failed = reduceLogin(pending, {type: 'failure', message: '密码不正确'});
  assert.equal(failed.pending, false);
  assert.equal(failed.error, '密码不正确');
  assert.equal(failed.password, '');
});

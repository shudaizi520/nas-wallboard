import test from 'node:test';
import assert from 'node:assert/strict';
import {initialLoginState, reduceLogin, validateLogin, buildLoginPayload} from './login.js';

test('login requires username and password and builds the complete request', () => {
	assert.deepEqual(validateLogin(initialLoginState()), {username: '请输入管理员用户名', password: '请输入管理员密码'});
	let ready = reduceLogin(initialLoginState(), {type: 'username', value: 'admin'});
	ready = reduceLogin(ready, {type: 'password', value: 'correct horse battery staple'});
	assert.deepEqual(validateLogin(ready), {});
	assert.deepEqual(buildLoginPayload(ready), {username: 'admin', password: 'correct horse battery staple'});
});

test('failed login preserves username and clears password', () => {
	let ready = reduceLogin(initialLoginState(), {type: 'username', value: 'Owner'});
	ready = reduceLogin(ready, {type: 'password', value: 'temporary secret'});
	const pending = reduceLogin(ready, {type: 'submit'});
	assert.equal(pending.username, 'Owner');
	assert.equal(pending.password, '');
	assert.equal(pending.pending, true);
	const failed = reduceLogin(pending, {type: 'failure', message: '用户名或密码不正确'});
	assert.equal(failed.pending, false);
	assert.equal(failed.error, '用户名或密码不正确');
	assert.equal(failed.username, 'Owner');
	assert.equal(failed.password, '');
});

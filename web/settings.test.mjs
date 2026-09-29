import test from 'node:test';
import assert from 'node:assert/strict';
import {bindExclusiveDisclosures, validateBackup, validatePasswordChange, validateReset, validateUsernameChange, redactUsernameChange, settingsSectionDefinitions} from './settings.js';

function fakeDisclosure(open = false) {
	const listeners = new Set();
	let submissions = 0;
	let resets = 0;
	const form = {reset() { resets += 1; }};
	return {
		open,
		addEventListener(name, listener) { if (name === 'toggle') listeners.add(listener); },
		removeEventListener(name, listener) { if (name === 'toggle') listeners.delete(listener); },
		querySelectorAll(selector) { return selector === 'form' ? [form] : []; },
		dispatchToggle() { for (const listener of listeners) listener(); },
		requestSubmit() { submissions += 1; },
		get submissions() { return submissions; },
		get resets() { return resets; },
	};
}

test('settings disclosures keep one operation open and cleanup removes listeners', () => {
	const username = fakeDisclosure(true);
	const backup = fakeDisclosure(false);
	const root = {querySelectorAll: () => [username, backup]};
	const cleanup = bindExclusiveDisclosures(root);

	backup.open = true;
	backup.dispatchToggle();
	assert.equal(username.open, false);
	assert.equal(backup.open, true);
	assert.equal(username.resets, 1);
	assert.equal(username.submissions + backup.submissions, 0);

	backup.open = false;
	backup.dispatchToggle();
	assert.equal(backup.resets, 1);

	cleanup();
	username.open = true;
	backup.dispatchToggle();
	assert.equal(username.open, true);
});

test('settings validates encrypted backup and password change without retaining values', () => {
  assert.deepEqual(validateBackup({administrator: '', backup: 'short'}), {administrator: '请输入管理员密码', backup: '备份密码至少 12 个字符'});
  assert.deepEqual(validatePasswordChange({current: 'old', replacement: 'long enough but mismatch', confirmation: 'different'}), {confirmation: '两次输入的新密码不一致'});
  assert.deepEqual(validatePasswordChange({current: 'old', replacement: '这是一个足够长的新密码呀', confirmation: '这是一个足够长的新密码呀'}), {});
});

test('factory reset requires the exact visible phrase and administrator password', () => {
  assert.deepEqual(validateReset({administrator: '', confirmation: '删除'}), {administrator: '请输入管理员密码', confirmation: '请输入完整确认文字'});
  assert.deepEqual(validateReset({administrator: 'secret', confirmation: '删除 NAS WALLBOARD'}), {});
});

test('username change validates identifiers and clears the current password', () => {
	assert.deepEqual(validateUsernameChange({username: 'bad/name', currentPassword: ''}), {
		username: '用户名只能包含中英文字母、数字、点、下划线和连字符',
		currentPassword: '请输入当前密码',
	});
	assert.deepEqual(validateUsernameChange({username: '管理员_01', currentPassword: 'temporary secret'}), {});
	assert.deepEqual(redactUsernameChange({username: 'Owner', currentPassword: 'temporary secret'}), {username: 'Owner', currentPassword: ''});
});

test('settings sections keep account, diagnostics, backup, and reset order with scoped danger', () => {
	const sections = settingsSectionDefinitions();
	assert.deepEqual(sections.map((section) => section.id), ['administrator', 'diagnostics', 'encrypted-backup', 'factory-reset']);
	assert.deepEqual(sections.filter((section) => section.danger).map((section) => section.id), ['factory-reset']);
	assert.equal(sections[0].primary, 'username');
	assert.equal(sections[0].secondary, 'password');
});

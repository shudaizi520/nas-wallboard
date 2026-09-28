import test from 'node:test';
import assert from 'node:assert/strict';
import {validateBackup, validatePasswordChange, validateReset} from './settings.js';

test('settings validates encrypted backup and password change without retaining values', () => {
  assert.deepEqual(validateBackup({administrator: '', backup: 'short'}), {administrator: '请输入管理员密码', backup: '备份密码至少 12 个字符'});
  assert.deepEqual(validatePasswordChange({current: 'old', replacement: 'long enough but mismatch', confirmation: 'different'}), {confirmation: '两次输入的新密码不一致'});
  assert.deepEqual(validatePasswordChange({current: 'old', replacement: '这是一个足够长的新密码呀', confirmation: '这是一个足够长的新密码呀'}), {});
});

test('factory reset requires the exact visible phrase and administrator password', () => {
  assert.deepEqual(validateReset({administrator: '', confirmation: '删除'}), {administrator: '请输入管理员密码', confirmation: '请输入完整确认文字'});
  assert.deepEqual(validateReset({administrator: 'secret', confirmation: '删除 NAS WALLBOARD'}), {});
});

import assert from 'node:assert/strict';
import test from 'node:test';

import {createSettingsSection, setInlineStatus} from './manage-ui.js';

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.attributes = new Map();
    this.className = '';
    this.dataset = {};
    this.textContent = '';
  }
  append(...children) { this.children.push(...children); }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  hasAttribute(name) { return this.attributes.has(name); }
  querySelector(selector) {
    const matches = (node) => selector.startsWith('.') ? node.className.split(/\s+/).includes(selector.slice(1)) : node.tagName === selector.toUpperCase();
    for (const child of this.children) {
      if (matches(child)) return child;
      const nested = child.querySelector?.(selector);
      if (nested) return nested;
    }
    return null;
  }
}

const documentRef = {createElement: (tagName) => new FakeElement(tagName)};

test('settings section creates one labelled semantic row without inline styles', () => {
  const main = documentRef.createElement('div');
  main.textContent = '主要内容';
  const aside = documentRef.createElement('div');
  aside.textContent = '次要操作';
  const section = createSettingsSection(documentRef, {
    className: 'account-section', title: '管理员账户', description: '修改本地管理身份。', main, aside,
  });

  assert.equal(section.tagName, 'SECTION');
  assert.equal(section.className, 'settings-section account-section');
  const heading = section.querySelector('h2');
  assert.equal(heading.textContent, '管理员账户');
  assert.equal(section.getAttribute('aria-labelledby'), heading.getAttribute('id'));
  assert.equal(section.querySelector('.section-label').querySelector('p').textContent, '修改本地管理身份。');
  const content = section.querySelector('.section-content');
  assert.ok(content);
  assert.equal(section.children[1], content);
  assert.equal(content.querySelector('.section-main').children[0], main);
  assert.equal(content.querySelector('.section-aside').children[0], aside);
  const stack = [section];
  while (stack.length) {
    const node = stack.pop();
    assert.equal(node.hasAttribute('style'), false);
    stack.push(...node.children);
  }
});

test('settings section omits an empty aside and status tone is replaced', () => {
  const main = documentRef.createElement('span');
  const section = createSettingsSection(documentRef, {title: '诊断', description: '下载脱敏信息。', main});
  assert.equal(section.querySelector('.section-aside'), null);

  const status = documentRef.createElement('p');
  setInlineStatus(status, '正在保存', 'pending');
  assert.equal(status.textContent, '正在保存');
  assert.equal(status.dataset.tone, 'pending');
  setInlineStatus(status, '保存完成', 'good');
  assert.deepEqual(status.dataset, {tone: 'good'});
});

test('settings section omits explanatory copy when no description is needed', () => {
  const main = documentRef.createElement('span');
  const section = createSettingsSection(documentRef, {title: '处理器', main});
  assert.equal(section.querySelector('.section-label').querySelector('p'), null);
});

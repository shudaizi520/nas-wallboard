import test from 'node:test';
import assert from 'node:assert/strict';
import * as layout from './layout.js';
import {candidateFromForm} from './integrations.js';

const widget = (id, order=0) => ({id, definition_id:id, integration_id:`${id}-source`, enabled:true, order, config:{limit:3}});
test('source synchronization rebases only changed draft fields and keeps new server widgets', () => {
 const saved = {revision:'one', width:360, widgets:[widget('a'), widget('b',1)]};
 const draft = structuredClone(saved); draft.width=500; draft.widgets[0].config.limit=6;
 const incoming = {revision:'two',width:420,widgets:[{...widget('a'),config:{limit:4,label:'new'}},widget('c',1)]};
 assert.equal(typeof layout.mergeLayoutDraft, 'function');
 const merged = layout.mergeLayoutDraft(saved,draft,incoming,[{id:'a-source'},{id:'c-source'}]);
 assert.equal(merged.width,500); assert.equal(merged.revision,'two');
 assert.deepEqual(merged.widgets.map(item=>item.id),['a','c']);
 assert.deepEqual(merged.widgets[0].config,{limit:6,label:'new'});
 assert.deepEqual(incoming.widgets[0].config,{limit:4,label:'new'});
});
test('clean synchronization adopts server width and draft widget additions survive', () => {
 const saved={revision:'one',width:360,widgets:[widget('a')]};
 const draft={...structuredClone(saved),widgets:[widget('a'),widget('b',1)]};
 const incoming={revision:'two',width:420,widgets:[widget('a'),widget('c',1)]};
 assert.equal(typeof layout.mergeLayoutDraft, 'function');
 const merged=layout.mergeLayoutDraft(saved,draft,incoming,[{id:'a-source'},{id:'b-source'},{id:'c-source'}]);
 assert.equal(merged.width,420);
 assert.deepEqual(merged.widgets.map(item=>[item.id,item.order]),[['a',0],['c',1],['b',2]]);
});
test('saved integration candidate carries identity for masked credential testing', () => {
 const definition={id:'plex',fields:[{key:'token',kind:'secret',label:'token',required:true}]};
 const form={elements:{namedItem:()=>({value:'********'})}};
 assert.equal(candidateFromForm(definition,form,{id:'plex-main',secrets:{token:true}}).candidate.instance_id,'plex-main');
});

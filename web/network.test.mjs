import test from 'node:test';
import assert from 'node:assert/strict';
import * as layout from './layout.js';

test('network choices retain an unavailable saved interface and real zero samples', () => {
  assert.equal(typeof layout.networkInterfaceOptions, 'function');
  const options = layout.networkInterfaceOptions([
    {identifier:'eth0',available:true,rx_bps:0,tx_bps:0},
    {identifier:'br0',available:false},
  ], 'gone0');
  assert.deepEqual(options.map(option => [option.value, option.available]), [['',true],['br0',false],['eth0',true],['gone0',false]]);
  assert.match(options[0].label, /自动/);
  assert.match(options[3].label, /不可用/);
});

test('network choice freshness never alters the saved selection', () => {
  assert.equal(typeof layout.networkInterfaceOptions, 'function');
  const saved = {width:375,widgets:[{id:'network-1',definition_id:'network',enabled:true,order:0,config:{interface:'eth0'}}]};
  const options = layout.networkInterfaceOptions([], saved.widgets[0].config.interface);
  assert.equal(options[1].value, 'eth0');
  assert.equal(saved.widgets[0].config.interface, 'eth0');
  const edited = layout.updateWidget(saved,'network-1',{config:{interface:'br0'}});
  assert.equal(edited.widgets[0].config.interface,'br0');
  assert.equal(saved.widgets[0].config.interface,'eth0');
});

test('network control saves explicit, auto and manual values without selecting a fallback', () => {
  assert.equal(typeof layout.networkInterfaceInput, 'function');
  const element = tag => ({tag,children:[],value:'',hidden:false,listeners:{},append(...nodes){this.children.push(...nodes);},setAttribute(){},addEventListener(name,listener){this.listeners[name]=listener;}});
  const changes=[];
  const control=layout.networkInterfaceInput({createElement:element},{key:'interface',label:'NAS 网络接口',help:'NAS 接口流量'},'gone0',[{identifier:'eth0',available:true}],value=>changes.push(value));
  const select=control.children.find(node=>node.tag==='select');
  const manual=control.children.find(node=>node.tag==='input');
  assert.equal(select.value,'gone0');
  assert.equal(manual.hidden,true);
  assert.deepEqual(changes,[]);
  select.value='eth0'; select.listeners.change();
  select.value=''; select.listeners.change();
  select.value='/manual'; select.listeners.change();
  assert.equal(manual.hidden,false);
  assert.deepEqual(changes,['eth0','']);
  manual.value='veth-peer.100'; manual.listeners.input();
  assert.deepEqual(changes,['eth0','','veth-peer.100']);
});

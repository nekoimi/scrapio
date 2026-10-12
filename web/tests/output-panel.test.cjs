// Compile and exercise the actual output panel without a server or browser.
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const vue = require('vue'), ts = require('typescript');
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');
const tick = async () => { for (let i = 0; i < 8; i++) { await Promise.resolve(); await vue.nextTick(); } };
const field = key => ({ field_key: key, name: key, type: 'string', required: true });
const table = () => ({ table_id: '1', name: '列表表', schema_version: 1, schema: { fields: [{ field_key: 'list_id', name: 'list_id', type: 'string', nullable: false, multiple: false }], unique_key: ['list_id'] } });
const binding = () => ({ table_id: '1', schema_version: 1, step_id: 'records', stage: 'list', mapping: [{ source_field_key: 'list_id', target_field_key: 'list_id' }], update_policy: 'update', empty_policy: 'preserve_existing' });
function panel({ bound = false, stage = 'detail', fields = [field('detail_url'), field('title')], api = {}, confirm = () => true } = {}) {
 const filename = path.join(__dirname, '../src/views/app/output-panel.vue');
 const { descriptor, errors } = parse(fs.readFileSync(filename, 'utf8'), { filename });
 assert.equal(errors.length, 0);
 const script = compileScript(descriptor, { id: 'output-test' });
 const template = compileTemplate({ source: descriptor.template.content, filename, id: 'output-test', compilerOptions: { bindingMetadata: script.bindings } });
 assert.deepEqual(template.errors, []);
 const output = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
 const module = { exports: {} }, storage = new Map(), events = [];
 vm.runInNewContext(output, { module, exports: module.exports,
  require: name => name === 'vue' ? vue : name === './api' ? { appApi: { tables: async () => ({ items: [table()], next_cursor: '' }), table: async () => table(), ...api } } : { confirmAction: async (...args) => confirm(...args) },
  sessionStorage: { getItem: key => storage.get(key) || null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) },
  crypto: require('node:crypto'), setInterval, clearInterval });
 const props = vue.reactive({ draft: { id: 'collector', name: 'Movies', revision: 1, definition: { steps: [{ step_id: 'records', type: 'record_set', config: { fields: [field('list_id')], detail_fields: fields } }], ...(bound ? { output: binding() } : {}) } },
  capture: { capture_id: 'capture-detail', content_hash: 'hash', status: 'succeeded' }, stepId: 'records', stage, blocked: false });
 const component = module.exports.default, setup = component.setup;
 let state;
 component.setup = (props, context) => { state = setup(props, context); return state; };
 component.render = () => vue.h('div');
 const node = () => ({ children: [] });
 const renderer = vue.createRenderer({ createElement: node, createText: node, createComment: node, insert: (child, parent) => parent.children.push(child), remove() {}, setElementText() {}, setText() {}, patchProp() {}, parentNode: () => null, nextSibling: () => null });
 const app = renderer.createApp({ render: () => vue.h(component, { ...props, onDraftSaved: value => events.push(value) }) });
 app.mount(node());
 return { props, storage, events, get state() { return state; }, unmount: () => app.unmount() };
}
test('missing unique key is explained before any preview request or pending state', async () => {
 let calls = 0;
 const h = panel({ api: { createOutputCheck: async () => { calls++; } } });
 try {
  await tick(); await h.state.generate();
  assert.match(h.state.configurationProblem.value, /尚未选择唯一键/);
  await h.state.runCheck();
  assert.equal(calls, 0); assert.equal(h.storage.size, 0); assert.equal(h.state.pendingKey.value, '');
  assert.match(h.state.message.value, /唯一键.*勾选/);
 } finally { h.unmount(); }
});
test('an empty detail rule explains that fields must be saved before configuring output', async () => {
 let calls = 0;
 const h = panel({ bound: true, fields: [], api: { createOutputCheck: async () => { calls++; } } });
 try {
  await tick(); await h.state.runCheck();
  assert.match(h.state.configurationProblem.value, /详情页尚无已保存.*添加并保存字段/);
  assert.equal(calls, 0); assert.equal(h.storage.size, 0);
 } finally { h.unmount(); }
});
test('an older server schema error is translated locally and clears the rejected request', async () => {
 const h = panel({ api: { createOutputCheck: async () => { throw { error: { code: 'INVALID_ARGUMENT', message: 'invalid output configuration: schema requires 1..30 fields and 1..5 unique-key fields' } }; } } });
 try {
  await tick(); await h.state.generate(); h.state.toggleKey('detail_url', true); await h.state.runCheck();
  assert.match(h.state.message.value, /保存字段或唯一键数量.*唯一键.*勾选/);
  assert.equal(h.storage.size, 0); assert.equal(h.state.pendingKey.value, '');
 } finally { h.unmount(); }
});
test('detail generation uses saved detail fields and does not silently guess a unique key', async () => {
 let input;
 const h = panel({ api: { createOutputCheck: async (_, value) => { input = value; return { ready: true }; } } });
 try {
  await tick(); await h.state.generate();
  assert.deepEqual(Array.from(h.state.schema.value.fields, f => f.field_key), ['detail_url', 'title']);
  assert.equal(h.state.schema.value.unique_key.length, 0);
  h.state.toggleKey('detail_url', true);
  assert.equal(h.state.configurationProblem.value, '');
  await h.state.runCheck();
  assert.equal(input.stage, 'detail'); assert.equal(input.step_id, 'records'); assert.equal(input.capture_id, 'capture-detail');
  assert.deepEqual(Array.from(input.proposed_table.schema.unique_key), ['detail_url']);
  assert.deepEqual(Array.from(input.mapping, m => m.source_field_key), ['detail_url', 'title']);
  assert.equal(h.storage.size, 0);
 } finally { h.unmount(); }
});
test('list binding remains intact in detail mode until an explicit output confirmation', async () => {
 const h = panel({ bound: true });
 try {
  await tick();
  assert.equal(h.state.bindingChanged.value, true);
  assert.match(h.state.configurationProblem.value, /list_id.*详情页/);
  await h.state.generate();
  assert.equal(h.state.tableId.value, '');
  assert.equal(h.props.draft.definition.output.stage, 'list');
  assert.equal(h.events.length, 0);
  assert.deepEqual(Array.from(h.state.schema.value.fields, f => f.field_key), ['detail_url', 'title']);
 } finally { h.unmount(); }
});
test('existing table can be explicitly remapped to a detail field', async () => {
 let input;
 const h = panel({ bound: true, api: { createOutputCheck: async (_, value) => { input = value; return { ready: true }; } } });
 try {
  await tick(); h.state.mapField('list_id', 'detail_url');
  assert.equal(h.state.configurationProblem.value, ''); await h.state.runCheck();
  assert.equal(input.table_id, '1'); assert.equal(input.proposed_table, undefined);
  assert.equal(input.mapping[0].source_field_key, 'detail_url'); assert.equal(input.stage, 'detail');
 } finally { h.unmount(); }
});
test('five-key limit and invalid scalar keys cannot dispatch a preview', async () => {
 let calls = 0;
 const h = panel({ fields: Array.from({ length: 6 }, (_, i) => field(`key${i}`)), api: { createOutputCheck: async () => { calls++; } } });
 try {
  await tick(); await h.state.generate();
  for (let i = 0; i < 6; i++) h.state.toggleKey(`key${i}`, true);
  assert.equal(h.state.schema.value.unique_key.length, 5);
  h.state.schema.value.unique_key.push('key5'); await h.state.runCheck();
  assert.match(h.state.message.value, /最多.*5/);
  h.state.schema.value.unique_key = ['key0']; h.state.schema.value.fields[0].multiple = true; await h.state.runCheck();
  assert.match(h.state.message.value, /单值/);
  h.state.schema.value.fields[0].multiple = false; h.state.schema.value.fields[0].type = 'json'; await h.state.runCheck();
  assert.match(h.state.message.value, /JSON/);
  assert.equal(calls, 0); assert.equal(h.storage.size, 0);
 } finally { h.unmount(); }
});
test('late preview for another role stays stale and cannot be confirmed', async () => {
 let resolve;
 const h = panel({ api: { createOutputCheck: (_, input) => new Promise(done => { resolve = () => done({ ready: true, collector_revision: 1, capture_id: 'capture-detail', content_hash: 'hash', config: input, expires_at: new Date(Date.now() + 60000).toISOString() }); }) } });
 try {
  await tick(); await h.state.generate(); h.state.toggleKey('detail_url', true);
  const pending = h.state.runCheck(); await tick();
  h.props.stage = 'list'; resolve(); await pending;
  assert.equal(h.state.stale.value, true); assert.match(h.state.message.value, /上下文变化/);
 } finally { h.unmount(); }
});
test('canceling replacement of a list binding does not save a detail binding', async () => {
 let question, calls = 0;
 const h = panel({ bound: true, confirm: value => { question = value; return false; }, api: { confirmOutput: async () => { calls++; } } });
 try {
  await tick(); h.state.mapField('list_id', 'detail_url');
  h.state.acceptCheck({ check_id: 'checked', ready: true, collector_revision: 1, capture_id: 'capture-detail', content_hash: 'hash', config: { step_id: 'records', stage: 'detail' }, expires_at: new Date(Date.now() + 60000).toISOString() });
  await h.state.confirm();
  assert.match(question, /列表.*替换为详情页/); assert.match(question, /原表和记录保留/);
  assert.equal(calls, 0); assert.equal(h.props.draft.definition.output.stage, 'list'); assert.equal(h.storage.size, 0);
 } finally { h.unmount(); }
});
test('changing role while confirming regeneration preserves the existing local draft', async () => {
 let resolve;
 const h = panel({ confirm: () => new Promise(done => { resolve = done; }) });
 try {
  await tick(); await h.state.generate(); h.state.tableName.value = 'Edited';
  const previous = JSON.stringify(h.state.schema.value), pending = h.state.generate();
  await tick(); h.props.stage = 'list'; resolve(true); await pending;
  assert.equal(JSON.stringify(h.state.schema.value), previous); assert.equal(h.state.tableName.value, 'Edited');
 } finally { h.unmount(); }
});

const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const vue = require('vue'), ts = require('typescript');
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');
const tick = async () => { for (let i = 0; i < 10; i++) { await Promise.resolve(); await vue.nextTick(); } };
const trial = (status = 'queued') => ({ trial_id: 'trial-original', collector_revision: 73, status, created_at: new Date(Date.now() - 60000).toISOString(), input: { mode: 'live' }, summary: {}, event_seq: 1 });
function panel(api = {}) {
 const filename = path.join(__dirname, '../src/views/app/trial-panel.vue');
 const { descriptor, errors } = parse(fs.readFileSync(filename, 'utf8'), { filename });
 assert.deepEqual(errors, []);
 const script = compileScript(descriptor, { id: 'trial-test' });
 assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename, id: 'trial-test', compilerOptions: { bindingMetadata: script.bindings } }).errors, []);
 const code = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
 const module = { exports: {} }, intervals = new Map(), waits = []; let timer = 0;
 vm.runInNewContext(code, { module, exports: module.exports, URL, AbortController,
  require: name => name === 'vue' ? vue : name === './api' ? { appApi: { trials: async () => ({ items: [trial()] }), samples: async () => ({ items: [], next_cursor: '' }), trialResults: async () => ({ items: [], next_cursor: '' }), trial: async () => trial(), streamTrial: async () => { throw new Error('stream disconnected'); }, ...api } } : { confirmAction: async () => true },
  sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} }, crypto: require('node:crypto'),
  setInterval: fn => { intervals.set(++timer, fn); return timer; }, clearInterval: id => intervals.delete(id), setTimeout: fn => waits.push(fn) });
 const component = module.exports.default, setup = component.setup; let state;
 component.setup = (props, context) => { state = setup(props, context); return state; }; component.render = () => vue.h('div');
 const node = () => ({ children: [] });
 const renderer = vue.createRenderer({ createElement: node, createText: node, createComment: node, insert: (child, parent) => parent.children.push(child), remove() {}, setElementText() {}, setText() {}, patchProp() {}, parentNode: () => null, nextSibling: () => null });
 const app = renderer.createApp(component, { draft: { id: 'collector', entry_url: 'https://example.test/list', revision: 74, definition: { steps: [] } }, blocked: false });
 app.mount(node());
 return { get state() { return state; }, intervals, waits, poll: async () => { for (const fn of intervals.values()) fn(); await tick(); }, unmount: () => app.unmount() };
}
test('stream failures keep polling the original trial without promising execution or creating another job', async () => {
 let reads = 0, creates = 0;
 const h = panel({ trial: async () => { reads++; return trial(); }, createTrial: async () => { creates++; } });
 try {
  await tick();
  for (let i = 0; i < 2; i++) { const wait = h.waits.shift(); assert.equal(typeof wait, 'function'); wait(); await tick(); }
  assert.match(h.state.streamNotice.value, /每 2 秒查询.*以当前状态为准/);
  assert.equal(h.state.message.value, ''); assert.equal(h.state.queueDelayed.value, true);
  assert.equal(h.intervals.size, 1); await h.poll();
  assert.equal(reads, 1); assert.equal(creates, 0); assert.equal(h.state.selected.value.trial_id, 'trial-original');
  assert.equal(h.state.selected.value.collector_revision, 73);
 } finally { h.unmount(); }
 assert.equal(h.intervals.size, 0);
});
test('queue warning and stream fallback notice disappear once polling observes completion', async () => {
 let results = 0;
 const h = panel({ trial: async () => trial('succeeded'), trialResults: async () => { results++; return { items: [], next_cursor: '' }; } });
 try {
  await tick(); assert.equal(h.state.queueDelayed.value, true);
  h.state.streamNotice.value = 'Disconnected'; await h.poll();
  assert.equal(h.state.selected.value.status, 'succeeded'); assert.equal(h.state.queueDelayed.value, false);
  assert.equal(h.state.streamNotice.value, ''); assert.equal(h.intervals.size, 0); assert.equal(results, 2);
 } finally { h.unmount(); }
});
test('fresh queued trials and running trials do not display a delayed queue warning', async () => {
 const h = panel();
 try {
  await tick(); h.state.selected.value.created_at = new Date().toISOString();
  assert.equal(h.state.queueDelayed.value, false);
  h.state.selected.value = trial('running'); assert.equal(h.state.queueDelayed.value, false);
 } finally { h.unmount(); }
});

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const vue = require('vue');
const ts = require('typescript');
const moduleSource = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../src/views/app/browser-frame.ts'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const exportsObject = {};
vm.runInNewContext(moduleSource, { exports: exportsObject, require: () => vue });
const { createBrowserFrame } = exportsObject;
const target = (pageStateID = 'page-1', sessionID = 'session-1') => ({ sessionID, pageStateID, url: '/frame', available: true });
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
const tick = async () => { await Promise.resolve(); await vue.nextTick(); await Promise.resolve(); };
function harness(overrides = {}) {
  const revoked = [], requests = [];
  const queue = createBrowserFrame({
    load: input => { const task = deferred(); requests.push({ ...task, input }); return task.promise; },
    decode: async () => ({ width: 1280, height: 800 }),
    createURL: blob => `image-${blob.id}`, revokeURL: url => revoked.push(url), ...overrides,
  });
  queue.setTarget(target());
  return { queue, requests, revoked };
}
test('old image stays visible through capture and decode; release only after replacement', async () => {
  const decode = deferred();
  let slow = false;
  const h = harness({ decode: async () => slow ? decode.promise : { width: 1280, height: 800 } });
  let task = h.queue.refresh(); await tick(); h.requests[0].resolve({ id: 1 }); await task;
  slow = true; task = h.queue.refresh(); await tick();
  assert.equal(h.queue.frame.value.objectURL, 'image-1'); assert.equal(h.queue.stale.value, true);
  h.requests[1].resolve({ id: 2 }); await tick();
  assert.equal(h.queue.frame.value.objectURL, 'image-1'); assert.equal(h.revoked.length, 0);
  decode.resolve({ width: 1280, height: 800 }); await task;
  assert.equal(h.queue.frame.value.objectURL, 'image-2'); assert.equal(h.queue.stale.value, false);
  assert.deepEqual(h.revoked, ['image-1']); h.queue.dispose();
  assert.deepEqual(h.revoked, ['image-1', 'image-2']);
});
test('repeated demand is coalesced and late captures cannot replace newer state', async () => {
  const h = harness(); const task = h.queue.refresh(); await tick();
  h.queue.setTarget(target('page-2')); h.queue.refresh(); h.queue.refresh(); h.queue.refresh();
  assert.equal(h.requests.length, 1);
  h.requests[0].resolve({ id: 'old' }); await tick();
  assert.equal(h.queue.frame.value, undefined); assert.equal(h.requests.length, 2);
  assert.equal(h.requests[1].input.pageStateID, 'page-2');
  h.requests[1].resolve({ id: 'new' }); await task;
  assert.equal(h.requests.length, 2); assert.equal(h.queue.frame.value.objectURL, 'image-new'); h.queue.dispose();
});
test('decode failure keeps old image marked stale and releases failed URL', async () => {
  let fail = false;
  const h = harness({ decode: async () => { if (fail) throw new Error('bad image'); return { width: 1, height: 1 }; } });
  let task = h.queue.refresh(); await tick(); h.requests[0].resolve({ id: 'good' }); await task;
  fail = true; task = h.queue.refresh(); await tick(); h.requests[1].resolve({ id: 'bad' }); await task;
  assert.equal(h.queue.frame.value.objectURL, 'image-good'); assert.equal(h.queue.stale.value, true);
  assert.match(h.queue.error.value, /保留上次截图/); assert.deepEqual(h.revoked, ['image-bad']); h.queue.dispose();
});
test('session switch during decoding discards old frame and releases all URLs', async () => {
  const decode = deferred(); const h = harness({ decode: () => decode.promise });
  const task = h.queue.refresh(); await tick(); h.requests[0].resolve({ id: 'old-session' }); await tick();
  h.queue.setTarget(target('page-1', 'session-2'));
  decode.resolve({ width: 1, height: 1 }); await task;
  assert.equal(h.queue.frame.value, undefined); assert.deepEqual(h.revoked, ['image-old-session']); h.queue.dispose();
});
test('disconnect preserves old image for viewing; in-flight success cannot reactivate it', async () => {
  const h = harness(); let task = h.queue.refresh(); await tick(); h.requests[0].resolve({ id: 'old' }); await task;
  task = h.queue.refresh(); await tick(); h.queue.setTarget({ ...target(), available: false });
  h.requests[1].resolve({ id: 'late' }); await task;
  assert.equal(h.queue.frame.value.objectURL, 'image-old'); assert.equal(h.queue.stale.value, true);
  h.queue.dispose(); assert.deepEqual(h.revoked, ['image-old']);
});
test('continuous changes have bounded capture attempts and stop with recovery guidance', async () => {
  const h = harness(); const task = h.queue.refresh();
  for (let i = 0; i < 3; i++) {
    await tick(); h.queue.setTarget(target(`page-${i + 2}`)); h.queue.refresh(); h.requests[i].resolve({ id: i });
  }
  await task;
  assert.equal(h.requests.length, 3); assert.equal(h.queue.busy.value, false); assert.equal(h.queue.stale.value, true);
  assert.match(h.queue.error.value, /持续更新/); h.queue.dispose();
});

function mountEditor(apiOverrides = {}) {
  const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');
  const filename = path.join(__dirname, '../src/views/app/collector-detail.vue');
  const { descriptor } = parse(fs.readFileSync(filename, 'utf8'), { filename });
  const script = compileScript(descriptor, { id: 'frame-editor-test' });
  assert.equal(compileTemplate({ source: descriptor.template.content, filename, id: 'frame-editor-test', compilerOptions: { bindingMetadata: script.bindings } }).errors.length, 0);
  const api = {
    collector: async () => ({ id: 'collector-1', name: '测试方案', entry_type: 'web', definition: { steps: [] }, revision: 1 }),
    browserFrame: async () => ({ id: 'capture' }),
    streamBrowserSession: () => new Promise(() => {}), ...apiOverrides,
  };
  const module = { exports: {} };
  const output = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText;
  vm.runInNewContext(output, { module, exports: module.exports,
    require: name => name === 'vue' ? vue : name === './api' ? { appApi: api }
      : name === './browser-frame' ? { createBrowserFrame: options => createBrowserFrame({ ...options, decode: async () => ({ width: 1280, height: 800 }), createURL: blob => `image-${blob.id}`, revokeURL: () => {} }) }
        : name === 'vue-router' ? { useRoute: () => ({ params: { id: 'collector-1' }, query: {} }), onBeforeRouteLeave: () => {}, onBeforeRouteUpdate: () => {} } : {},
    sessionStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    AbortController, queueMicrotask, setTimeout, clearTimeout, setInterval: () => 1, clearInterval: () => {},
  });
  const component = module.exports.default; let state;
  const setup = component.setup; component.setup = (props, context) => { state = setup(props, context); return state; }; component.render = () => vue.h('div');
  const node = () => ({ children: [] });
  const renderer = vue.createRenderer({ createElement: node, createText: node, createComment: node, insert: (child, parent) => parent.children.push(child),
    remove: () => {}, setElementText: () => {}, setText: () => {}, patchProp: () => {}, parentNode: () => null, nextSibling: () => null });
  const app = renderer.createApp(component); app.mount(node());
  return { state, unmount: () => app.unmount() };
}
const session = pageState => ({ session_id: 'session-1', page_state_id: pageState, frame_url: '/frame', available: true, status: 'ready', heartbeat_interval_seconds: 20 });
test('editor refuses image coordinates while stale or updating and retains same-page highlights', async () => {
  const h = mountEditor(); await tick();
  try {
    const s = h.state; await s.setBrowserSession(session('page-1'));
    s.frameImage.value = { naturalWidth: 1280, naturalHeight: 800, currentSrc: s.frameObjectUrl.value, getBoundingClientRect: () => ({ left: 0, top: 0 }), clientLeft: 0, clientTop: 0, clientWidth: 1280, clientHeight: 800 };
    s.updateFrameSize(); let clicked = 0;
    s.actionsPanel.value = { frameClick: () => clicked++, frameHover: () => clicked++ };
    const event = { currentTarget: s.frameImage.value, clientX: 5, clientY: 5 };
    s.prepareFrameClick(event); assert.equal(clicked, 1);
    s.setHighlights({ kind: 'selected', page_state_id: 'page-1', elements: [] });
    await s.updateBrowserFrame(session('page-1'));
    assert.ok(s.pageHighlights.value.selected); // No clear-selection path during replacement.
    s.frameQueue.invalidate(); s.prepareFrameClick(event); s.prepareFrameHover(event);
    assert.equal(clicked, 1); assert.match(s.frameStatus.value, /过期/);
    s.handleFrameError(); assert.match(s.frameError.value, /显示失败/);
  } finally { h.unmount(); }
});
test('late state read for manual refresh cannot roll back a newer SSE state', async () => {
  const response = deferred(); const captures = [];
  const h = mountEditor({ browserSession: () => response.promise, browserFrame: async (_, token) => { captures.push(token); return { id: token }; } }); await tick();
  try {
    const s = h.state; await s.setBrowserSession(session('page-1'));
    const pending = s.updateBrowserFrame(); await tick();
    await s.setBrowserSession(session('page-2')); response.resolve(session('page-1')); await pending;
    assert.equal(s.browserSession.value.page_state_id, 'page-2'); assert.equal(s.framePageStateID.value, 'page-2');
    assert.equal(captures.at(-1), 'page-2'); assert.equal(s.syncingFrame.value, false);
  } finally { h.unmount(); }
});

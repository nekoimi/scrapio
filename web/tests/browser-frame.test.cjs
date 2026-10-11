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
vm.runInNewContext(moduleSource, { exports: exportsObject, require: name => name === 'vue' ? vue : require('./workbench-loader.cjs') });
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
test('same-version requests share both capture and decode without discarding the in-flight image', async () => {
  const decode = deferred(); const h = harness({ decode: () => decode.promise });
  const first = h.queue.refresh(); await tick();
  const second = h.queue.refresh(); assert.equal(first, second);
  h.requests[0].resolve({ id: 'shared' }); await tick();
  const third = h.queue.refresh(); assert.equal(first, third); assert.equal(h.requests.length, 1);
  decode.resolve({ width: 1280, height: 800 }); await first;
  assert.equal(h.requests.length, 1); assert.equal(h.queue.frame.value.objectURL, 'image-shared'); assert.equal(h.queue.stale.value, false);
  h.queue.dispose();
});

test('explicit invalidation still replaces a same-page capture and rejects its late result', async () => {
  const h = harness(); const first = h.queue.refresh(); await tick();
  h.queue.invalidate(); h.queue.refresh(); h.requests[0].resolve({ id: 'invalidated' }); await tick();
  assert.equal(h.requests.length, 2); assert.equal(h.queue.frame.value, undefined);
  h.requests[1].resolve({ id: 'fresh' }); await first;
  assert.equal(h.queue.frame.value.objectURL, 'image-fresh'); h.queue.dispose();
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

function mountEditor(apiOverrides = {}, time) {
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
        : name === './browser-preview' ? require('./workbench-loader.cjs') : name === './browser-workbench' ? { ...require('./workbench-loader.cjs'), createAdaptiveRefresh: options => require('./workbench-loader.cjs').createAdaptiveRefresh({ ...options, clock: time }), createDelayedRefreshIndicator: options => require('./workbench-loader.cjs').createDelayedRefreshIndicator({ ...options, clock: time }) } : name === 'vue-router' ? { useRoute: () => ({ params: { id: 'collector-1' }, query: {} }), onBeforeRouteLeave: () => {}, onBeforeRouteUpdate: () => {} } : {},
    sessionStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    document: { visibilityState: 'visible', addEventListener() {}, removeEventListener() {} }, AbortController, queueMicrotask, setTimeout, clearTimeout, setInterval: () => 1, clearInterval: () => {},
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

test('live preview freezes before selection and never reuses streamed click coordinates', async () => {
  const stopped=deferred(); let streamSignal, captures=0, picked=0, activated=0;
  const h=mountEditor({capabilities:async()=>({supports_preview_stream:true}), browserSession:async()=>session('page-1'),
    browserFrame:async()=>{captures++; return {id:'frozen'};},
    streamBrowserPreview:(_,signal)=>{streamSignal=signal; return stopped.promise;}}); await tick();
  try {
    const s=h.state; s.dirty.value=false; await s.setBrowserSession(session('page-1')); await tick();
    assert.equal(s.previewRunning.value,true); assert.equal(captures,0);
    s.previewDisplay.frame.value={objectURL:'live-jpeg'};
    s.actionsPanel.value={frameClick:()=>picked++, activateSelection:()=>activated++};
    s.prepareFrameClick({clientX:10,clientY:10}); await tick();
    assert.equal(streamSignal.aborted,true); assert.equal(s.syncingFrame.value,true); assert.equal(picked,0); assert.equal(captures,0);
    stopped.resolve(); for (let i=0;i<20;i++) await tick();
    assert.equal(s.viewMode.value,'configure'); assert.equal(captures,1); assert.equal(activated,1); assert.equal(picked,0);
    assert.equal(s.frameObjectUrl.value,'image-frozen'); assert.equal(s.previewRunning.value,false);
  } finally {stopped.resolve();h.unmount();}
});

test('preview failure falls back to authoritative PNG and hidden tabs stop streaming', async () => {
  let signal; const h=mountEditor({capabilities:async()=>({supports_preview_stream:true}), browserSession:async()=>session('page-1'),
    streamBrowserPreview:(_,s)=>{signal=s; return Promise.reject(Object.assign(new Error('unsupported'),{code:'STREAM_MASK_REQUIRED'}));}}); await tick();
  try {
    h.state.dirty.value=false; await h.state.setBrowserSession(session('page-1')); for(let i=0;i<5;i++) await tick();
    assert.equal(h.state.previewFailed.value,true); assert.equal(h.state.showPreview.value,false); assert.match(h.state.previewNotice.value,/遮罩/);
    await h.state.updateBrowserFrame(); assert.equal(h.state.frameObjectUrl.value,'image-capture');
    h.state.tabVisible.value=false; await tick(); assert.equal(signal.aborted,true);
  } finally {h.unmount();}
});
test('editor refuses image coordinates while stale or updating and retains same-page highlights', async () => {
  const h = mountEditor(); await tick();
  try {
    const s = h.state; await s.setBrowserSession(session('page-1'));
    s.frameImage.value = { naturalWidth: 1280, naturalHeight: 800, currentSrc: s.frameObjectUrl.value, getBoundingClientRect: () => ({ left: 0, top: 0, width: 1280, height: 800 }), clientLeft: 0, clientTop: 0, clientWidth: 1280, clientHeight: 800 };
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

test('editor reserves inspection and hidden stages from SSE captures; remote wheel is opt-in', async () => {
  let captures = 0;
  const h = mountEditor({ browserFrame: async () => { captures++; return { id: captures }; } }); await tick();
  try {
    const s = h.state; await s.setBrowserSession(session('page-1'));
    s.workbenchBusy.value = true;
    await s.setBrowserSession(session('page-2')); assert.equal(captures, 1);
    s.workbenchBusy.value = false; s.tabVisible.value = false;
    await s.setBrowserSession(session('page-3')); assert.equal(captures, 1);
    s.tabVisible.value = true; s.pointerInFrame.value = true;
    await s.setBrowserSession(session('page-4')); assert.equal(captures, 1);
    let consumed = 0, prevent = 0;
    s.actionsPanel.value = { frameWheel: () => { consumed++; return true; } };
    const event = { preventDefault: () => prevent++ };
    s.remoteWheel.value = false; s.handleFrameWheel(event); assert.equal(consumed, 0);
    s.remoteWheel.value = true; s.handleFrameWheel(event); assert.equal(prevent, 1);
    s.editorStage.value = 'preview'; s.handleFrameWheel(event); assert.equal(consumed, 1);
  } finally { h.unmount(); }
});

const metadata = (overrides = {}) => ({ protocol: 'editor.state.v1', page_state_id: 'page-1', document_id: 'a'.repeat(64), validation_state_id: 'b'.repeat(64), frame_state_id: 'c'.repeat(64), viewport: { width: 640, height: 400 }, scroll: { x: 0, y: 0 }, pixel_ratio: 2, image: { width: 1280, height: 800 }, frame_sequence: 1, captured_at_unix_ms: 1, ...overrides });
test('negotiated capture requires matching metadata and decoded pixel dimensions', async () => {
  const h = harness(); h.queue.setTarget({ ...target(), state: metadata() });
  let task = h.queue.refresh(); await tick(); h.requests[0].resolve({ id: 'valid', frameState: metadata() }); await task;
  assert.equal(h.queue.stale.value, false); assert.equal(h.queue.frame.value.state.frame_sequence, 1);
  for (const bad of [undefined, metadata({ page_state_id: 'old' }), metadata({ image: { width: 20, height: 20 } }), metadata({ frame_sequence: 1 })]) {
    task = h.queue.refresh(); await tick(); h.requests.at(-1).resolve({ id: 'bad', frameState: bad }); await task;
    assert.equal(h.queue.frame.value.objectURL, 'image-valid'); assert.equal(h.queue.stale.value, true);
  }
  task = h.queue.refresh(); await tick(); h.requests.at(-1).resolve({ id: 'next', frameState: metadata({ frame_sequence: 2 }) }); await task;
  assert.equal(h.queue.stale.value, false); h.queue.dispose();
});


function fakeClock() {
  let now = 0, id = 0; const jobs = new Map();
  return { now: () => now, later(fn, delay) { jobs.set(++id, { fn, due: now + delay }); return id; }, cancel: id => jobs.delete(id),
    async advance(ms) { now += ms; for (const [id, job] of [...jobs]) if (job.due <= now) { jobs.delete(id); job.fn(); }
      for (let i = 0; i < 15; i++) await tick(); }, get count() { return jobs.size; } };
}

test('editor defaults to demand refresh; repeated SSE is silent, page changes wait for idle and action capture consumes demand', async () => {
  const time = fakeClock(); let captures = 0;
  const h = mountEditor({ browserFrame: async () => ({ id: ++captures }) }, time); await tick();
  try {
    const s = h.state;
    assert.equal(s.autoFrames.value, false); s.dirty.value = false; await s.setBrowserSession(session('page-1'));
    await time.advance(60000); assert.equal(captures, 1);
    await s.setBrowserSession(session('page-1')); await time.advance(60000); assert.equal(captures, 1);
    s.workbenchBusy.value = true; await s.setBrowserSession(session('page-2'));
    assert.equal(s.frameQueue.stale.value, true); assert.equal(s.frameDemand.value, true);
    await time.advance(10000); assert.equal(captures, 1); assert.match(s.frameStatus.value, /配置已保留/);
    s.workbenchBusy.value = false; await tick(); await time.advance(600); assert.equal(captures, 2);
    await s.setBrowserSession(session('page-3')); await s.updateBrowserFrame(session('page-3'));
    const afterAction = captures; await time.advance(60000);
    assert.equal(captures, afterAction); assert.equal(s.frameDemand.value, false);
  } finally { h.unmount(); }
  assert.equal(time.count, 0);
});

test('editor keeps refreshing frames non-interactive while delaying the loading sentence', async () => {
  const time = fakeClock(); const response = deferred(); let slow = false;
  const h = mountEditor({ browserFrame: async () => slow ? response.promise : { id: 'first' } }, time); await tick();
  try {
    const s = h.state; await s.setBrowserSession(session('page-1'));
    s.frameLoaded.value = true; assert.equal(s.frameInteractive.value, true);
    s.refreshMeasurement.value = { duration: 0, delay: 10000, failures: 1 };
    slow = true; const request = s.frameQueue.refresh(); await tick();
    assert.equal(s.frameInteractive.value, false); assert.equal(s.frameStatus.value, '');
    await time.advance(799); assert.equal(s.frameStatus.value, '');
    await time.advance(1); assert.match(s.frameStatus.value, /正在更新画面/);
    response.resolve({ id: 'second' }); await request; assert.equal(s.slowFrameRefresh.value, false); assert.equal(s.refreshMeasurement.value.failures, 0);
  } finally { h.unmount(); }
  assert.equal(time.count, 0);
});

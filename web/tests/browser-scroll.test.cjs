const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const vue = require('vue');
const ts = require('typescript');
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');
const tick = async () => { for (let i = 0; i < 4; i++) { await Promise.resolve(); await vue.nextTick(); } };
function mount(overrides = {}, refresh) {
  const filename = path.join(__dirname, '../src/views/app/browser-actions.vue');
  const { descriptor } = parse(fs.readFileSync(filename, 'utf8'), { filename });
  const script = compileScript(descriptor, { id: 'scroll-test' });
  const template = compileTemplate({ source: descriptor.template.content, filename, id: 'scroll-test', compilerOptions: { bindingMetadata: script.bindings } });
  assert.equal(template.errors.length, 0);
  const calls = [], refreshed = [];
  const props = vue.reactive({
    draft: { id: 'collector-1', revision: 1, entry_url: 'https://example.com', definition: { steps: [] } },
    session: { session_id: 'session-1', page_state_id: 'page-1', available: true },
    dirty: false, saving: false, frameReady: true, frameRefreshing: false,
    refreshFrame: async state => { refreshed.push(state); if (refresh) await refresh(state); props.frameReady = true; },
  });
  const api = {
    capabilities: async () => ({ supported_actions: ['scroll', 'click'], supports_live_inspection: true }),
    checkpoints: async () => ({ items: [] }), browserCommands: async () => ({ items: [] }),
    browserSession: async () => ({ ...props.session }),
    collector: async () => props.draft,
    createBrowserCommand: async (id, input, key) => {
      calls.push({ id, input, key });
      return { session_id: id, command_id: key, type: input.type, status: 'succeeded', result: {}, request: input };
    }, ...overrides,
  };
  const output = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } });
  const module = { exports: {} };
  class FocusTarget { focus() { this.focused = true; } }
  const origin = new FocusTarget();
  vm.runInNewContext(output.outputText, { module, exports: module.exports, require: name => ({ vue, './api': { appApi: api }, './browser-workbench': require('./workbench-loader.cjs'), './browser-selection.vue': {}, './record-designer.vue': {} })[name],
    crypto: { randomUUID: require('node:crypto').randomUUID }, window: { confirm: () => { throw new Error('scroll should not open a click confirmation'); } },
    document: { activeElement: origin }, HTMLElement: FocusTarget, setTimeout, clearTimeout,
  });
  const component = module.exports.default;
  let state;
  const setup = component.setup;
  component.setup = (props, context) => { state = setup(props, context); return state; };
  component.render = () => vue.h('div');
  const node = () => ({ children: [] });
  const renderer = vue.createRenderer({ createElement: node, createText: node, createComment: node, insert: (child, parent) => parent.children.push(child),
    remove: () => {}, setElementText: () => {}, setText: () => {}, patchProp: () => {}, parentNode: () => null, nextSibling: () => null });
  const app = renderer.createApp({ render: () => vue.h(component, { ...props,
    onSessionState: session => { props.session = session; }, onFrameInvalidated: () => { props.frameReady = false; },
  }) });
  app.mount(node());
  return { props, calls, refreshed, origin, get state() { return state; }, unmount: () => app.unmount() };
}
test('scroll buttons keep modes, use bounded CSS increments, and record only with explicit opt-in', async () => {
  const h = mount(); await tick();
  try {
    const s = h.state;
    await s.scrollPage(1); await tick();
    assert.equal(h.calls[0].input.record, false); assert.equal(h.calls[0].input.value, '600'); assert.equal(s.mode.value, 'browse');
    s.mode.value = 'record'; await tick();
    await s.scrollPage(-1); await tick();
    assert.equal(h.calls[1].input.record, false); assert.equal(h.calls[1].input.value, '-600');
    s.recordScroll.value = true;
    await s.scrollPage(1); await tick();
    assert.equal(h.calls[2].input.record, true); assert.ok(h.calls[2].input.step_id);
    s.mode.value = 'browse'; await tick(); assert.equal(s.recordScroll.value, false);
    assert.equal(h.refreshed.length, 3);
  } finally { h.unmount(); }
});
test('next scroll is blocked until terminal receipt and replacement frame are ready', async () => {
  let release;
  const h = mount({}, () => new Promise(resolve => { release = resolve; })); await tick();
  try {
    const first = h.state.scrollPage(1); await tick();
    assert.equal(h.state.canScroll.value, false);
    await h.state.scrollPage(1); assert.equal(h.calls.length, 1);
    release(); await first; await tick(); assert.equal(h.state.canScroll.value, true);
  } finally { h.unmount(); }
});
test('unknown result blocks new dispatch; manual reconciliation reuses the original key', async () => {
  const attempts = [];
  const h = mount({ createBrowserCommand: async (id, input, key) => {
    attempts.push({ key, input });
    if (attempts.length === 1) throw new Error('Network Error');
    return { session_id: id, command_id: key, type: 'scroll', status: 'succeeded', result: {}, request: input };
  } }); await tick();
  try {
    await h.state.scrollPage(1); await tick();
    assert.ok(h.state.unknownRequest.value);
    h.props.frameReady = true; await tick(); await h.state.scrollPage(1);
    assert.equal(attempts.length, 1);
    await h.state.resolveUnknown(); await tick();
    assert.equal(attempts.length, 2); assert.equal(attempts[1].key, attempts[0].key);
    assert.equal(JSON.stringify(attempts[1].input), JSON.stringify(attempts[0].input)); assert.equal(h.state.unknownRequest.value, undefined);
  } finally { h.unmount(); }
});
test('stale frame blocks coordinates and scroll; refresh is a read-only request', async () => {
  const h = mount(); await tick();
  try {
    h.props.frameReady = false; await tick();
    h.state.frameClick(1, 1, 'page-1'); await h.state.scrollPage(1);
    assert.equal(h.state.position.value, undefined); assert.equal(h.calls.length, 0);
    await h.state.requestFrame();
    assert.equal(h.calls.length, 0); assert.equal(h.refreshed.length, 1);
  } finally { h.unmount(); }
});

test('wheel admission keeps zoom native and refuses stale or uncertain command context', async () => {
  const h = mount(); await tick();
  try {
    const event = { deltaY: 100, deltaMode: 0 };
    assert.equal(h.state.frameWheel({ ...event, ctrlKey: true }, 800), false);
    h.props.frameReady = false; await tick();
    assert.equal(h.state.frameWheel(event, 800), false);
    h.props.frameReady = true; await tick();
    assert.equal(h.state.frameWheel(event, 800), true);
    h.state.unknownRequest.value = { sessionId: 'session-1', key: 'original', input: {} }; await tick();
    assert.equal(h.state.frameWheel(event, 800), false);
    assert.equal(h.calls.length, 0);
  } finally { h.unmount(); }
});


test('explicit webpage picks reopen the selection panel without losing configured action input', async () => {
  const h = mount(); await tick();
  try {
    const s = h.state;
    let picked;
    s.mode.value = 'record-select'; await tick();
    s.selectionPanel.value = { framePick: (...args) => { picked = args; }, leaveFrame: () => {} };
    s.value.value = 'unfinished value'; s.expression.value = '.saved-locator';
    s.openPanel('history'); s.panelCollapsed.value = true;
    s.frameClick(12, 24, 'page-1'); await tick();
    assert.equal(s.panelTab.value, 'selection'); assert.equal(s.panelCollapsed.value, false);
    assert.deepEqual(picked, [12, 24, 'page-1']);
    s.openPanel('preview'); s.panelCollapsed.value = true; await tick();
    assert.equal(s.value.value, 'unfinished value'); assert.equal(s.expression.value, '.saved-locator');
    assert.equal(h.calls.length, 0);
  } finally { h.unmount(); }
});

test('background receipts do not steal the active tab or reopen a collapsed panel', async () => {
  let release;
  const h = mount({}, () => new Promise(resolve => { release = resolve; })); await tick();
  try {
    const s = h.state;
    const command = s.scrollPage(1);
    for (let i = 0; i < 10 && !release; i++) await tick();
    assert.equal(typeof release, 'function');
    s.openPanel('preview'); s.panelCollapsed.value = true;
    release(); await command; await tick();
    assert.equal(s.last.value.status, 'succeeded');
    assert.equal(s.panelTab.value, 'preview'); assert.equal(s.panelCollapsed.value, true);
  } finally { h.unmount(); }
});

test('confirming a rule opens its preserved configuration; step switching refuses unsaved rules', async () => {
  const h = mount(); await tick();
  try {
    const s = h.state;
    const panel = { localDirty: true, applySelection: rule => { panel.rule = rule; return true; } };
    s.recordPanel.value = panel;
    const rule = { target: 'field', name: 'title', locator: { strategy: 'css', expression: '.title' } };
    s.applySelection(rule); await tick();
    assert.equal(s.panelTab.value, 'flow'); assert.equal(s.flowEditing.value, true); assert.equal(s.recordPanel.value.rule.name, 'title');
    h.props.draft.definition.steps = [{ type: 'navigate', step_id: 'open', config: { url: 'https://example.com' } }];
    s.selectStep(0); assert.equal(s.selectedStep.value, -1);
    s.recordPanel.value.localDirty = false; s.selectStep(0); await tick();
    assert.equal(s.selectedStep.value, 0); assert.equal(s.mode.value, 'browse');
    assert.equal(s.value.value, 'https://example.com');
  } finally { h.unmount(); }
});

test('rejected selection stays in the selection panel and reports the actual reason', async () => {
  const h=mount(); await tick();
  try {
    h.state.recordPanel.value={localDirty:false,applySelection:()=>false,selectionError:'字段范围与记录配置不同'};
    h.state.applySelection({target:'field',name:'title',locator:{strategy:'css',expression:'.title'}});
    assert.equal(h.state.panelTab.value,'selection'); assert.equal(h.state.flowEditing.value,false); assert.match(h.state.notice.value,/范围/);
  } finally {h.unmount();}
});

test('focus mode collapses temporarily and Escape restores the panel and focus without dispatching actions', async () => {
  const h = mount(); await tick();
  try {
    const s = h.state;
    let focused = false;
    s.workbenchRoot.value = { focus: () => { focused = true; } };
    s.openPanel('preview'); s.toggleFocus(); await tick();
    assert.equal(s.focused.value, true); assert.equal(s.panelCollapsed.value, true); assert.equal(focused, true);
    let prevented = false;
    s.handleWorkbenchKeydown({ key: 'Escape', preventDefault: () => { prevented = true; } }); await tick();
    assert.equal(prevented, true); assert.equal(s.focused.value, false); assert.equal(s.panelCollapsed.value, false);
    assert.equal(s.panelTab.value, 'preview'); assert.equal(h.origin.focused, true); assert.equal(h.calls.length, 0);
    s.toggleFocus(); await tick(); h.props.active = false; await tick();
    assert.equal(s.focused.value, false);
  } finally { h.unmount(); }
});


test('reviewing an element or data preview reserves automatic updates without disabling explicit refresh', async () => {
  const h = mount(); await tick();
  try {
    const s = h.state;
    s.mode.value = 'record-select'; await tick();
    s.selectionPanel.value = { hasPending: true, leaveFrame: () => {} }; await tick();
    assert.equal(s.reviewing.value, true); assert.equal(s.workbenchBusy.value, true);
    assert.equal(s.canRefreshFrame.value, true);
    s.panelCollapsed.value = true; await tick(); assert.equal(s.reviewing.value, false);
    s.openPanel('preview'); await tick(); assert.equal(s.reviewing.value, true);
    s.openPanel('history'); await tick(); assert.equal(s.reviewing.value, false);
    assert.equal(h.calls.length, 0);
  } finally { h.unmount(); }
});

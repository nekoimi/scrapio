// Short, offline checks against the real Vue setup and request interceptor.
// Run: node --test tests/browser-selection.test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const vue = require('vue');
const ts = require('typescript');
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');

function evaluate(source, imports, globals = {}) {
  const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } });
  const module = { exports: {} };
  vm.runInNewContext(output.outputText, { module, exports: module.exports, require: name => imports[name], ...globals });
  return module.exports.default;
}

function mount(api = {}) {
  const filename = path.join(__dirname, '../src/views/app/browser-selection.vue');
  const { descriptor, errors } = parse(fs.readFileSync(filename, 'utf8'), { filename });
  assert.equal(errors.length, 0);
  const script = compileScript(descriptor, { id: 'selection-test' });
  const template = compileTemplate({ source: descriptor.template.content, filename, id: 'selection-test', compilerOptions: { bindingMetadata: script.bindings } });
  assert.equal(template.errors.length, 0);
  const timers = new Set();
  const component = evaluate(script.content, { vue, './browser-state': require('./workbench-loader.cjs'), './api': { appApi: api } }, {
    setTimeout: fn => { timers.add(fn); return fn; }, clearTimeout: fn => timers.delete(fn),
  });
  let state;
  const setup = component.setup;
  component.setup = (props, context) => { state = setup(props, context); return state; };
  component.render = () => vue.h('div');
  const events = [];
  const props = vue.reactive({ session: { session_id: 'session-1', page_state_id: 'page-1', available: true }, target: 'field', mode: 'field', enabled: true, blocked: false, revision: 1 });
  const node = () => ({ children: [] });
  const renderer = vue.createRenderer({
    createElement: node, createText: node, createComment: node, insert: (child, parent) => parent.children.push(child),
    remove: () => {}, setElementText: () => {}, setText: () => {}, patchProp: () => {}, parentNode: () => null, nextSibling: () => null,
  });
  const app = renderer.createApp({ render: () => vue.h(component, { ...props, onRule: rule => events.push(['rule', rule]), onActionLocator: locator => events.push(['action', locator]), onHighlight: group => events.push(['highlight', group]), onVerifiedState: state => { props.session = {...props.session, state, page_state_id: state.page_state_id}; events.push(['verified', state]); } }) });
  app.mount(node());
  return { props, get state() { return state; }, events, unmount: () => app.unmount(), fireHover: () => { for (const fn of [...timers]) { timers.delete(fn); fn(); } } };
}

function locatorResult(input) {
  return { page_state_id: input.page_state_id, locator: input.locator, fingerprint: 'evidence', elements: [], sample_values: ['电影'], match_count: 1, truncated: false, comparison: 'changed', warnings: [] };
}
const settle = async () => { await Promise.resolve(); await vue.nextTick(); await Promise.resolve(); };

function linkInspection(input) {
  const anchor={element_id:'anchor',tag:'a',attributes:{href:'/movie/1'},text:'电影',parent_id:'record',child_count:1};
  const element=input.element_id==='anchor'?anchor:{element_id:'title',tag:'span',attributes:{},text:'电影',parent_id:'anchor',child_count:0};
  return {page_state_id:input.page_state_id,element,...(element===anchor?{}:{enclosing_link:anchor}),
    locators:[{...locatorResult(input),locator:{strategy:'css',expression:element===anchor?'a.title':'span.title'},kind:'id'}]};
}
test('switching a nested text field to link selects and validates its enclosing anchor',async()=>{
  const calls=[];
  const h=mount({inspectElement:async(_,input)=>{calls.push(input);return linkInspection(input);},checkLocator:async(_,input)=>({...locatorResult(input),link_sample_values:['https://example.test/movie/1']})});
  try{
    h.props.recordScope={strategy:'css',expression:'.record'};await settle();
    await h.state.inspect({position:{x:10,y:20}},'page-1');
    assert.equal(h.state.selection.value.element.tag,'span');assert.equal(h.state.extractKind.value,'text');
    h.state.fieldName.value='电影地址';h.state.extractKind.value='link';await h.state.onExtractChanged();
    assert.equal(calls.length,2);assert.equal(calls[1].element_id,'anchor');assert.equal(calls[1].scope.expression,'.record');
    assert.equal(h.state.selection.value.element.tag,'a');assert.equal(h.state.checkValid.value,true);assert.equal(h.state.fieldName.value,'电影地址');
    assert.equal(h.state.displayedSamples.value[0],'https://example.test/movie/1');
    h.state.saveRule('field');const rule=h.events.find(event=>event[0]==='rule')[1];
    assert.equal(rule.locator.expression,'a.title');assert.equal(rule.extract,'link');assert.equal(rule.scope.expression,'.record');
  }finally{h.unmount();}
});
test('reselecting a link field resolves nested text without changing field metadata',async()=>{
  const h=mount({inspectElement:async(_,input)=>linkInspection(input),checkLocator:async(_,input)=>locatorResult(input)});
  try{
    h.props.replacingField=true;await settle();h.state.prepareField({name:'原链接字段',extract:'link'});
    await h.state.inspect({position:{x:1,y:1}},'page-1');
    assert.equal(h.state.selection.value.element.tag,'a');assert.equal(h.state.expression.value,'a.title');
    assert.equal(h.state.fieldName.value,'原链接字段');assert.equal(h.state.extractKind.value,'link');assert.equal(h.state.checkValid.value,true);
  }finally{h.unmount();}
});
test('stale element evidence cannot be used to select its enclosing link',async()=>{
  let calls=0;
  const h=mount({inspectElement:async(_,input)=>{calls++;return linkInspection(input);},checkLocator:async(_,input)=>locatorResult(input)});
  try{
    await h.state.inspect({position:{x:1,y:1}},'page-1');
    h.props.session.page_state_id='page-2';await settle();
    await h.state.selectEnclosingLink();assert.equal(calls,1);assert.equal(h.state.selection.value.element.tag,'span');
  }finally{h.unmount();}
});
test('older browser service keeps explicit parent selection available without invented anchor evidence',async()=>{
  const h=mount({inspectElement:async(_,input)=>{const result=linkInspection(input);delete result.enclosing_link;return result;},checkLocator:async(_,input)=>locatorResult(input)});
  try{
    await h.state.inspect({position:{x:1,y:1}},'page-1');h.state.extractKind.value='link';await h.state.onExtractChanged();
    assert.equal(h.state.selection.value.element.tag,'span');assert.match(h.state.message.value,/扩大到父元素/);
  }finally{h.unmount();}
});

test('reselecting an existing field starts fresh evidence while preserving its name and extraction kind',async()=>{
 const h=mount({inspectElement:async()=>({page_state_id:'page-1',element:{element_id:'node',tag:'img',attributes:{},text:''},locators:[]})});
 try{
  h.props.replacingField=true;await settle();h.state.prepareField({name:'电影链接',extract:'link'});
  h.props.frameState={frame_sequence:7};await settle();h.state.framePick(1,1,'page-1');await settle();
  assert.equal(h.state.fieldName.value,'电影链接');assert.equal(h.state.extractKind.value,'link');assert.equal(h.state.selection.value.element.tag,'img');assert.equal(h.state.checkValid.value,false);
 }finally{h.unmount();}
});

test('same-document background recheck preserves fields and refreshes only locator evidence', async () => {
  const current = {protocol:'editor.state.v1', document_id:'a'.repeat(64), frame_state_id:'frame', validation_state_id:'dom', page_state_id:'page-1'};
  let requests = [];
  const h = mount({checkLocator: async (_, input) => ({...locatorResult(input), fingerprint:'b'.repeat(64), state:current}),
    recheckLocator: async (_, input) => { requests.push(input); return {...locatorResult(input), fingerprint:input.previous_fingerprint, comparison:'unchanged', state:h.props.session.state}; }});
  try {
    h.props.recheckEnabled = true; h.props.recheckBlocked = false; h.props.session.state = current; await settle();
    const s = h.state; s.expression.value='.title'; s.fieldName.value='电影标题'; await s.validateLocator();
    h.props.blocked = true; // PNG can be stale; locator-only recheck remains available.
    h.props.session = {...h.props.session, page_state_id:'page-2', state:{...current, page_state_id:'page-2', validation_state_id:'new-dom'}}; await settle();
    assert.equal(s.checkValid.value, false); assert.equal(s.rechecking.value, true); assert.equal(s.staleReason.value, '');
    h.fireHover(); await settle(); await settle();
    assert.equal(requests.length, 1); assert.equal(requests[0].document_id, current.document_id);
    assert.equal(requests[0].position, undefined); assert.equal(requests[0].frame_sequence, undefined); assert.equal(requests[0].element_id, undefined);
    assert.equal(s.checkValid.value, true); assert.equal(s.fieldName.value, '电影标题'); assert.equal(s.selectionValid.value, false);
    s.saveRule('field'); assert.equal(h.events.filter(e => e[0]==='rule').length, 1);
    s.prepareAction(); assert.equal(h.events.filter(e => e[0]==='action').length, 0);
  } finally {h.unmount();}
});

test('changed target evidence requires explicit confirmation and navigation never rechecks', async () => {
  const current = {protocol:'editor.state.v1', document_id:'a'.repeat(64), frame_state_id:'frame', validation_state_id:'dom', page_state_id:'page-1'};
  let calls=0;
  const h = mount({checkLocator:async (_, input)=>({...locatorResult(input), fingerprint:'b'.repeat(64), state:current}),
    recheckLocator:async (_, input)=>{calls++; return {...locatorResult(input), fingerprint:'c'.repeat(64), sample_values:['新标题'], state:h.props.session.state};}});
  try {
    h.props.recheckEnabled=true; h.props.recheckBlocked=false; h.props.session.state=current; await settle();
    const s=h.state; s.expression.value='.title'; s.fieldName.value='标题'; await s.validateLocator();
    h.props.session={...h.props.session, page_state_id:'page-2', state:{...current, page_state_id:'page-2', validation_state_id:'changed'}}; await settle(); h.fireHover(); await settle(); await settle();
    assert.equal(s.checkValid.value, false); assert.equal(s.pendingComparison.value.sample_values[0], '新标题');
    s.saveRule('field'); assert.equal(h.events.filter(e=>e[0]==='rule').length, 0);
    s.acceptComparison(); assert.equal(s.checkValid.value, true);
    h.props.session={...h.props.session, page_state_id:'page-3', state:{...current, page_state_id:'page-3', document_id:'d'.repeat(64)}}; await settle(); h.fireHover(); await settle();
    assert.equal(calls, 1); assert.equal(s.checkValid.value,false); assert.match(s.staleReason.value,/文档/);
  } finally {h.unmount();}
});

test('late background recheck cannot overwrite edited locator or adopt its state', async () => {
  const current={protocol:'editor.state.v1', document_id:'a'.repeat(64), frame_state_id:'frame', validation_state_id:'dom', page_state_id:'page-1'};
  let finish;
  const h=mount({checkLocator:async (_,input)=>({...locatorResult(input), fingerprint:'b'.repeat(64), state:current}),
    recheckLocator:(_,input)=>new Promise(resolve=>finish=()=>resolve({...locatorResult(input), comparison:'unchanged', fingerprint:input.previous_fingerprint, state:h.props.session.state}))});
  try {
    h.props.recheckEnabled=true; h.props.recheckBlocked=false; h.props.session.state=current; await settle();
    h.state.expression.value='.old'; await h.state.validateLocator();
    h.props.session={...h.props.session,page_state_id:'page-2',state:{...current,page_state_id:'page-2',validation_state_id:'changed'}}; await settle(); h.fireHover(); await settle();
    h.state.expression.value='.new'; finish(); await settle(); await settle();
    assert.equal(h.state.expression.value,'.new'); assert.equal(h.state.checkValid.value,false); assert.equal(h.events.filter(e=>e[0]==='verified').length,0);
  } finally {h.unmount();}
});

test('save blocking preserves inputs; page changes require new evidence before rule/action confirmation', async () => {
  const editor = mount({ checkLocator: async (_, input) => locatorResult(input) });
  try {
    const s = editor.state;
    s.expression.value = '.title'; s.fieldName.value = '电影名称'; s.extractKind.value = 'link';
    await s.validateLocator();
    assert.equal(s.checkValid.value, true);
    editor.props.blocked = true; await settle();
    s.saveRule('field'); s.prepareAction();
    assert.equal(editor.events.filter(e => e[0] !== 'highlight').length, 0);
    editor.props.blocked = false; await settle();
    assert.equal(s.checkValid.value, true);
    editor.props.session.page_state_id = 'page-2'; await settle();
    assert.equal(s.checkValid.value, false);
    assert.equal(s.fieldName.value, '电影名称'); assert.equal(s.expression.value, '.title'); assert.equal(s.extractKind.value, 'link');
    assert.equal(s.check.value.match_count, 1); // Last evidence is visible, but unusable.
    s.saveRule('field'); s.prepareAction();
    assert.equal(editor.events.filter(e => e[0] !== 'highlight').length, 0);
    await s.validateLocator(); s.saveRule('field');
    assert.equal(editor.events.filter(e => e[0] === 'rule').length, 1);
    editor.props.revision++; await settle();
    assert.equal(s.checkValid.value, false);
    editor.props.session.available = false; await settle();
    assert.equal(s.fieldName.value, '电影名称');
  } finally { editor.unmount(); }
});

test('split state explains invalidation while preserving inputs; identical SSE state keeps evidence', async () => {
  const current = { protocol: 'editor.state.v1', document_id: 'document', frame_state_id: 'frame', validation_state_id: 'dom' };
  let resultState = current;
  const h = mount({ checkLocator: async (_, input) => ({ ...locatorResult(input), state: resultState }) });
  try {
    const s = h.state; h.props.session.state = current; await settle();
    s.expression.value = '.title'; s.fieldName.value = '标题'; await s.validateLocator();
    assert.equal(s.checkValid.value, true);
    h.props.session.state = { ...current }; await settle(); assert.equal(s.checkValid.value, true);
    h.props.session = { ...h.props.session, page_state_id: 'page-2', state: { ...current, frame_state_id: 'scrolled' } }; await settle();
    assert.equal(s.checkValid.value, false); assert.match(s.staleReason.value, /旧坐标/); assert.equal(s.fieldName.value, '标题');
    resultState = h.props.session.state; await s.validateLocator(); assert.equal(s.checkValid.value, true);
    h.props.session = { ...h.props.session, page_state_id: 'page-3', state: { ...current, validation_state_id: 'new-dom' } }; await settle();
    assert.match(s.staleReason.value, /DOM/);
    h.props.session = { ...h.props.session, page_state_id: 'page-4', state: { ...current, document_id: 'new-document' } }; await settle();
    assert.match(s.staleReason.value, /文档/); assert.equal(s.expression.value, '.title');
  } finally { h.unmount(); }
});

test('target and browse-mode changes retain independent pending configurations', async () => {
  const editor = mount({ checkLocator: async (_, input) => locatorResult(input) });
  try {
    const s = editor.state;
    s.expression.value = '.title'; s.fieldName.value = '标题';
    await s.validateLocator();
    editor.props.mode = 'record'; editor.props.blocked = true; await settle();
    editor.props.mode = 'field'; editor.props.blocked = false; await settle();
    assert.equal(s.fieldName.value, '标题');
    editor.props.target = 'next'; await settle();
    s.expression.value = '.next'; s.nextKind.value = 'load_more';
    editor.props.target = 'field'; await settle();
    assert.equal(s.expression.value, '.title'); assert.equal(s.fieldName.value, '标题'); assert.equal(s.checkValid.value, false);
    editor.props.target = 'next'; await settle();
    assert.equal(s.expression.value, '.next'); assert.equal(s.nextKind.value, 'load_more');
    s.cancelSelection();
    assert.equal(s.hasPending.value, false);
  } finally { editor.unmount(); }
});

test('late validation cannot overwrite a changed rule or a replacement session', async () => {
  let finish;
  const editor = mount({ checkLocator: (_, input) => new Promise(resolve => { finish = () => resolve(locatorResult(input)); }) });
  try {
    const s = editor.state;
    s.expression.value = '.old'; s.fieldName.value = '保留';
    const pending = s.validateLocator();
    s.expression.value = '.new'; editor.props.session = { session_id: 'session-2', page_state_id: 'page-2', available: true }; await settle();
    finish(); await pending;
    assert.equal(s.check.value, undefined); assert.equal(s.checkValid.value, false);
    assert.equal(s.expression.value, '.new'); assert.equal(s.fieldName.value, '保留');
  } finally { editor.unmount(); }
});

test('hover misses stay quiet, service failures stay visible, and editing suspends hover', async () => {
  let code = 'NO_ELEMENT', calls = 0;
  const editor = mount({ inspectElement: async () => { calls++; throw { error: { code }, request_id: 'request-1' }; } });
  try {
    const s = editor.state;
    editor.props.frameState = { frame_sequence: 7 }; await settle();
    s.frameHover(1, 1, 'page-1'); editor.fireHover(); await settle();
    assert.equal(s.message.value, '');
    code = 'BROWSER_UNAVAILABLE'; s.frameHover(1, 1, 'page-1'); editor.fireHover(); await settle();
    assert.match(s.message.value, /服务暂时不可用/); assert.match(s.message.value, /request-1/);
    s.fieldName.value = '正在命名'; const before = calls;
    s.frameHover(1, 1, 'page-1'); editor.fireHover(); await settle(); assert.equal(calls, before);
    code = 'OUTSIDE_RECORD_SCOPE'; s.framePick(1, 1, 'page-1'); await settle();
    assert.match(s.message.value, /记录范围内部/); assert.equal(s.fieldName.value, '正在命名');
  } finally { editor.unmount(); }
});

test('local request failures suppress duplicate toast but authentication still uses the global flow', async () => {
  let reject, toasts = 0, authAlerts = 0;
  const axios = { create: () => ({ interceptors: { request: { use: () => {} }, response: { use: (_, error) => { reject = error; } } } }) };
  evaluate(fs.readFileSync(path.join(__dirname, '../src/utils/request.ts'), 'utf8').replace('import.meta.env.VITE_API_URL', "''"), {
    axios, 'element-plus': { ElMessage: { error: () => toasts++ }, ElMessageBox: { alert: () => { authAlerts++; return Promise.reject(); } } },
    '/@/utils/storage': { Session: { get: () => '', clear: () => {} } }, qs: {},
  });
  const failure = (code, localError = true, status = 400) => ({ config: { localError }, response: { status, data: { error: { code, message: '检查失败' } } } });
  await assert.rejects(reject(failure('NO_ELEMENT'))); assert.equal(toasts, 0);
  await assert.rejects(reject({ config: { localError: true }, message: 'Network Error' })); assert.equal(toasts, 0);
  await assert.rejects(reject(failure('NO_ELEMENT', false))); assert.equal(toasts, 1);
  await assert.rejects(reject(failure('UNAUTHENTICATED', true, 401))); assert.equal(authAlerts, 1);
  await Promise.resolve();
});

test('explicit click supersedes an in-flight hover and keeps unfinished field preferences', async () => {
  const requests = [];
  const editor = mount({ inspectElement: (_, input) => new Promise(resolve => requests.push({ input, resolve })) });
  try {
    const s = editor.state;
    editor.props.frameState = { frame_sequence: 7 }; await settle();
    s.frameHover(1, 1, 'page-1'); editor.fireHover();
    s.framePick(20, 20, 'page-1');
    const result = id => ({ page_state_id: 'page-1', element: { element_id: id, tag: 'img', attributes: {}, text: id }, locators: [] });
    requests[0].resolve(result('hover')); await settle();
    assert.equal(requests.length, 2); assert.equal(s.selection.value, undefined);
    assert.equal(requests[1].input.frame_sequence, 7);
    assert.equal(editor.events.some(e => e[0] === 'highlight' && e[1].kind === 'hover' && e[1].elements.length), false);
    s.fieldName.value = '正在编辑的标题'; s.extractKind.value = 'link';
    requests[1].resolve(result('selected')); await settle();
    assert.equal(s.selection.value.element.element_id, 'selected');
    assert.equal(s.fieldName.value, '正在编辑的标题'); assert.equal(s.extractKind.value, 'link');
  } finally { editor.unmount(); }
});


test('capture request can retain response headers without changing ordinary response unwrapping', () => {
  let response;
  const axios = { create: () => ({ interceptors: { request: { use() {} }, response: { use: fn => { response = fn; } } } }) };
  evaluate(fs.readFileSync(path.join(__dirname, '../src/utils/request.ts'), 'utf8').replace('import.meta.env.VITE_API_URL', "''"), {
    axios, 'element-plus': { ElMessage: {}, ElMessageBox: {} }, '/@/utils/storage': { Session: {} }, qs: {},
  });
  const normal = { data: { data: 'value' }, config: {} };
  assert.equal(response(normal), normal.data);
  const capture = { data: new Blob(), config: { responseEnvelope: true }, headers: { 'x-scrapio-frame': 'metadata' } };
  assert.equal(response(capture), capture);
});

const test = require('node:test');
const assert = require('node:assert/strict');
const { framePoint, wheelPixels, createWheelQueue, createAdaptiveRefresh } = require('./workbench-loader.cjs');
const tick = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
function clock() {
  let now = 0, id = 0;
  const jobs = new Map();
  return { now: () => now, later(fn, delay) { jobs.set(++id, { fn, due: now + delay }); return id; }, cancel: id => jobs.delete(id),
    async advance(ms) { now += ms; for (const [id, job] of [...jobs]) if (job.due <= now) { jobs.delete(id); job.fn(); } await tick(); },
    get count() { return jobs.size; }, get next() { return Math.min(...[...jobs.values()].map(j => j.due - now)); } };
}
test('wheel normalizes units, bounds travel and leaves zoom/horizontal gestures native', () => {
  assert.equal(wheelPixels({ deltaY: 3, deltaMode: 1 }, 800), 48);
  assert.equal(wheelPixels({ deltaY: 3, deltaMode: 2 }, 800), 1200);
  for (const modifier of ['ctrlKey', 'metaKey', 'shiftKey']) assert.equal(wheelPixels({ deltaY: 3, deltaMode: 0, [modifier]: true }, 800), 0);
});
test('coordinates use displayed bounds including local pan and screenshot DPR exactly once', () => {
  const point = framePoint(150, 110, { left: -50, top: 10, width: 640, height: 400 }, 2560, 1600);
  assert.equal(point.x, 800); assert.equal(point.y, 400);
  assert.equal(framePoint(700, 110, { left: -50, top: 10, width: 640, height: 400 }, 2560, 1600), undefined);
});
test('wheel has one aggregate slot and waits for terminal receipt plus replacement frame', async () => {
  const time = clock(), calls = []; let ready = true, finish;
  const queue = createWheelQueue({ clock: time, ready: () => ready, dispatch: delta => { calls.push(delta); ready = false; return new Promise(resolve => { finish = resolve; }); } });
  queue.add(100); queue.add(200); await time.advance(119); assert.equal(calls.length, 0);
  await time.advance(1); assert.deepEqual(calls, [300]);
  for (let i = 0; i < 50; i++) queue.add(100);
  await time.advance(120); finish(true); await tick(); assert.equal(calls.length, 1);
  ready = true; queue.pump(); assert.deepEqual(calls, [300, 1200]);
  queue.add(10); finish(false); await tick(); ready = true; await time.advance(120); queue.pump(); assert.equal(calls.length, 2);
  queue.dispose(); assert.equal(time.count, 0);
});
test('context switch stops old wheel intent without creating a new command', async () => {
  const time = clock(); let finish, calls = 0;
  const queue = createWheelQueue({ clock: time, ready: () => true, dispatch: () => { calls++; return new Promise(resolve => { finish = resolve; }); } });
  queue.add(100); await time.advance(120); queue.clear(); assert.equal(queue.add(100), false);
  finish(true); await tick(); await time.advance(1000); assert.equal(calls, 1); queue.dispose();
});
test('adaptive capture serializes, measures cost, idles and pauses while hidden or reserved', async () => {
  const time = clock(); let allowed = true, finish, calls = 0, measurement;
  const refresh = createAdaptiveRefresh({ clock: time, allowed: () => allowed, capture: () => { calls++; return new Promise(resolve => { finish = resolve; }); }, report: (...values) => measurement = values });
  refresh.activity(); await time.advance(400); assert.equal(calls, 1);
  await time.advance(900); refresh.activity(); assert.equal(time.count, 0);
  finish(true); await tick(); assert.equal(measurement[0], 900); assert.equal(time.next, 900);
  allowed = false; refresh.reconcile(); await time.advance(10000); assert.equal(calls, 1);
  allowed = true; refresh.reconcile(); assert.equal(time.next, 1800);
  await time.advance(1800); finish(false); await tick(); assert.equal(time.next, 3600);
  await time.advance(3600); finish(false); await tick(); assert.equal(time.next, 7200);
  await time.advance(7200); finish(false); await tick(); assert.equal(time.next, 14400);
  await time.advance(14400); finish(false); await tick(); assert.equal(time.next, 15000);
  refresh.dispose(); assert.equal(time.count, 0);
});

test('frame response distinguishes absent legacy metadata from broken extension responses', () => {
  const { captureBlob, stateChange } = require('./workbench-loader.cjs');
  const state = { protocol: 'editor.state.v1', page_state_id: 'page', document_id: 'a'.repeat(64), validation_state_id: 'b'.repeat(64), frame_state_id: 'c'.repeat(64), viewport: { width: 640, height: 400 }, scroll: { x: 0, y: 0 }, pixel_ratio: 2, image: { width: 1280, height: 800 }, frame_sequence: 1, captured_at_unix_ms: 1 };
  const blob = new Blob(); assert.equal(captureBlob(blob, undefined, 'page'), blob);
  assert.equal(captureBlob(blob, JSON.stringify(state), 'page').frameState.document_id, state.document_id);
  for (const header of ['null', '{', JSON.stringify({ ...state, pixel_ratio: 0 }), JSON.stringify({ ...state, page_state_id: 'old' })]) assert.throws(() => captureBlob(blob, header, 'page'));
  assert.equal(stateChange(state, { ...state, frame_state_id: 'd'.repeat(64) }), 'frame');
  assert.equal(stateChange(state, { ...state, validation_state_id: 'd'.repeat(64) }), 'validation');
  assert.equal(stateChange(state, { ...state, document_id: 'd'.repeat(64) }), 'document');
});

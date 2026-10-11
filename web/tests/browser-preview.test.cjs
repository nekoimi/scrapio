const test = require('node:test');
const assert = require('node:assert/strict');
const { createPreviewDecoder, createPreviewDisplay } = require('./workbench-loader.cjs');
const packet = (sequence = 1) => ({ metadata: { protocol: 'editor.preview.v1', sequence, mime: 'image/jpeg', width: 1280, height: 800, captured_at_unix_ms: 1, preview_only: true }, image: new Blob(['jpeg']) });
function wire(metadata, image = Buffer.from('jpeg')) {
  const header = Buffer.from(JSON.stringify(metadata)), size = Buffer.alloc(8);
  size.writeUInt32BE(header.length, 0); size.writeUInt32BE(image.length, 4);
  return Buffer.concat([size, header, image]);
}
const tick = async () => { for (let i=0; i<5; i++) await Promise.resolve(); };
test('preview framing tolerates byte splits and ignores idle heartbeats', async () => {
  const decoder = createPreviewDecoder(), frames = [];
  const stream = Buffer.concat([wire({protocol:'editor.preview.v1', heartbeat:true}, Buffer.alloc(0)), wire(packet().metadata), wire(packet(2).metadata)]);
  for (const byte of stream) frames.push(...decoder.feed(Uint8Array.of(byte)));
  decoder.finish(); assert.equal(frames.length, 2); assert.equal(frames[1].metadata.sequence, 2);
  assert.equal(await frames[0].image.text(), 'jpeg');
});
test('oversize, invalid, old or incomplete preview frames fail closed', () => {
  const size = Buffer.alloc(8); size.writeUInt32BE(4097);
  assert.throws(() => createPreviewDecoder().feed(size), /限制/);
  size.writeUInt32BE(1); size.writeUInt32BE(4*1024*1024+1, 4);
  assert.throws(() => createPreviewDecoder().feed(size), /限制/);
  const d = createPreviewDecoder(); d.feed(wire(packet().metadata));
  assert.throws(() => d.feed(wire(packet().metadata)), /元数据/);
  assert.throws(() => createPreviewDecoder().feed(wire({...packet().metadata, preview_only:false})), /元数据/);
  const truncated = createPreviewDecoder(); truncated.feed(wire(packet().metadata).subarray(0, 10));
  assert.throws(() => truncated.finish(), /不完整/);
  assert.throws(() => createPreviewDecoder().feed(wire({error_code:'STREAM_BUSY'}, Buffer.alloc(0))), e => e.code === 'STREAM_BUSY');
});
test('slow preview display retains only latest pending frame and releases replaced URLs', async () => {
  const decodes = [], revoked = [];
  const display = createPreviewDisplay({createURL: blob => `frame-${blob.id}`, revokeURL: url => revoked.push(url), decode: url => new Promise(resolve => decodes.push({url, resolve}))});
  const push = id => display.push({...packet(id), image:{id}});
  push(1); push(2); push(3); assert.equal(decodes.length, 1);
  decodes[0].resolve({width:1280,height:800}); await tick();
  assert.equal(display.frame.value.metadata.sequence, 1); assert.equal(decodes.length, 2); assert.equal(decodes[1].url, 'frame-3');
  decodes[1].resolve({width:1280,height:800}); await tick();
  assert.equal(display.frame.value.metadata.sequence, 3); assert.deepEqual(revoked, ['frame-1']);
  display.clear(); assert.deepEqual(revoked, ['frame-1','frame-3']);
});
test('cancelled decode cannot restore preview after freezing or session replacement', async () => {
  let resolve; const revoked = [];
  const display = createPreviewDisplay({createURL: () => 'old', revokeURL: url => revoked.push(url), decode: () => new Promise(done => resolve=done)});
  display.push(packet()); display.clear(); resolve({width:1280,height:800}); await tick();
  assert.equal(display.frame.value, undefined); assert.deepEqual(revoked, ['old']);
});

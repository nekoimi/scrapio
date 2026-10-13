// Short offline artifact checks. This is not the production workflow interpreter.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const { createRequire } = require('node:module');
const webRequire = createRequire(path.resolve(__dirname, '../../../web/package.json'));
const Ajv = webRequire('ajv'), { parse: baseParse } = webRequire('@vue/compiler-dom');
const { openapi, baseline } = require('./build-contract.cjs');
let count = 0;
function check(name, fn) { fn(); count++; process.stdout.write('OK ' + name + '\n'); }
const read = name => fs.readFileSync(path.join(__dirname, name), 'utf8');
const clone = x => JSON.parse(JSON.stringify(x));
const schemaRoot = JSON.parse(JSON.stringify({ definitions: openapi.components.schemas }).replaceAll('#/components/schemas/', '#/definitions/'));
const ajv = new Ajv({ allErrors: true });
function validate(name, value) { const fn = ajv.compile({ ...schemaRoot, $ref: '#/definitions/' + name }); assert.ok(fn(value), name + ': ' + JSON.stringify(fn.errors)); }
function reject(name, value) { const fn = ajv.compile({ ...schemaRoot, $ref: '#/definitions/' + name }); assert.equal(fn(value), false); }
const collectorsByVersion = Object.fromEntries(baseline.collectors.map(c => [c.version_id, c.definition]));
function semanticTask(task) {
 validate('TaskDefinition', task);
 const ids = task.stages.map(s => s.id); assert.equal(new Set(ids).size, ids.length, 'duplicate stage');
 assert.ok(ids.includes(task.entry_stage));
 assert.equal(new Set(task.edges.map(e => e.id)).size, task.edges.length, 'duplicate edge');
 const indegree = Object.fromEntries(ids.map(id => [id, 0]));
 for (const s of task.stages) assert.ok(collectorsByVersion[s.collector_version_id], 'unknown version');
 for (const edge of task.edges) {
  assert.ok(ids.includes(edge.from) && ids.includes(edge.to)); indegree[edge.to]++;
  const dest = collectorsByVersion[task.stages.find(s => s.id === edge.to).collector_version_id];
  for (const field of dest.input_fields.filter(f => f.required && !Object.hasOwn(f, 'default'))) assert.ok(edge.mapping[field.key], 'missing target ' + field.key);
  const src = collectorsByVersion[task.stages.find(s => s.id === edge.from).collector_version_id];
  for (const b of Object.values(edge.mapping)) if (b.kind === 'field' && b.path.startsWith('output.')) assert.ok(src.output.fields.some(f => f.key === b.path.slice(7)), 'unknown source field');
 }
 assert.equal(indegree[task.entry_stage], 0); for (const id of ids.filter(i => i !== task.entry_stage)) assert.equal(indegree[id], 1, 'exactly one upstream');
 const queue = [task.entry_stage], visited = new Set();
 while (queue.length) { const id = queue.shift(); assert.ok(!visited.has(id), 'cycle'); visited.add(id); for (const e of task.edges.filter(e => e.from === id)) queue.push(e.to); }
 assert.equal(visited.size, ids.length, 'disconnected');
 if (task.source.kind === 'manual') assert.ok(task.source.inputs?.length, 'manual inputs required');
}
function typedFields(fields, value) {
 const allowed = new Set(fields.map(f => f.key)); for (const k of Object.keys(value)) assert.ok(allowed.has(k), 'unknown field ' + k);
 for (const f of fields) {
  if (!Object.hasOwn(value, f.key)) { assert.ok(!f.required || Object.hasOwn(f, 'default'), 'missing ' + f.key); continue; }
  const v = value[f.key]; if (v === null) { assert.ok(f.nullable, 'null ' + f.key); continue; }
  const values = f.multiple ? v : [v]; assert.ok(Array.isArray(values)); if (f.required) assert.ok(values.length);
  for (const item of values) {
   if (f.type === 'integer') assert.ok(Number.isSafeInteger(item));
   else if (f.type === 'number') assert.ok(typeof item === 'number' && Number.isFinite(item));
   else if (f.type === 'boolean') assert.equal(typeof item, 'boolean');
   else if (f.type !== 'json') { assert.equal(typeof item, 'string'); if (f.required && !f.empty_allowed) assert.ok(item.length); }
   if (f.type === 'url') { const url = new URL(item); assert.ok(['http:', 'https:'].includes(url.protocol)); assert.ok(!url.username && !url.password); }
  }
 }
}
function mapInput(edge, output) { return Object.fromEntries(Object.entries(edge.mapping).map(([key, b]) => { assert.equal(b.kind, 'field'); return [key, output[b.path.slice(7)]]; })); }
check('generated OpenAPI and baseline have no drift', () => { assert.deepEqual(JSON.parse(read('openapi.json')), openapi); assert.deepEqual(JSON.parse(read('fixtures/baseline.json')), baseline); });
check('planned API operations, unique IDs and valid schema references', () => {
 const ids=[]; for (const [url, methods] of Object.entries(openapi.paths)) for (const [method, op] of Object.entries(methods)) { assert.equal(op['x-implementation-status'], ['O1','O2','O3','O4'].includes(op['x-delivery-phase'])?'implemented':'planned'); ids.push(op.operationId); assert.ok(op.responses['401'] && op.responses['403']); if (method === 'post') assert.ok(op.parameters.some(p => p.name === 'Idempotency-Key' && p.required)); for (const p of [...url.matchAll(/\{(\w+)\}/g)]) assert.ok(op.parameters.some(x=>x.name===p[1] && x.required)); }
 assert.equal(new Set(ids).size, ids.length); for (const m of JSON.stringify(openapi).matchAll(/"\$ref":"#\/components\/schemas\/([^"]+)"/g)) assert.ok(openapi.components.schemas[m[1]]);
});
check('three standalone collectors and one task validate', () => { for (const c of baseline.collectors) validate('CollectorDefinition', c.definition); semanticTask(baseline.task); });
check('draft trial and immutable formal trigger have distinct contracts',()=>{validate('TaskTrialRequest',{expected_revision:1,inputs:baseline.task.source.inputs,mode:'offline',sample_ids:['list','detail','comments']});reject('TaskTrialRequest',{task_version_id:'v1',inputs:baseline.task.source.inputs});validate('TriggerRequest',{task_version_id:'v1'});reject('TriggerRequest',{expected_revision:1});});
check('new collector schema rejects inherited detail and output table binding', () => { const c=clone(baseline.collectors[1].definition); c.detail_fields=[]; reject('CollectorDefinition',c); delete c.detail_fields; c.output.table_id='old'; reject('CollectorDefinition',c); });
check('required inputs, unknown fields and unsafe URL are rejected', () => { const f=baseline.collectors[1].definition.input_fields; assert.throws(()=>typedFields(f,{url:'https://example.test'})); assert.throws(()=>typedFields(f,{...baseline.samples[1].input,cookie:'secret'})); assert.throws(()=>typedFields(f,{url:'javascript:alert(1)',movie_id:'m001'})); });
check('graph rejects cycle, multi-upstream and missing target input', () => { let t=clone(baseline.task);t.edges.push({...t.edges[0],id:'back',from:'comment',to:'ranking'});assert.throws(()=>semanticTask(t));t=clone(baseline.task);t.edges.push({...t.edges[0],id:'double'});assert.throws(()=>semanticTask(t));t=clone(baseline.task);delete t.edges[0].mapping.movie_id;assert.throws(()=>semanticTask(t)); });
check('three-stage mapping preserves ID and resolved URL', () => { for (let i=0;i<2;i++){const input=mapInput(baseline.task.edges[i],baseline.samples[i].expected[0]);typedFields(baseline.collectors[i+1].definition.input_fields,input);assert.deepEqual(input,baseline.samples[i+1].input);} });
check('HTML fixtures match declared per-page values; no full pagination claim', () => {
 function attr(node,key){return node.props?.find(p=>p.type===6&&p.name===key)?.value?.content || '';}
 function descendants(node){return (node.children||[]).flatMap(c=>c.type===1?[c,...descendants(c)]:[]);}
 function text(node){return (node.children||[]).map(c=>c.type===2?c.content:text(c)).join('').trim();}
 function select(node,expression){return descendants(node).filter(n=>expression.startsWith('.')?attr(n,'class').split(' ').includes(expression.slice(1)):n.tag===expression||expression.includes('.')&&n.tag===expression.split('.')[0]&&attr(n,'class').split(' ').includes(expression.split('.')[1]));}
 for(const sample of baseline.samples){const c=baseline.collectors.find(x=>x.id===sample.collector_id).definition;const ast=baseParse(read('fixtures/'+sample.html));const roots=select(ast,c.records.expression);const base=attr(select(ast,'base')[0],'href');const actual=roots.map(root=>Object.fromEntries(c.output.mapping.map(m=>{let value;if(m.source==='input')value=sample.input[m.input_key];else{const node=select(root,m.locator.expression)[0];assert.ok(node);value=m.extract==='link'?new URL(attr(node,'href'),base).href:m.extract==='attribute'?attr(node,m.attribute):text(node);}const type=c.output.fields.find(f=>f.key===m.key).type;if(['integer','number'].includes(type))value=Number(value);return[m.key,value];})));assert.deepEqual(actual,sample.expected);typedFields(c.input_fields,sample.input);for(const output of actual)typedFields(c.output.fields,output);if(c.pagination.kind!=='none'){assert.ok(select(ast,'.next').length);assert.match(sample.coverage,/page1 only/);}}
});
check('JSON array and explicit input passthrough expected values agree', () => { assert.deepEqual(baseline.json_sample.response.map(r=>({...baseline.json_sample.input,...r})),baseline.json_sample.expected); });
check('failure sequence baseline and provenance remain explicit', () => { assert.match(baseline.provenance,/synthetic/);assert.equal(baseline.event_cases.length,6);assert.match(baseline.real_targets.verification,/pending/);const c=baseline.event_cases.find(c=>c.id==='unknown_action');assert.match(c.expected,/no repeated click/); });

check('prototype script runs offline with empty data and standalone collector input', () => {
 const html=read('prototype.html'), script=html.match(/<script>([\s\S]*?)<\/script>/)[1];new vm.Script(script);
 const elements=new Map();const element=id=>{if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',value:'',open:false,showModal(){this.open=true;},close(){this.open=false;},addEventListener(){}});return elements.get(id);};
 const ctx={window:{},document:{getElementById:element,addEventListener(){}},URL};vm.runInNewContext(script,ctx,{timeout:1000});const app=ctx.window.O0App;
 app.command('nav:data');assert.match(element('view').innerHTML,/尚无正式输出/);
 app.command('collector:movie');app.command('test-collector',JSON.stringify(baseline.samples[1].input));assert.equal(app.state.tested.movie,true);
 app.command('test-collector','{}');assert.equal(app.state.tested.movie,false);assert.match(app.state.notice,/无效/);
 app.command('single');assert.equal(app.state.singleCollector,'movie');assert.equal(app.state.stage,'movie');assert.match(element('view').innerHTML,/电影详情采集器/);
 app.command('new-task');app.command('check-mapping','title');assert.match(app.mappingError(),/映射错误/);app.command('trial');assert.equal(app.state.trial,null);
 app.command('check-mapping','detail_url');app.command('trial');assert.equal(app.state.trial.writes,0);assert.equal(Object.keys(app.state.data).length,0);
 app.command('publish-task');assert.equal(element('confirmation').open,true);app.command('dialog-cancel');assert.equal(app.state.published,false);
 app.command('publish-task');app.command('dialog-confirm');assert.equal(app.state.published,true);
 app.command('run');app.command('dialog-confirm');assert.equal(app.state.run.status,'partial');assert.equal(Object.keys(app.state.data).length,3);
 app.command('unknown');assert.match(app.state.dialog.text,/先查询原回执/);app.command('dialog-cancel');
 app.command('conflict');assert.match(app.state.dialog.text,/Schema/);app.command('dialog-cancel');
 app.command('filter');assert.match(app.state.notice,/不产生短评输入/);
 app.command('replay');app.command('dialog-confirm');assert.equal(app.state.run.replay,true);assert.match(app.state.notice,/原版本/);
 app.command('nav:data');app.command('select-record');app.command('from-table');assert.equal(app.state.taskMode,'single');assert.equal(app.state.stage,'comment');assert.equal(app.state.published,false);
 assert.ok(!/\b(fetch|alert|confirm|prompt)\s*\(/.test(script));
});
check('Markdown links and fences remain valid',()=>{for(const file of fs.readdirSync(__dirname).filter(f=>f.endsWith('.md'))){const body=read(file);assert.equal((body.match(/^```/gm)||[]).length%2,0);for(const m of body.matchAll(/\]\(([^)]+)\)/g)){if(!/^https?:/.test(m[1]))assert.ok(fs.existsSync(path.resolve(__dirname,m[1].split('#')[0])),file+': '+m[1]);}}});
process.stdout.write(`Passed ${count} offline artifact checks; no network, database or browser launched.\n`);

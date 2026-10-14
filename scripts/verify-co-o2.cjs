// Short offline checks for actual O2 routes and lost-response mutation handling.
const fs = require("node:fs"),
  path = require("node:path"),
  assert = require("node:assert/strict"),
  vm = require("node:vm");
const root = path.resolve(__dirname, ".."),
  ts = require(path.join(root, "web/node_modules/typescript"));
const api = fs.readFileSync(
  path.join(root, "internal/api/v4/tasks.go"),
  "utf8",
);
const mounted = new Map();
for (const m of api.matchAll(
  /v\.HandleFunc\("([^"]+)"[^\n]*\.Methods\(([^\n]+)\)/g,
)) {
  const urls = m[1].includes("{kind:")
    ? ["work-items", "outputs", "observations", "dispatches", "gather-groups"].map((kind) =>
        m[1].replace(/\{kind:[^}]+\}/, kind),
      )
    : [m[1]];
  for (const url of urls)
    for (const method of [...m[2].matchAll(/"(GET|POST|PUT)"/g)].map((v) =>
      v[1].toLowerCase(),
    ))
      mounted.set(method + " " + url, true);
}
const contract = JSON.parse(
  fs.readFileSync(
    path.join(root, "docs/项目文档v2.2/编排O0/openapi.json"),
    "utf8",
  ),
);
let routes = 0;
for (const [url, methods] of Object.entries(contract.paths))
  for (const [method, op] of Object.entries(methods))
    if (["O2", "O3", "O4", "O5"].includes(op["x-delivery-phase"])) {
      assert.ok(
        mounted.has(method + " " + url),
        "unmounted contract: " + method + " " + url,
      );
      routes++;
    }
assert.equal(routes, mounted.size, "mounted O2/O3/O4 API missing from contract");
process.stdout.write("OK " + routes + " mounted O2/O3/O4 route/method contracts\n");
const Ajv = require(path.join(root, "web/node_modules/ajv"));
const ajv = new Ajv({ strict: false });
for (const [name, schema] of Object.entries(contract.components.schemas))
  ajv.addSchema(schema, "#/components/schemas/" + name);
const baseline = JSON.parse(
  fs.readFileSync(
    path.join(root, "docs/项目文档v2.2/编排O0/fixtures/baseline.json"),
    "utf8",
  ),
);
const validate = ajv.getSchema(
  "#/components/schemas/SingleStageTaskDefinition",
);
assert.equal(validate(baseline.task), false);
const task = JSON.parse(JSON.stringify(baseline.task));
task.stages = [task.stages[0]];
task.edges = [];
assert.equal(validate(task), true);
task.source.kind = "table";
assert.equal(validate(task), false);
const graph = ajv.getSchema('#/components/schemas/GraphTaskDefinition');
assert.equal(graph(baseline.task),true,JSON.stringify(graph.errors));
const pass = ajv.getSchema('#/components/schemas/SaveBinding');
assert.equal(pass({mode:'pass_through'}),true);
assert.equal(pass({mode:'pass_through',table_id:'legacy'}),false);
const multi=JSON.parse(JSON.stringify(baseline.collectors[0].definition));
multi.additional_outputs=[{records:multi.records,output:{...multi.output,port:'links'}}];
const collector=ajv.getSchema('#/components/schemas/CollectorDefinition');
assert.equal(collector(multi),true,JSON.stringify(collector.errors));
multi.additional_outputs[0].output.port='bad-port';assert.equal(collector(multi),false);
const portsTask=JSON.parse(JSON.stringify(baseline.task));
portsTask.stages[0].save={mode:'pass_through'};
portsTask.stages[0].additional_saves=[{port:'links',save:{mode:'pass_through'}}];
portsTask.edges[0].port='links';assert.equal(graph(portsTask),true,JSON.stringify(graph.errors));
process.stdout.write('OK O5-02 named ports, pass-through and strict save contracts\n');
const mergeTask=JSON.parse(JSON.stringify(baseline.task));
mergeTask.stages[1].input_policy='merge_identical';assert.equal(graph(mergeTask),true);
mergeTask.stages[1].input_policy='url_only';assert.equal(graph(mergeTask),false);
assert.ok(contract.paths['/runs/{run_id}/work-items/{item_id}'].get.parameters.some(p=>p.name==='source_cursor'));
const badGraph=JSON.parse(JSON.stringify(baseline.task));badGraph.edges[0].filter.op='contains';assert.equal(graph(badGraph),false);
const expanded=JSON.parse(JSON.stringify(baseline.task));expanded.edges[0].mapping.url={kind:'field',path:'element'};expanded.edges[0].expand={path:'output.urls'};assert.equal(graph(expanded),true);
const control = ajv.getSchema("#/components/schemas/ControlRequest");
assert.equal(control({ action: "cancel" }), false);
assert.equal(control({ action: "pause", expected_control_epoch: 0 }), true);
const storage = new Map(),
  calls = [];
let nextKey = 0,
  handler;
const code = ts.transpileModule(
  fs.readFileSync(path.join(root, "web/src/views/co/tasks-api.ts"), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
    },
  },
).outputText;
const testedModule = { exports: {} };
vm.runInNewContext(
  code,
  {
    module: testedModule,
    exports: testedModule.exports,
    URLSearchParams,
    Error,
    JSON,
    sessionStorage: {
      getItem: (k) => storage.get(k) || null,
      setItem: (k, v) => storage.set(k, v),
      removeItem: (k) => storage.delete(k),
    },
    require: () => ({
      key: () => String(++nextKey),
      call: async (...args) => {
        calls.push(args);
        return handler(...args);
      },
    }),
    console,
  },
  { timeout: 1000 },
);
(async () => {
  const { taskMutation, recoverTaskMutation } = testedModule.exports;
  const payload = { task_version_id: "v1", namespace: "formal" };
  handler = async () => {
    throw new Error("response lost");
  };
  await assert.rejects(() =>
    taskMutation("startTask", "t", "/tasks/t/runs", payload),
  );
  const saved = JSON.parse(storage.values().next().value);
  await assert.rejects(() =>
    taskMutation("startTask", "t", "/tasks/t/runs", {
      ...payload,
      task_version_id: "v2",
    }),
  );
  assert.equal(calls.length, 1);
  handler = async () => {
    throw { error: { code: "NOT_FOUND" } };
  };
  await assert.rejects(() => recoverTaskMutation("startTask", "t"));
  assert.equal(storage.size, 1, "missing receipt must retain identity");
  handler = async () => ({ id: "original-run" });
  const same = await taskMutation("startTask", "t", "/tasks/t/runs", payload);
  assert.equal(same.id, "original-run");
  assert.equal(calls.at(-1)[3], saved.key);
  assert.equal(storage.size, 0);
  handler = async () => {
    throw { error: { code: "INPUT_FIELD_INVALID" } };
  };
  await assert.rejects(() =>
    taskMutation("createTask", "", "/tasks", { name: "bad" }),
  );
  assert.equal(
    storage.size,
    0,
    "definite rollback should allow corrected fields",
  );
  handler = async () => {
    throw new Error("response lost");
  };
  await assert.rejects(() =>
    taskMutation("startTask", "t", "/tasks/t/runs", payload),
  );
  handler = async () => ({ result: { id: "original-run" } });
  const before = calls.length;
  const recovered = await recoverTaskMutation("startTask", "t");
  assert.equal(recovered.id, "original-run");
  assert.equal(calls.length, before + 1);
  assert.equal(calls.at(-1)[1], undefined, "recovery must only GET");
  assert.equal(storage.size, 0);
  process.stdout.write(
    "OK O2 single-stage/schema contracts and lost-response mutation invariants\n",
  );
})().catch((e) => {
  console.error(e);
  process.exit(1);
});

const unionTask=JSON.parse(JSON.stringify(baseline.task));
unionTask.stages[2].input_mode='union';
unionTask.edges.push({...unionTask.edges[0],id:'also_comments',to:unionTask.stages[2].id});
assert.equal(graph(unionTask),true,JSON.stringify(graph.errors));
unionTask.stages[2].input_mode='join';assert.equal(graph(unionTask),false);
unionTask.stages[2].gather={key_fields:['movie_id'],missing:'reject',max_rows:100,max_bytes:262144};assert.equal(graph(unionTask),true,JSON.stringify(graph.errors));
unionTask.stages[2].gather.max_rows=1001;assert.equal(graph(unionTask),false);
const replay=ajv.getSchema('#/components/schemas/ReplayStageRequest');
assert.equal(replay({stage_id:'movie',edge_id:'ranking_movie',output_ids:['out'],confirmed:true}),true);
assert.equal(replay({stage_id:'movie',edge_id:'ranking_movie',item_ids:['item'],confirmed:true}),false);
process.stdout.write('OK O5-03 union and explicit replay edge contracts\n');

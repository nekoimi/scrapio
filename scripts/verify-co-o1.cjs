// Offline targeted checks only: no install, full build, service or site access.
const fs = require("node:fs"),
  path = require("node:path"),
  assert = require("node:assert/strict");
const root = path.resolve(__dirname, ".."),
  web = path.join(root, "web");
const sfc = require(path.join(web, "node_modules/@vue/compiler-sfc"));
const ts = require(path.join(web, "node_modules/typescript"));
const Ajv = require(path.join(web, "node_modules/ajv"));
const files = [
 "web/src/views/co/ai-assist.vue",
 "web/src/views/co/delivery-bindings.vue",
 "web/src/views/co/run-deliveries.vue",
 "web/src/views/co/transform-editor.vue",
 "web/src/views/co/collector-ports.vue",
 "web/src/views/co/port-saves.vue",
  "web/src/views/co/collectors.vue",
  "web/src/views/co/workbench.vue",
 "web/src/views/co/tasks.vue",
 "web/src/views/co/task-edit.vue",
 "web/src/views/co/runs.vue",
 "web/src/views/co/run-detail.vue",
 "web/src/views/co/data.vue",
 "web/src/views/co/source-picker.vue",
 "web/src/views/co/task-daily.vue",
 "web/src/views/co/task-subscription.vue",
 "web/src/views/co/replay-stage.vue",
 "web/src/views/co/home.vue",
  "web/src/views/app/browser-selection.vue",
  "web/src/views/app/layout.vue",
];
const virtual = new Map();
for (const file of files) {
  const filename = path.join(root, file);
  const parsed = sfc.parse(fs.readFileSync(filename, "utf8"), { filename });
  assert.deepEqual(parsed.errors, []);
  const script = sfc.compileScript(parsed.descriptor, { id: file });
  const template = sfc.compileTemplate({
    source: parsed.descriptor.template.content,
    filename,
    id: file,
    compilerOptions: { bindingMetadata: script.bindings },
  });
  assert.deepEqual(template.errors, []);
  const transpiled = ts.transpileModule(script.content, {
    compilerOptions: {
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.ESNext,
    },
    reportDiagnostics: true,
  });
  assert.deepEqual(transpiled.diagnostics || [], []);
  if (file.includes("/co/"))
    virtual.set(filename.replace(/\.vue$/, ".co-check.ts"), script.content);
  process.stdout.write("OK SFC compile " + file + "\n");
}
// Check types in changed co TS/SFC scripts, with real existing dependencies.
// Existing dependency diagnostics are out of scope; no full project typecheck.
const parsed = ts.readConfigFile(
  path.join(web, "tsconfig.json"),
  ts.sys.readFile,
);
const options = ts.convertCompilerOptionsFromJson(
  parsed.config.compilerOptions,
  web,
).options;
Object.assign(options, {
  noEmit: true,
  skipLibCheck: true,
  incremental: false,
  allowNonTsExtensions: true,
});
const host = ts.createCompilerHost(options),
  read = host.readFile.bind(host),
  exists = host.fileExists.bind(host);
host.readFile = (f) => virtual.get(path.normalize(f)) ?? read(f);
host.fileExists = (f) => virtual.has(path.normalize(f)) || exists(f);
const api = path.join(web, "src/views/co/api.ts");
const program = ts.createProgram({
  rootNames: [
    api,
    ...virtual.keys(),
    path.join(web, "src/types/global.d.ts"),
    path.join(web, "env.d.ts"),
  ].filter((f) => exists(f) || virtual.has(f)),
  options,
  host,
});
const diagnostics = ts
  .getPreEmitDiagnostics(program)
  .filter(
    (d) =>
      d.file &&
      (d.file.fileName.includes("views/co/") ||
        d.file.fileName.includes("views\\co\\")),
  );
if (diagnostics.length) {
  process.stderr.write(
    ts.formatDiagnosticsWithColorAndContext(diagnostics, {
      getCurrentDirectory: () => root,
      getCanonicalFileName: (f) => f,
      getNewLine: () => "\n",
    }),
  );
  process.exit(1);
}
process.stdout.write("OK targeted co API/SFC script typecheck\n");
const contract = JSON.parse(
  fs.readFileSync(
    path.join(root, "docs/项目文档v2.2/编排O0/openapi.json"),
    "utf8",
  ),
);
const ajv = new Ajv({ strict: false });
const validators = {};
for (const [name, schema] of Object.entries(contract.components.schemas))
  ajv.addSchema(schema, "#/components/schemas/" + name);
function validate(name, v) {
  const check = (validators[name] ||= ajv.getSchema(
    "#/components/schemas/" + name,
  ));
  assert.ok(check(v), name + ": " + JSON.stringify(check.errors));
}
validate("SampleRequest", { expected_revision: 2, evidence_id: "live-test" });
validate("SampleRequest", {
  expected_revision: 2,
  input: { url: "https://fixture.example.test" },
  snapshots: [
    {
      url: "https://fixture.example.test",
      format: "html",
      content: "<main>test</main>",
    },
  ],
  expected_outputs: [],
});
assert.equal(
  ajv.getSchema("#/components/schemas/SampleRequest")({
    expected_revision: 2,
    evidence_id: "test",
    input: {},
  }),
  false,
);
validate("Operation", {
  id: "test",
  resource_id: "collector",
  operation: "testCollector",
  status: "succeeded",
  result: { formal_writes: 0 },
  error: {},
  created_at: "2026-10-13T00:00:00Z",
});
for (const item of Object.values(contract.paths))
  for (const op of Object.values(item))
    assert.equal(
      op["x-implementation-status"],
      ["O1","O2","O3","O4","O5"].includes(op["x-delivery-phase"]) ? "implemented" : "planned",
    );
const router = fs.readFileSync(path.join(web, "src/router/route.ts"), "utf8");
assert.ok(router.includes("path: 'co/collectors/:id'"));
const workbench = fs.readFileSync(
  path.join(web, "src/views/co/workbench.vue"),
  "utf8",
);
assert.ok(!/\b(?:window\.)?(alert|prompt)\s*\(/.test(workbench));
assert.ok(workbench.includes("onBeforeRouteLeave"));
assert.ok(workbench.includes("selectionApi"));
process.stdout.write(
  "OK O1/O2/O3/O4 contract, new-domain isolation, routes and template controls\n",
);

validate("TransformPreviewRequest", {
 transform:{version:"co.transform.v1",input:{key:"value",type:"string",required:true,nullable:false,multiple:false},expression:'Number(value.trim())',samples:[{value:" 1 ",expected:1}]},
 value:" 2 ",target:{key:"rating",type:"number",required:true,nullable:false,multiple:false}
});
const transformSchema=ajv.getSchema("#/components/schemas/Transform");
const baseTransform={version:"co.transform.v1",input:{key:"value",type:"string",required:true,nullable:false,multiple:false},expression:"value",samples:[{value:"a",expected:"a"}]};
assert.ok(transformSchema(baseTransform));
assert.equal(transformSchema({...baseTransform,input:{...baseTransform.input,key:"other"}}),false);
assert.equal(transformSchema({...baseTransform,input:{...baseTransform.input,default:"a"}}),false);
assert.equal(transformSchema({...baseTransform,samples:[]}),false);
process.stdout.write("OK O5-05 typed transform and sample contract\n");
const delivery={id:'download',port:'records',plugin_code:'aria2',version:'co.delivery.aria2.v1',emit:'changed',mapping:{url:{kind:'field',path:'output.url'}}};
validate('DeliveryBinding',delivery);
validate('DeliveryBinding',{...delivery,mapping:{url:{kind:'constant',value:'https://example.test/file'},dir:{kind:'constant',value:'/downloads'}}});
const deliverySchema=ajv.getSchema('#/components/schemas/DeliveryBinding');
assert.equal(deliverySchema({...delivery,plugin_code:'cloud-driver'}),false);
assert.equal(deliverySchema({...delivery,mapping:{url:{kind:'template',template:'{{output.url}}'}}}),false);
assert.equal(deliverySchema({...delivery,mapping:{url:delivery.mapping.url,options:{kind:'constant',value:{}}}}),false);
validate('DeliveryControlRequest',{delivery_id:'fixture',action:'query',expected_revision:2,confirmed:true});
assert.equal(ajv.getSchema('#/components/schemas/DeliveryControlRequest')({delivery_id:'fixture',action:'retry',expected_revision:2,confirmed:false}),false);
process.stdout.write('OK O5-06 delivery profile, mapping and confirmed control contract\n');

validate('AIPrepareRequest',{kind:'selectors',resource_id:'collector',expected_revision:1,sample_id:'sample',port:'records'});
validate('AIPrepareRequest',{kind:'mapping',resource_id:'task',expected_revision:1,sample_id:'sample',edge_id:'edge',output_index:0});
validate('AIControlRequest',{confirmed:true,expected_revision:1,evidence_hash:'fixture'});
assert.equal(ajv.getSchema('#/components/schemas/AIControlRequest')({confirmed:false}),false);
assert.equal(ajv.getSchema('#/components/schemas/AIPrepareRequest')({kind:'run_code',resource_id:'fixture',expected_revision:1}),false);
for(const action of ['call','validate','accept','apply','reject'])assert.equal(contract.paths['/ai/suggestions/{suggestion_id}/'+action].post['x-implementation-status'],'implemented');
const aiUI=fs.readFileSync(path.join(web,'src/views/co/ai-assist.vue'),'utf8');
assert.ok(!/\b(?:window\.)?(alert|prompt)\s*\(/.test(aiUI));
assert.ok(aiUI.includes("emit('applied')")&&aiUI.includes("control('accept')"));
process.stdout.write('OK O5-07 explicit AI preview, confirmation and candidate adoption contract\n');

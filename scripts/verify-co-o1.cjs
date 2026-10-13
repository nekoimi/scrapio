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
  "web/src/views/co/collectors.vue",
  "web/src/views/co/workbench.vue",
 "web/src/views/co/tasks.vue",
 "web/src/views/co/task-edit.vue",
 "web/src/views/co/runs.vue",
 "web/src/views/co/run-detail.vue",
 "web/src/views/co/data.vue",
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
      ["O1","O2"].includes(op["x-delivery-phase"]) ? "implemented" : "planned",
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
  "OK O1/O2 contract, planned O3+ isolation, routes and template controls\n",
);

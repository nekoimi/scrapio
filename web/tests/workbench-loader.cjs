const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const stateOutput = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../src/views/app/browser-state.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const stateModule = { exports: {} };
vm.runInNewContext(stateOutput, { module: stateModule, exports: stateModule.exports });
const output = ts.transpileModule(fs.readFileSync(path.join(__dirname, '../src/views/app/browser-workbench.ts'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const target = { exports: {} };
vm.runInNewContext(output, { module: target, exports: target.exports, setTimeout, clearTimeout });
module.exports = { ...target.exports, ...stateModule.exports };

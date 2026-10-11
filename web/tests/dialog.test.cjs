const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), ts = require('typescript');
function load(messageBox) {
 const filename=path.join(__dirname,'../src/utils/dialog.ts');
 const script=ts.transpileModule(fs.readFileSync(filename,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
 const module={exports:{}};
 vm.runInNewContext(script,{module,exports:module.exports,require:()=>({ElMessageBox:messageBox})});
 return module.exports;
}
test('template confirmation waits for a decision and rejects duplicate dialog attempts',async()=>{
 let resolve,options;
 const dialog=load({confirm:(message,title,input)=>{options=input;return new Promise(done=>{resolve=done;});}});
 let finished=false;const first=dialog.confirmAction('删除字段？').then(value=>{finished=true;return value;});
 await Promise.resolve();assert.equal(finished,false);
 assert.equal(await dialog.confirmAction('重复删除？'),false);
 assert.equal(options.closeOnClickModal,false);assert.equal(options.cancelButtonText,'取消');
 resolve('confirm');assert.equal(await first,true);
});
test('cancel and close stop the action, release the dialog lock, and do not hide real errors',async()=>{
 let result='cancel';const dialog=load({confirm:async()=>{if(result==='confirm')return result;throw result;}});
 assert.equal(await dialog.confirmAction('继续？'),false);
 result='close';assert.equal(await dialog.confirmAction('继续？'),false);
 result='confirm';assert.equal(await dialog.confirmAction('继续？'),true);
 result=new Error('组件错误');await assert.rejects(dialog.confirmAction('继续？'),/组件错误/);
 result='confirm';assert.equal(await dialog.confirmAction('继续？'),true);
});
test('template input retains its default, validates blank input and handles cancellation explicitly',async()=>{
 let options;const dialog=load({prompt:async(message,title,input)=>{options=input;return {value:'  详情字段  ',action:'confirm'};}});
 assert.equal(await dialog.promptValue('字段名称','field_1'),'详情字段');
 assert.equal(options.inputValue,'field_1');assert.equal(options.inputValidator('  '),'请输入内容');
 const cancelled=load({prompt:async()=>{throw 'cancel';}});
 assert.equal(await cancelled.promptValue('字段名称'),null);
});

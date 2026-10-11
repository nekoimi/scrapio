const test = require('node:test'), assert = require('node:assert/strict');
const {editorProblem,recordConfigProblem}=require('./workbench-loader.cjs');
test('legacy invalid arguments identify the action and admit missing cause details',()=>{
 const result=editorProblem({error:{code:'INVALID_ARGUMENT',message:'检查参数无效',stage:'record-preview:list'},request_id:'req-123'},'打开详情前检查列表记录与详情入口');
 assert.match(result.title,/打开详情前/);assert.match(result.reason,/尚未细分/);assert.match(result.recovery,/版本一致/);
 assert.equal(result.requestID,'req-123');assert.equal(result.stage,'record-preview:list');
 const local=editorProblem({error:{code:'INVALID_ARGUMENT',message:'分页参数无效',path:'offset'}},'读取 DOM');
 assert.equal(local.reason,'分页参数无效');assert.equal(local.path,'offset');
});
test('axios wrapped errors retain field path and request identity; plain network errors do not claim success',()=>{
 const result=editorProblem({response:{data:{error:{code:'INVALID_DETAIL_RULE',message:'详情预算无效',path:'config.detail.max_details'},request_id:'request'}}},'检查详情候选');
 assert.equal(result.path,'config.detail.max_details');assert.equal(result.requestID,'request');assert.match(result.recovery,/1 至 5/);
 const network=editorProblem({code:'ECONNABORTED',message:'timeout of 8000ms exceeded'},'打开详情');
 assert.match(network.reason,/尚未确认/);assert.match(network.recovery,/回执/);
});
test('configuration validation names the invalid field or budget without reducing valid run limits',()=>{
 const plan={mode:'single',max_records:200,fields:[{name:'标题',extract:'text',locator:{strategy:'css',expression:'.title'}}],detail_fields:[]};
 assert.equal(recordConfigProblem(plan),undefined);
 plan.detail={locator:{strategy:'css',expression:'a'},max_details:10,return_strategy:'back'};
 assert.equal(recordConfigProblem(plan).path,'config.detail.max_details');
 plan.detail.max_details=3;plan.fields[0].name='电影 名称';assert.match(recordConfigProblem(plan).message,/电影 名称/);
 plan.fields[0].name='标题';plan.fields[0].locator.expression='';assert.equal(recordConfigProblem(plan).path,'config.fields[0].locator');
});

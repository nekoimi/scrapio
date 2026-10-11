// Offline component setup checks: no browser service, server, or full build.
const test=require('node:test'), assert=require('node:assert/strict');
const fs=require('node:fs'), path=require('node:path'), vm=require('node:vm');
const vue=require('vue'), ts=require('typescript');
const {parse,compileScript,compileTemplate}=require('@vue/compiler-sfc');
const tick=async()=>{for(let i=0;i<5;i++){await Promise.resolve();await vue.nextTick();}};
const field=(name='title')=>({name,field_key:`key-${name}`,extract:'text',locator:{strategy:'css',expression:'.title'},type:'string',required:true});
const plan=()=>({mode:'single',max_records:10,fields:[field()],detail_fields:[]});
function mount(filename,initial,api={},confirm=()=>true){
 filename=path.join(__dirname,'../src/views/app/',filename);
 const {descriptor,errors}=parse(fs.readFileSync(filename,'utf8'),{filename});assert.equal(errors.length,0);
 const script=compileScript(descriptor,{id:'record-test'});
 const template=compileTemplate({source:descriptor.template.content,filename,id:'record-test',compilerOptions:{bindingMetadata:script.bindings}});assert.equal(template.errors.length,0);
 const output=ts.transpileModule(script.content,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText;
 const module={exports:{}}; const events=[],navigations=[],props=vue.reactive(initial);
 vm.runInNewContext(output,{module,exports:module.exports,require:name=>name==='vue'?vue:name==='./api'?{appApi:api}:{},window:{confirm},
  sessionStorage:{getItem:()=>null,setItem(){},removeItem(){}},crypto:require('node:crypto'),URL,document:{getElementById:()=>null}});
 const component=module.exports.default;let state;
 const setup=component.setup; component.setup=(props,context)=>{state=setup(props,context);return state;};component.render=()=>vue.h('div');
 const node=()=>({children:[]});
 const renderer=vue.createRenderer({createElement:node,createText:node,createComment:node,insert:(child,parent)=>parent.children.push(child),remove(){},setElementText(){},setText(){},patchProp(){},parentNode:()=>null,nextSibling:()=>null});
 const app=renderer.createApp({render:()=>vue.h(component,{...props,onSelect:(...args)=>events.push(['select',...args]),onPreviewRequested:()=>events.push(['preview']),onDraftSaved:draft=>{events.push(['saved',draft]);props.draft=draft;},onInspectPath:target=>events.push(['path',target]),onReturnList:()=>navigations.push('return')})});app.mount(node());
 return {props,events,navigations,get state(){return state;},unmount:()=>app.unmount()};
}
function rules(extra={},confirm){return mount('record-rule-editor.vue',{plan:plan(),available:true,disabled:false,detailPage:false,canPreview:true,canReturn:true,previewStale:true,...extra},{},confirm);}
function designer(api={},extra={},confirm){
 return mount('record-designer.vue',{draft:{id:'collector',name:'Movies',revision:1,definition:{steps:[{step_id:'records',type:'record_set',config:plan()}]}},session:{session_id:'session',page_state_id:'page',available:true},blocked:false,enabled:true,execute:async()=>{throw new Error('Unexpected website action');},view:'rules',...extra},api,confirm);
}

function detailWorkflow({status='succeeded',candidates=true,confirm=()=>true,redirectToList=false}={}){
 const calls=[];let h;
 const api={updateCollector:async(_,input)=>{calls.push('save');return {...h.props.draft,revision:h.props.draft.revision+1,definition:input.definition};},
  previewRecords:async(_,page,revision,step,stage)=>{calls.push(`preview-${stage}`);return {page_state_id:page,stage,records:[{index:0,values:{},fields:[]}],current_url:h.props.session.current_url,
   detail_urls:stage==='list' && candidates?[{url:'https://example.test/movie/1',raw_url:'/movie/1',record_index:0}]:[],next_pages:[],warnings:[]};}};
 h=designer(api,{session:{session_id:'session',page_state_id:'page',available:true,current_url:'https://example.test/list'},execute:async action=>{
  calls.push(action.type);if(status==='succeeded')h.props.session={...h.props.session,current_url:action.type==='back'||redirectToList?'https://example.test/list':action.value,page_state_id:action.type==='back'?'returned':'detail'};
  await tick();return {status,result:{final_url:h.props.session.current_url}};
 }},confirm);
 h.state.enableDetail();h.state.plan.value.fields=[];
 return {h,calls};
}
test('opening an example saves the entry, navigates before selecting and stores fields in detail scope',async()=>{
 const {h,calls}=detailWorkflow();try{
  await h.state.openExampleDetail();
  assert.deepEqual(calls,['save','preview-list','navigate','preview-detail']);
  assert.equal(h.state.context.value.listURL,'https://example.test/list');assert.equal(h.state.context.value.detailURL,'https://example.test/movie/1');
  assert.equal(h.state.selectionBlocked.value,false);assert.equal(h.events.filter(event=>event[0]==='select').length,1);
  assert.equal(h.state.applySelection({target:'field',name:'description',extract:'text',locator:{strategy:'css',expression:'.description'}}),true);
  assert.equal(h.state.plan.value.fields.length,0);assert.equal(h.state.plan.value.detail_fields[0].name,'description');
  await h.state.save();await tick();await h.state.returnToList();
  assert.equal(h.state.context.value,undefined);assert.equal(h.props.session.current_url,'https://example.test/list');
  assert.equal(h.state.plan.value.detail_fields[0].name,'description');assert.equal(h.state.selectionBlocked.value,false);
 }finally{h.unmount();}
});
test('failed or unconfirmed detail navigation never enables field selection or accepts a field',async()=>{
 for(const status of ['failed','uncertain']){
  const {h,calls}=detailWorkflow({status});try{
   await h.state.openExampleDetail();assert.equal(h.state.selectionBlocked.value,true);
   assert.equal(h.props.session.current_url,'https://example.test/list');assert.equal(calls.includes('preview-detail'),false);
   assert.equal(h.events.filter(event=>event[0]==='select').length,0);
   assert.equal(h.state.applySelection({target:'field',name:'bad',locator:{strategy:'css',expression:'.list'}}),false);
   assert.equal(h.state.plan.value.detail_fields.length,0);
  }finally{h.unmount();}
 }
});
test('a successful navigation receipt that redirects to the list cannot enable detail field selection',async()=>{
 const {h,calls}=detailWorkflow({redirectToList:true});try{
  await h.state.openExampleDetail();assert.equal(h.state.selectionBlocked.value,true);
  assert.equal(calls.includes('preview-detail'),false);assert.match(h.state.message.value,/列表地址/);
  assert.equal(h.events.filter(event=>event[0]==='select').length,0);
 }finally{h.unmount();}
});
test('missing candidates or declined navigation leave the list without selecting detail fields',async()=>{
 for(const options of [{candidates:false},{confirm:()=>false}]){
  const {h,calls}=detailWorkflow(options);try{
   await h.state.openExampleDetail();assert.equal(calls.includes('navigate'),false);assert.equal(h.state.context.value,undefined);
   assert.equal(h.events.filter(event=>event[0]==='select').length,0);
  }finally{h.unmount();}
 }
});
test('leaving the example detail page blocks field selection until its page is restored',async()=>{
 const {h}=detailWorkflow();try{
  await h.state.openExampleDetail();h.props.session.current_url='https://example.test/list';await tick();
  assert.equal(h.state.selectionBlocked.value,true);const before=h.events.filter(event=>event[0]==='select').length;
  h.state.beginSelection('field');assert.equal(h.events.filter(event=>event[0]==='select').length,before);
  assert.equal(h.state.applySelection({target:'field',name:'bad',locator:{strategy:'css',expression:'.list'}}),false);
  h.props.session.current_url=h.state.context.value.detailURL;await tick();assert.equal(h.state.selectionBlocked.value,false);
 }finally{h.unmount();}
});
test('detail node cannot emit field selection while the webpage is still the list',async()=>{
 const h=rules();try{
  h.state.activeNode.value='detail';h.state.select('field');assert.equal(h.events.length,0);
  h.props.detailPage=true;await tick();h.state.select('field');assert.equal(h.events[0][0],'select');
  h.state.activeNode.value='fields';h.state.select('field');assert.equal(h.events.length,1);
 }finally{h.unmount();}
});
test('node switching preserves field edits and never navigates or executes a webpage',async()=>{
 const h=rules();try{
  assert.equal(h.state.activeNode.value,'overview');h.state.activeNode.value='fields';h.state.editField(h.props.plan.fields[0]);
  h.state.editingField.value.name='电影名称';h.state.activeNode.value='next';await tick();h.state.activeNode.value='fields';
  assert.equal(h.props.plan.fields[0].name,'电影名称');assert.equal(h.state.editingField.value.name,'电影名称');assert.equal(h.events.length,0);assert.equal(h.navigations.length,0);
 }finally{h.unmount();}
});
test('saved plan refresh keeps the active field by stable key; explicit step reset returns to overview',async()=>{
 const h=rules();try{
  h.state.activeNode.value='fields';h.state.editField(h.props.plan.fields[0]);
  h.props.plan={...plan(),fields:[{...field(),name:'电影名称'}]};await tick();
  assert.equal(h.state.activeNode.value,'fields');assert.equal(h.state.editingField.value.name,'电影名称');
  h.state.resetNode();assert.equal(h.state.activeNode.value,'overview');assert.equal(h.state.editingField.value,undefined);
 }finally{h.unmount();}
});
test('compact fields show only matching preview stage, and stale samples never claim current verification',async()=>{
 const preview={stage:'list',records:[{values:{title:'电影'},fields:[{name:'title',match_count:1,errors:[],truncated:false}]}]};
 const h=rules({preview});try{
  h.state.activeNode.value='fields';const f=h.props.plan.fields[0];assert.equal(h.state.fieldSample(f),'电影');assert.match(h.state.fieldStatus(f),/已过期/);
  h.props.previewStale=false;await tick();assert.equal(h.state.fieldStatus(f),'当前页已检查');
  h.props.preview.stage='detail';assert.equal(h.state.fieldStatus(f),'待检查');assert.equal(h.state.fieldSample(f),'—');
 }finally{h.unmount();}
});
test('single active field editor, unique new names and reordering preserve object identity',()=>{
 const h=rules();try{
  h.state.activeNode.value='fields';h.state.addField();const first=h.props.plan.fields[0],second=h.state.editingField.value;
  assert.equal(h.props.plan.fields.length,2);assert.equal(h.state.editingField.value,second);
  h.state.moveField(-1);assert.equal(h.props.plan.fields[0],second);h.state.editField(first);assert.equal(h.state.editingField.value,first);
  h.state.focusSelection('field',second);assert.equal(h.state.activeNode.value,'fields');assert.equal(h.state.editingField.value,second);
 }finally{h.unmount();}
});
test('removing details explains related field loss, cancellation preserves the entire path',async()=>{
 let allowed=false,question='';const h=rules({plan:{...plan(),detail:{locator:{strategy:'css',expression:'a'},return_strategy:'back',max_details:3},detail_fields:[field('description')]}},text=>{question=text;return allowed;});
 try{
  h.state.removeDetail();assert.match(question,/1 个详情字段/);assert.ok(h.props.plan.detail);assert.equal(h.props.plan.detail_fields.length,1);
  allowed=true;h.props.detailPage=true;await tick();h.state.removeDetail();assert.ok(h.props.plan.detail);
  h.props.detailPage=false;await tick();h.state.removeDetail();assert.equal(h.props.plan.detail,undefined);assert.equal(h.props.plan.detail_fields.length,0);
 }finally{h.unmount();}
});
test('selection routes directly to its node and keeps unsaved fields when moving through the flow',()=>{
 const h=designer();try{
  const routed=[];h.state.ruleEditor.value={focusSelection:(...args)=>routed.push(args)};
  assert.equal(h.state.applySelection({target:'field',name:'rating',extract:'text',locator:{strategy:'css',expression:'.rating'}}),true);
  assert.equal(routed[0][0],'field');assert.equal(routed[0][1].name,'rating');assert.match(h.state.selectionNotice.value,/尚未保存/);assert.equal(h.state.localDirty.value,true);
  assert.equal(h.state.applySelection({target:'next',locator:{strategy:'css',expression:'.next'},next_kind:'click'}),true);
  assert.equal(routed[1][0],'next');assert.equal(h.state.plan.value.fields.length,2);assert.equal(h.state.summary.value.next,'已配置 · 可检查入口');
 }finally{h.unmount();}
});
test('reselect updates only existing field locator, preserving stable key and transformation settings',()=>{
 const h=designer();try{
  const original=h.state.plan.value.fields[0];h.state.prepareSelection('field',original);
  const ok=h.state.applySelection({target:'field',name:'other',extract:'image',locator:{strategy:'css',expression:'.new-title'}});
  assert.equal(ok,true);assert.equal(h.state.plan.value.fields.length,1);assert.equal(original.name,'title');assert.equal(original.field_key,'key-title');assert.equal(original.required,true);assert.equal(original.extract,'text');assert.equal(original.locator.expression,'.new-title');
  h.state.prepareSelection('field');h.state.applySelection({target:'field',name:'added',locator:{strategy:'css',expression:'.added'}});assert.equal(h.state.plan.value.fields.length,2);
 }finally{h.unmount();}
});
test('scope mismatch or replacement on a different page rejects selection without mutating fields',()=>{
 const h=designer();try{
  h.state.plan.value.mode='repeated';h.state.plan.value.locator={strategy:'css',expression:'.record'};
  assert.equal(h.state.applySelection({target:'field',name:'invalid',locator:{strategy:'css',expression:'.title'}}),false);assert.equal(h.state.plan.value.fields.length,1);
  const original=h.state.plan.value.fields[0];h.state.prepareSelection('field',original);
  h.state.context.value={sessionID:'session',stepID:'records',listURL:'https://example.test',strategy:'back',uncertain:false};
  assert.equal(h.state.applySelection({target:'field',locator:{strategy:'css',expression:'.description'}}),false);assert.equal(original.locator.expression,'.title');assert.match(h.state.message.value,/所属页面/);
 }finally{h.unmount();}
});
test('invalid save stays on configuration; viewing saved data does not issue another save',async()=>{
 let writes=0,reads=0;
 const h=designer({updateCollector:async()=>{writes++;throw new Error('must not save');},previewRecords:async()=>{reads++;return {page_state_id:'page',stage:'list',records:[],current_url:'https://example.test',detail_urls:[],next_pages:[],warnings:[]};}});
 try{
  h.state.plan.value.fields[0].locator.expression='';await h.state.saveAndPreview();assert.equal(h.events.filter(e=>e[0]==='preview').length,0);assert.equal(writes,0);
  h.state.undoLocal();await h.state.saveAndPreview();assert.equal(writes,0);assert.equal(reads,1);assert.equal(h.events.filter(e=>e[0]==='preview').length,1);
 }finally{h.unmount();}
});
test('switching to path candidates is read-only and unsaved detail edits block return navigation',async()=>{
 let reads=0;
 const h=designer({previewRecords:async()=>{reads++;return {page_state_id:'page',stage:'list',records:[],current_url:'https://example.test',detail_urls:[],next_pages:[],warnings:[]};}});
 try{
  await h.state.inspectPath('detail');assert.equal(reads,1);assert.equal(h.events.filter(e=>e[0]==='preview').length,1);
  h.state.context.value={sessionID:'session',stepID:'records',listURL:'https://example.test',strategy:'back',uncertain:false};
  h.state.plan.value.fields[0].name='edited';await h.state.returnToList();assert.equal(h.state.context.value.uncertain,false);
 }finally{h.unmount();}
});

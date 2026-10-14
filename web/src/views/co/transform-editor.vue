<template>
 <details class="co-transform">
  <summary>{{ scope }} · {{ modelValue ? '已启用 JS 表达式' : '不转换' }}</summary>
  <el-switch :model-value="!!modelValue" active-text="启用受控转换" @change="enable" />
  <template v-if="modelValue">
   <p>仅处理当前 value。提取/映射后先校验转换输入，再执行表达式；结果须为 {{ target.type }}{{ target.multiple ? '数组' : '单值' }}。不改变记录定位、分页或访问范围。</p>
   <div class="co-fields">
    <el-select v-model="modelValue.input.type" aria-label="转换前类型"><el-option v-for="t in types" :key="t" :value="t" :label="'输入 ' + t" /></el-select>
    <el-checkbox v-model="modelValue.input.multiple">输入数组</el-checkbox><el-checkbox v-model="modelValue.input.nullable">输入允许null</el-checkbox><el-checkbox v-model="modelValue.input.empty_allowed">输入允许空值</el-checkbox>
   </div>
   <el-input v-model="modelValue.expression" type="textarea" :rows="3" :maxlength="8192" placeholder="例如 Number(value.replace('分','').trim())" aria-label="JS转换表达式" />
   <p>白名单：String / Number / Boolean / parseInt / parseFloat、Math.abs/round/floor/ceil/min/max、trim / toLowerCase / toUpperCase / slice / substring / replace / split / join，条件及算术表达式。固定属性和索引可读取JSON；禁止循环、函数、正则和外部访问。</p>
   <el-form-item label="离线样例（1..5组 value 与 expected，JSON数组）">
    <el-input :model-value="sampleText" type="textarea" :rows="5" @input="setSamples" aria-label="转换样例和预期结果" />
   </el-form-item>
   <el-alert v-if="invalidSamples" :title="invalidSamples" type="error" :closable="false" />
   <el-button :loading="busy" :disabled="!!invalidSamples" @click="preview">校验全部样例并预览第一组</el-button>
   <el-alert v-if="problem" :title="problem" type="error" :closable="false" />
   <pre v-if="result">{{ JSON.stringify(result, null, 2) }}</pre>
   <p>表达式、输入Schema或预期值改变后须重新验证。采集器需重新实时测试/发布；任务连接需保存/发布新版本，既有运行仍使用冻结版本。</p>
  </template>
 </details>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { call, describe, type Field, type Transform } from './api';
const props=defineProps<{modelValue?:Transform;target:Field;scope:string}>();
const emit=defineEmits<{ 'update:modelValue':[Transform | undefined] }>();
const types=['string','url','number','integer','boolean','date','datetime','json'];
const busy=ref(false),problem=ref(''),result=ref<unknown>(),invalidSamples=ref(''),draft=ref<string>();
const sampleText=computed(()=>draft.value??JSON.stringify(props.modelValue?.samples||[],null,2));
function enable(enabled:unknown){
 draft.value=undefined;invalidSamples.value='';result.value=undefined;problem.value='';
 if(!enabled){emit('update:modelValue',undefined);return}
 const numeric=['integer','number'].includes(props.target.type);
 const convertNumeric=numeric && !props.target.multiple;
 const expression=convertNumeric?'Number(value.trim())':'value';
 const input={key:'value',type:convertNumeric?'string':props.target.type,required:true,nullable:props.target.nullable,multiple:props.target.multiple,empty_allowed:props.target.empty_allowed||false};
 let sample:unknown=convertNumeric?' 1 ':numeric?1:props.target.type==='boolean'?true:props.target.type==='json'?{text:'示例'}:props.target.type==='url'?'https://example.com/sample':props.target.type==='date'?'2026-10-14':props.target.type==='datetime'?'2026-10-14T00:00:00Z':'示例';
 if(props.target.multiple)sample=[sample];
 emit('update:modelValue',{version:'co.transform.v1',input,expression:props.target.multiple?'value':expression,samples:[{value:sample,expected:props.target.multiple?sample:numeric?1:sample}]});
}
function setSamples(text:string){
 draft.value=text;result.value=undefined;if(!props.modelValue)return;
 try{const samples=JSON.parse(text);if(!Array.isArray(samples)||!samples.length||samples.length>5||samples.some(s=>!s||!Object.hasOwn(s,'value')||!Object.hasOwn(s,'expected')))throw new Error('需要1..5组包含value和expected的JSON对象');props.modelValue.samples=samples;invalidSamples.value=''}
 catch(e){props.modelValue.samples=[];invalidSamples.value=String(e)}
}
async function preview(){
 if(!props.modelValue||invalidSamples.value)return;
 busy.value=true;problem.value='';result.value=undefined;
 const signature=JSON.stringify([props.modelValue,props.target]);
 try{const data=await call('/transforms/preview','post',{transform:props.modelValue,value:props.modelValue.samples[0]?.value,target:props.target});if(signature===JSON.stringify([props.modelValue,props.target]))result.value=data}
 catch(e){problem.value=describe(e)}finally{busy.value=false}
}
watch(()=>JSON.stringify([props.modelValue,props.target]),()=>{result.value=undefined;problem.value=''});
watch(()=>props.modelValue,()=>{draft.value=undefined;invalidSamples.value=''});
</script>

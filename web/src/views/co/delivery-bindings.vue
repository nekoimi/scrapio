<template>
	<details class="co-delivery-config">
		<summary>可选：保存结果后交付插件（{{ modelValue?.length || 0 }}）</summary>
		<p>先持久化输出与交付请求，再由独立worker提交。采集完成与交付状态分开；试采只预演，不调用外部服务。</p>
		<el-alert v-if="!configured" title="aria2尚未配置；可保存方案，正式交付会显示未提交，需要配置后安全重试。" type="info" :closable="false" />
		<div v-for="(b, i) in modelValue || []" :key="b.id" class="co-field-row">
			<div class="co-fields">
				<el-input v-model="b.id" placeholder="交付ID" aria-label="交付ID" />
				<el-tag>aria2 · 原请求回执</el-tag>
				<el-select v-model="b.port" @change="changePort(b)" aria-label="交付输出端口"
					><el-option v-for="p in ports" :key="p.port" :value="p.port" :label="p.port"
				/></el-select>
				<el-select v-model="b.emit" aria-label="交付触发策略"
					><el-option label="全部有效输出（每次新运行可能再交付）" value="valid" /><el-option label="仅新增" value="created" /><el-option
						label="新增或更新（推荐写表端口）"
						value="changed"
				/></el-select>
				<el-button link type="danger" @click="remove(i)">移除</el-button>
			</div>
			<el-form label-position="top">
				<el-alert
					v-if="passPorts.includes(b.port) && b.emit !== 'valid'"
					title="此端口为仅传递结果，请将交付触发策略改为全部有效输出。"
					type="warning"
					:closable="false"
				/>
				<el-form-item label="下载地址来源字段（支持http/https/magnet）">
					<el-select :model-value="sourceKey(b)" @change="(key: string) => mapURL(b, key)"
						><el-option v-for="f in sourceFields(b)" :key="f.key" :value="f.key" :label="f.key + ' · ' + f.type"
					/></el-select>
				</el-form-item>
				<el-form-item label="可选保存目录（aria2服务端目录；留空用服务配置）"
					><el-input :model-value="directory(b)" :maxlength="1024" @input="(value: string) => setDirectory(b, value)"
				/></el-form-item>
			</el-form>
			<p>回执成功表示aria2已接受请求，不代表下载完成。响应丢失时查询固定GID，回执缺失不会自动重发；在运行详情单独确认。</p>
		</div>
		<el-button :disabled="(modelValue?.length || 0) >= 5" @click="add">添加aria2交付</el-button>
	</details>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { call, type Definition, type Field } from './api';
import type { DeliveryBinding } from './tasks-api';
const props = defineProps<{ modelValue?: DeliveryBinding[]; definition: Definition; passPorts: string[] }>();
const emit = defineEmits<{ 'update:modelValue': [DeliveryBinding[]] }>();
const configured = ref(false);
const ports = computed(() => [
	props.definition.output,
	...(props.definition.additional_outputs || []).map((p: { output: Definition['output'] }) => p.output),
]);
function sourceFields(b: DeliveryBinding): Field[] {
	return (ports.value.find((p: Definition['output']) => p.port === b.port)?.fields || []).filter(
		(f: Field) => ['string', 'url'].includes(f.type) && !f.multiple && !f.nullable && f.required
	);
}
function sourceKey(b: DeliveryBinding) {
	return b.mapping.url?.kind === 'field' ? b.mapping.url.path?.slice(7) || '' : '';
}
function mapURL(b: DeliveryBinding, key: string) {
	b.mapping.url = { kind: 'field', path: 'output.' + key };
}
function directory(b: DeliveryBinding) {
	return b.mapping.dir?.kind === 'constant' ? String(b.mapping.dir.value || '') : '';
}
function setDirectory(b: DeliveryBinding, value: string) {
	if (value.trim()) b.mapping.dir = { kind: 'constant', value };
	else delete b.mapping.dir;
}
function changePort(b: DeliveryBinding) {
	b.mapping = {};
	if (props.passPorts.includes(b.port)) b.emit = 'valid';
	const f = sourceFields(b).find((f) => f.type === 'url') || sourceFields(b)[0];
	if (f) mapURL(b, f.key);
}
function add() {
	const all = [...(props.modelValue || [])];
	let n = 1;
	while (all.some((b) => b.id === 'deliver_' + n)) n++;
	const b: DeliveryBinding = {
		id: 'deliver_' + n,
		port: 'records',
		plugin_code: 'aria2',
		version: 'co.delivery.aria2.v1',
		emit: props.passPorts.includes('records') ? 'valid' : 'changed',
		mapping: {},
	};
	changePort(b);
	emit('update:modelValue', [...all, b]);
}
function remove(i: number) {
	emit(
		'update:modelValue',
		(props.modelValue || []).filter((_: DeliveryBinding, j: number) => j !== i)
	);
}
onMounted(async () => {
	try {
		const caps = await call<{ items: { code: string; configured: boolean }[] }>('/delivery-capabilities');
		configured.value = !!caps.items.find((p) => p.code === 'aria2')?.configured;
	} catch {
		configured.value = false;
	}
});
</script>

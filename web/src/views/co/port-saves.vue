<template>
	<el-divider v-if="ports.length">附加输出端口 · 逐一确认保存策略</el-divider>
	<el-card v-for="p in ports" :key="p.output.port">
		<template #header>{{ p.output.port }} · {{ p.output.fields.map((f) => f.key).join('、') }}</template>
		<p>独立 Schema 和输出身份；所有端口的本页结果一起提交，正式/试采运行均保留不可变输出。</p>
		<el-radio-group :model-value="binding(p.output.port).mode || 'table'" @change="setMode(p.output.port, String($event))">
			<el-radio-button label="pass_through">仅传递结果</el-radio-button><el-radio-button label="table">保存到数据表</el-radio-button>
		</el-radio-group>
		<template v-if="binding(p.output.port).mode !== 'pass_through'">
			<el-select :model-value="binding(p.output.port).table_id" placeholder="选择独立数据表" @change="bindTable(p.output.port, String($event))"
				><el-option v-for="t in tables" :key="t.id" :value="t.id" :label="t.name + ' · Schema ' + t.schema_version"
			/></el-select>
			<el-button :disabled="busy" @click="create(p.output.port)">按端口 Schema 新建表</el-button>
			<el-table v-if="table(p.output.port)" :data="table(p.output.port)!.schema.fields">
				<el-table-column prop="key" label="表字段" /><el-table-column label="输出字段"
					><template #default="{ row }"
						><el-tag v-if="binding(p.output.port).mapping?.[row.key]?.kind === 'constant'"
							>常量 {{ JSON.stringify(binding(p.output.port).mapping[row.key].value) }}</el-tag
						><el-select
							:model-value="binding(p.output.port).mapping?.[row.key]?.path?.slice(7) || ''"
							clearable
							@change="mapField(p.output.port, row.key, String($event))"
							><el-option v-for="f in p.output.fields" :key="f.key" :value="f.key" :label="f.key + ' · ' + f.type" /></el-select></template
				></el-table-column>
			</el-table>
			<el-select :model-value="binding(p.output.port).update_policy" @change="patch(p.output.port, { update_policy: String($event) })"
				><el-option value="merge_present" label="合并本次字段" /><el-option value="replace_present" label="替换本次字段"
			/></el-select>
			<el-select :model-value="binding(p.output.port).null_policy" @change="patch(p.output.port, { null_policy: String($event) })"
				><el-option value="keep" label="null保留旧值" /><el-option value="clear" label="null清空"
			/></el-select>
		</template>
		<el-button :disabled="busy" @click="preview(p.output.port)">预演此端口</el-button>
		<pre v-if="previews[p.output.port]">{{ JSON.stringify(previews[p.output.port], null, 2) }}</pre>
	</el-card>
	<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { coApi, describe, type Version, type Field } from './api';
import { taskApi, taskMutation, type TaskStage, type SaveBinding, type DataTable } from './tasks-api';
const props = defineProps<{ modelValue: NonNullable<TaskStage['additional_saves']>; version: Version; tables: DataTable[] }>();
const emit = defineEmits<{ 'update:modelValue': [NonNullable<TaskStage['additional_saves']>]; 'table-created': [DataTable] }>();
const ports = computed(() => props.version.definition.additional_outputs || []),
	busy = ref(false),
	problem = ref(''),
	previews = ref<Record<string, unknown>>({});
const empty = () => ({ mode: 'pass_through' }) as SaveBinding;
function binding(port: string): SaveBinding {
	return props.modelValue.find((p: { port: string; save: SaveBinding }) => p.port === port)?.save || empty();
}
function patch(port: string, part: Partial<SaveBinding>) {
	const rows = ports.value.map((p: any) => ({
		port: p.output.port,
		save: p.output.port === port ? { ...binding(port), ...part } : binding(p.output.port),
	}));
	emit('update:modelValue', rows);
	previews.value = {};
}
function setMode(port: string, mode: string) {
	const save =
		mode === 'pass_through'
			? empty()
			: ({
					mode: 'table',
					table_id: '',
					schema_version: 1,
					schema_hash: '',
					unique_key: [],
					mapping: {},
					update_policy: 'merge_present',
					null_policy: 'keep',
				} as SaveBinding);
	emit(
		'update:modelValue',
		ports.value.map((p: any) => ({ port: p.output.port, save: p.output.port === port ? save : binding(p.output.port) }))
	);
	previews.value = {};
}
function table(port: string) {
	return props.tables.find((t: DataTable) => t.id === binding(port).table_id);
}
function bindTable(port: string, id: string) {
	const t = props.tables.find((t: DataTable) => t.id === id),
		p = ports.value.find((p: any) => p.output.port === port);
	if (!t || !p) return;
	patch(port, {
		mode: 'table',
		table_id: id,
		schema_version: t.schema_version,
		schema_hash: t.schema_hash,
		unique_key: t.schema.unique_key,
		update_policy: 'merge_present',
		null_policy: 'keep',
		mapping: Object.fromEntries(
			t.schema.fields
				.filter((f: Field) => p.output.fields.some((o: any) => o.key === f.key))
				.map((f: Field) => [f.key, { kind: 'field', path: 'output.' + f.key }])
		),
	});
}
function mapField(port: string, key: string, value: string) {
	const m = { ...(binding(port).mapping || {}) };
	if (value) m[key] = { kind: 'field', path: 'output.' + value };
	else delete m[key];
	patch(port, { mapping: m });
}
async function guard(fn: () => Promise<void>) {
	busy.value = true;
	problem.value = '';
	try {
		await fn();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
async function create(port: string) {
	await guard(async () => {
		const p = ports.value.find((p: any) => p.output.port === port)!;
		const t = await taskMutation<DataTable>('createTable', '', '/tables', {
			name: props.version.collector_id?.slice(0, 8) + ' · ' + port,
			schema: { fields: p.output.fields, unique_key: p.output.identity_fields },
		});
		emit('table-created', t);
		patch(port, {
			mode: 'table',
			table_id: t.id,
			schema_version: t.schema_version,
			schema_hash: t.schema_hash,
			unique_key: t.schema.unique_key,
			mapping: Object.fromEntries(p.output.fields.map((f: any) => [f.key, { kind: 'field', path: 'output.' + f.key }])),
			update_policy: 'merge_present',
			null_policy: 'keep',
		});
	});
}
async function preview(port: string) {
	await guard(async () => {
		const test = (await coApi.tests(props.version.collector_id!)).items.find((t) => t.result?.definition_hash === props.version.definition_hash);
		if (!test) throw new Error('所选版本没有测试证据，请先测试');
		previews.value[port] = await taskApi.savePreview(
			props.version.id,
			binding(port),
			test.result.candidates.filter((c) => (c.port || 'records') === port).slice(0, 100),
			port
		);
	});
}
watch(
	() => props.version.id,
	() => {
		const rows = ports.value.map((p: any) => ({ port: p.output.port, save: binding(p.output.port) }));
		if (JSON.stringify(rows) !== JSON.stringify(props.modelValue)) emit('update:modelValue', rows);
		previews.value = {};
	},
	{ immediate: true }
);
</script>

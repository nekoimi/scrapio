<template>
	<div class="co-source-picker">
		<el-alert title="先预览并确认有限输入快照。后续运行复用原快照，不读取表最新值；要采集新数据请显式重新取样。" type="info" :closable="false" />
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
		<el-form label-position="top">
			<template v-if="kind === 'table'">
				<el-form-item label="来源数据表"
					><el-select v-model="tableID" @change="tableChanged"
						><el-option v-for="t in tables" :key="t.id" :label="t.name + ' · Schema ' + t.schema_version" :value="t.id" /></el-select
					><el-button v-if="cursor" link @click="loadTables">加载更多表</el-button></el-form-item
				>
				<el-form-item label="记录范围"
					><el-radio-group v-model="selection"
						><el-radio label="query">按字段条件查询</el-radio><el-radio label="selected">按已选记录ID</el-radio></el-radio-group
					></el-form-item
				>
				<el-form-item v-if="selection === 'query'" label="字段相等条件（JSON对象，{}表示全部；超出上限会阻断）"
					><el-input v-model="filterText" type="textarea" :rows="2"
				/></el-form-item>
				<el-form-item v-else label="记录ID（逗号或换行分隔）"><el-input v-model="recordIDs" type="textarea" :rows="3" /></el-form-item>
				<el-form-item label="本批最大行数"><el-input-number v-model="limit" :min="1" :max="1000" /></el-form-item>
			</template>
			<template v-else>
				<el-form-item label="文件格式"
					><el-select v-model="format"
						><el-option label="CSV（首行为列名，UTF-8）" value="csv" /><el-option label="JSON 对象数组" value="json" /></el-select
				></el-form-item>
				<el-form-item label="选择文件（最多768KiB、1000行）"
					><input type="file" accept=".csv,.json,text/csv,application/json" @change="readFile"
				/></el-form-item>
				<el-form-item label="文件内容"><el-input v-model="content" type="textarea" :rows="5" /></el-form-item>
				<el-button @click="detectColumns">读取列名</el-button>
			</template>
			<el-table :data="fields">
				<el-table-column prop="key" label="采集器输入" /><el-table-column prop="type" label="类型" width="100" />
				<el-table-column label="来源列"
					><template #default="{ row }"
						><el-select v-model="mapping[row.key]" clearable filterable allow-create default-first-option
							><el-option v-for="column in columns" :key="column" :label="column" :value="column" /></el-select></template
				></el-table-column>
			</el-table>
		</el-form>
		<div class="co-toolbar">
			<el-button :loading="busy" :disabled="!versionID" @click="preview">预览并冻结快照</el-button
			><el-button :disabled="busy" @click="recover">查询上次快照操作</el-button>
		</div>
		<template v-if="result">
			<p>指纹 {{ result.snapshot_hash }} · 总行数 {{ result.row_count }} · 有效不重复 {{ result.valid_count }}。预览前100行。</p>
			<el-table :data="result.items"
				><el-table-column prop="row" label="行号" width="70" /><el-table-column label="检查"
					><template #default="{ row }">{{
						row.error?.message || (row.duplicate_of ? '重复于第' + row.duplicate_of + '行' : '有效')
					}}</template></el-table-column
				><el-table-column label="输入快照"
					><template #default="{ row }"
						><code>{{ JSON.stringify(row.values) }}</code></template
					></el-table-column
				></el-table
			>
			<el-checkbox v-if="result.partial" v-model="acceptPartial">确认仅导入有效且不重复的行，保留其余行诊断</el-checkbox>
			<el-button type="primary" :disabled="busy || !result.valid_count || (result.partial && !acceptPartial)" @click="confirmSnapshot"
				>确认使用此输入快照</el-button
			>
		</template>
		<p v-if="modelValue.snapshot_id" class="co-muted">
			已确认 {{ modelValue.count }} 条 · {{ modelValue.snapshot_id }}<el-button link @click="viewFrozen">查看确认快照</el-button>
		</p>
	</div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { call, describe, type Field } from './api';
import { taskApi, taskMutation, recoverTaskMutation, type DataTable, type TaskSource } from './tasks-api';
const props = defineProps<{ modelValue: TaskSource; kind: 'table' | 'file'; versionID: string; fields: Field[] }>();
const emit = defineEmits<{ (e: 'update:modelValue', value: TaskSource): void }>();
const tables = ref<DataTable[]>([]),
	cursor = ref(''),
	tableID = ref(''),
	selection = ref('query'),
	filterText = ref('{}'),
	recordIDs = ref(''),
	limit = ref(100),
	format = ref('csv'),
	content = ref(''),
	fileColumns = ref<string[]>([]),
	mapping = ref<Record<string, string>>({}),
	busy = ref(false),
	problem = ref(''),
	result = ref<any>(),
	acceptPartial = ref(false),
	previewSignature = ref('');
const columns = computed(() =>
	props.kind === 'table' ? tables.value.find((t) => t.id === tableID.value)?.schema.fields.map((f: any) => f.key) || [] : fileColumns.value
);
const signature = () =>
	JSON.stringify([
		props.kind,
		props.versionID,
		tableID.value,
		selection.value,
		filterText.value,
		recordIDs.value,
		limit.value,
		format.value,
		content.value,
		mapping.value,
	]);
watch(signature, () => {
	result.value = undefined;
	acceptPartial.value = false;
});
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
async function loadTables() {
	await guard(async () => {
		const p = await taskApi.tables(cursor.value);
		tables.value.push(...p.items);
		cursor.value = p.next_cursor || '';
	});
}
function tableChanged() {
	mapping.value = Object.fromEntries(props.fields.filter((f: any) => columns.value.includes(f.key)).map((f: any) => [f.key, f.key]));
}
async function readFile(e: Event) {
	await guard(async () => {
		const file = (e.target as HTMLInputElement).files?.[0];
		if (!file) return;
		if (file.size > 768 * 1024) throw new Error('文件超过768KiB，请拆分');
		content.value = await file.text();
		format.value = file.name.toLowerCase().endsWith('.json') ? 'json' : 'csv';
		detectColumns();
	});
}
function detectColumns() {
	try {
		fileColumns.value = format.value === 'json' ? Object.keys(JSON.parse(content.value)[0] || {}) : parseHeader(content.value);
		mapping.value = Object.fromEntries(props.fields.filter((f: any) => fileColumns.value.includes(f.key)).map((f: any) => [f.key, f.key]));
		problem.value = '';
	} catch (e) {
		problem.value = describe(e);
	}
}
function parseHeader(text: string) {
	const match =
		text
			.replace(/^\uFEFF/, '')
			.split(/\r?\n/)[0]
			.match(/(?:"(?:[^"]|"")*"|[^,]+|(?<=,)(?=,))/g) || [];
	return match.map((v) => v.replace(/^"|"$/g, '').replace(/""/g, '"'));
}
function payload() {
	const t = tables.value.find((t) => t.id === tableID.value);
	const ids = recordIDs.value.split(/[,\s]+/).filter(Boolean);
	if (props.kind === 'table' && selection.value === 'selected' && !ids.length) throw new Error('请先选择记录');
	return {
		kind: props.kind,
		collector_version_id: props.versionID,
		mapping: Object.fromEntries(Object.entries(mapping.value).filter(([, v]) => v)),
		...(props.kind === 'table'
			? {
					table_id: tableID.value,
					schema_version: t?.schema_version,
					limit: limit.value,
					record_ids: selection.value === 'selected' ? ids : [],
					filter: selection.value === 'query' ? JSON.parse(filterText.value) : {},
				}
			: { format: format.value, content: content.value }),
	};
}
async function preview() {
	await guard(async () => {
		const sig = signature();
		const r = await taskMutation('snapshotSource', '', '/sources/preview', payload());
		if (signature() !== sig) throw new Error('预览期间配置已改变；快照已保留，请重新预览');
		result.value = r;
		previewSignature.value = sig;
	});
}
function confirmSnapshot() {
	if (signature() !== previewSignature.value) {
		problem.value = '来源配置已改变，请重新预览';
		return;
	}
	emit('update:modelValue', {
		kind: props.kind,
		inputs: [],
		snapshot_id: result.value.snapshot_id,
		snapshot_hash: result.value.snapshot_hash,
		count: result.value.valid_count,
		accept_partial: acceptPartial.value,
	});
}
async function recover() {
	await guard(async () => {
		const r = await recoverTaskMutation('snapshotSource', '');
		if (r) {
			result.value = r;
			previewSignature.value = '';
			problem.value = '已恢复原快照。请核对配置后重新预览，再确认使用，防止错用输入。';
		}
	});
}
async function viewFrozen() {
	await guard(async () => {
		const r = await call<any>('/sources/' + props.modelValue.snapshot_id + '?limit=100');
		result.value = { ...r, snapshot_id: r.id, partial: r.valid_count !== r.row_count };
		previewSignature.value = '';
		problem.value = r.expired ? '该证据已过期，请重新取样' : '这是已确认快照；新来源参数需重新预览才能重新确认';
	});
}
onMounted(async () => {
	await loadTables();
	const raw = sessionStorage.getItem('co-source-handoff');
	if (raw) {
		sessionStorage.removeItem('co-source-handoff');
		try {
			const v = JSON.parse(raw);
			tableID.value = v.table_id;
			recordIDs.value = v.record_ids.join('\n');
			selection.value = 'selected';
			if (!tables.value.some((t) => t.id === v.table_id)) tables.value.push(await call('/tables/' + v.table_id));
			tableChanged();
		} catch (e) {
			problem.value = describe(e);
		}
	}
});
</script>

<template>
	<section class="app-card">
		<h2>筛选与视图</h2>
		<div class="editor-actions">
			<label
				>常用视图<select v-model="viewID" :disabled="busy || blocked || !!pending" @change="chooseView">
					<option value="">当前自定义条件</option>
					<option v-for="v in views" :key="v.view_id" :value="v.view_id">{{ v.name }}</option>
				</select></label
			><button :disabled="busy || blocked" @click="reloadViews">刷新视图</button>
		</div>
		<div v-for="(f, i) in filters" :key="i" class="query-filter">
			<label
				>字段<select
					v-model="f.field"
					:disabled="busy || blocked"
					@change="
						f.op = operators(f.field)[0];
						f.value = '';
					"
				>
					<option v-for="field in schema.fields" :key="field.field_key" :value="field.field_key">{{ field.name }}（{{ field.type }}）</option>
				</select></label
			>
			<label
				>条件<select v-model="f.op" :disabled="busy || blocked">
					<option v-for="o in operators(f.field)" :key="o" :value="o">{{ labels[o] }}</option>
				</select></label
			>
			<label v-if="!['empty', 'not_empty'].includes(f.op)"
				>值<input
					v-model="f.value"
					:placeholder="fieldType(f.field) === 'boolean' ? 'true 或 false' : '输入值，数值按文本输入保留精度'"
					:disabled="busy || blocked"
			/></label>
			<button :disabled="busy || blocked" @click="filters.splice(i, 1)">移除</button>
		</div>
		<button :disabled="busy || blocked || filters.length >= 8" @click="addFilter">添加条件（全部满足）</button>
		<div class="editor-actions">
			<label
				>排序<select v-model="sortField" :disabled="busy || blocked">
					<option value="@first_observed_at">首次观察</option>
					<option value="@last_observed_at">最近观察</option>
					<option value="@revision">修订次数</option>
					<option v-for="f in sortable" :key="f.field_key" :value="f.field_key">{{ f.name }}</option>
				</select></label
			><label
				>方向<select v-model="direction" :disabled="busy || blocked">
					<option value="desc">降序</option>
					<option value="asc">升序</option>
				</select></label
			>
		</div>
		<details>
			<summary>显示和导出的列（至少一列）</summary>
			<label v-for="f in schema.fields" :key="f.field_key"
				><input v-model="columns" type="checkbox" :value="f.field_key" :disabled="busy || blocked" />{{ f.name }}</label
			>
		</details>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !columns.length" @click="apply">应用条件</button
			><button :disabled="busy || blocked" @click="reset">重置并查询</button
			><span v-if="dirty" class="status-warn">条件未应用，当前表格与导出仍使用上次查询</span>
		</div>
		<p class="muted">
			最多 8 个 AND 条件；文本包含区分大小写，多值/JSON 支持空值检查。快照最多 10000 条/16 MiB，超限请缩小筛选。修订与来源在记录详情中查看。
		</p>
		<label>视图名称<input v-model="viewName" maxlength="80" :disabled="busy || blocked" /></label>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !viewName.trim() || !columns.length || !!pending" @click="save(false)">保存为新视图</button
			><button v-if="selectedView" :disabled="busy || blocked || !!pending" @click="save(true)">更新此视图</button
			><button v-if="selectedView" :disabled="busy || blocked" @click="removeView">删除此视图</button
			><button v-if="pending" :disabled="busy || blocked" @click="recover">查询并重试原保存</button
			><button v-if="pending" :disabled="busy || blocked" @click="discardPending">结束本地等待</button>
		</div>
		<details v-if="pending">
			<summary>未确认的原视图保存</summary>
			<pre>{{ JSON.stringify(pending, null, 2) }}</pre>
		</details>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { appApi, type DataView, type RecordQuery, type TableSchema } from './api';
import { readError } from './data-read';
const props = defineProps<{ tableId: string; schema: TableSchema; query: RecordQuery; blocked: boolean }>();
const emit = defineEmits<{ (e: 'apply', q: RecordQuery): void }>();
const filters = ref<{ field: string; op: string; value: string }[]>([]),
	sortField = ref('@first_observed_at'),
	direction = ref<'asc' | 'desc'>('desc'),
	columns = ref<string[]>([]),
	views = ref<DataView[]>([]),
	viewID = ref(''),
	viewName = ref(''),
	busy = ref(false),
	message = ref(''),
	pending = ref<{ id: string; name: string; expected_revision: number; query: RecordQuery }>();
let epoch = 0;
const labels: Record<string, string> = {
	eq: '等于',
	ne: '不等于',
	contains: '包含',
	gt: '大于',
	gte: '大于等于',
	lt: '小于',
	lte: '小于等于',
	empty: '为空',
	not_empty: '不为空',
};
const selectedView = computed(() => views.value.find((v) => v.view_id === viewID.value));
const sortable = computed(() => props.schema.fields.filter((f) => !f.multiple && f.type !== 'json'));
const fieldType = (key: string) => props.schema.fields.find((f) => f.field_key === key)?.type;
function operators(key: string) {
	const f = props.schema.fields.find((f) => f.field_key === key);
	if (f?.multiple || f?.type === 'json') return ['empty', 'not_empty'];
	return [
		'eq',
		'ne',
		...(f?.type === 'string' ? ['contains'] : []),
		...(['integer', 'number', 'date', 'datetime'].includes(f?.type || '') ? ['gt', 'gte', 'lt', 'lte'] : []),
		'empty',
		'not_empty',
	];
}
function query(): RecordQuery {
	return {
		filters: filters.value.map((f) => ({
			field: f.field,
			op: f.op,
			...(['empty', 'not_empty'].includes(f.op)
				? {}
				: { value: fieldType(f.field) === 'boolean' ? (f.value === 'true' ? true : f.value === 'false' ? false : f.value) : f.value }),
		})),
		sort: { field: sortField.value, direction: direction.value },
		columns: [...columns.value],
	};
}
const dirty = computed(() => JSON.stringify(query()) !== JSON.stringify(props.query));
function restore(q: RecordQuery) {
	filters.value = q.filters.map((f) => ({ field: f.field, op: f.op, value: f.value === undefined ? '' : String(f.value) }));
	sortField.value = q.sort.field || '@first_observed_at';
	direction.value = q.sort.direction || 'desc';
	columns.value = q.columns.length ? [...q.columns] : props.schema.fields.map((f) => f.field_key);
}
function addFilter() {
	const f = props.schema.fields[0];
	if (f) filters.value.push({ field: f.field_key, op: f.multiple || f.type === 'json' ? 'empty' : 'eq', value: '' });
}
function apply() {
	emit('apply', query());
}
function reset() {
	viewID.value = '';
	viewName.value = '';
	restore({ filters: [], sort: { field: '@first_observed_at', direction: 'desc' }, columns: props.schema.fields.map((f) => f.field_key) });
	apply();
}
function chooseView() {
	const v = selectedView.value;
	if (v) {
		viewName.value = v.name;
		restore(v.query);
		apply();
	}
}
const storage = () => `scrapio:v22:view:${props.tableId}`;
async function reloadViews() {
	const token = epoch;
	try {
		const r = await appApi.dataViews(props.tableId);
		if (token === epoch) views.value = r.items;
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	}
}
async function perform(input: NonNullable<typeof pending.value>) {
	busy.value = true;
	pending.value = input;
	sessionStorage.setItem(storage(), JSON.stringify(input));
	const token = epoch;
	try {
		const v = await appApi.saveDataView(props.tableId, input);
		if (token !== epoch) return;
		pending.value = undefined;
		sessionStorage.removeItem(storage());
		viewID.value = v.view_id;
		viewName.value = v.name;
		message.value = '视图已保存；包含条件、排序和列，不复制正式数据。';
		await reloadViews();
	} catch (cause) {
		if (token === epoch) message.value = readError(cause) + '；本地条件和原请求已保留。';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function save(update: boolean) {
	if (busy.value || props.blocked || pending.value) return;
	const v = selectedView.value;
	await perform({
		id: update && v ? v.view_id : crypto.randomUUID(),
		name: viewName.value,
		expected_revision: update && v ? v.revision : 0,
		query: query(),
	});
}
async function recover() {
	if (pending.value && !busy.value) await perform(pending.value);
}
function discardPending() {
	if (window.confirm('先查询视图列表；结束本地等待不会撤销已保存视图。确认结束？')) {
		pending.value = undefined;
		sessionStorage.removeItem(storage());
	}
}
async function removeView() {
	const v = selectedView.value;
	if (!v || busy.value || !window.confirm(`删除视图 ${v.name}？不会删除正式数据。`)) return;
	busy.value = true;
	const token = epoch;
	try {
		await appApi.deleteDataView(v.view_id, v.revision);
		if (token !== epoch) return;
		viewID.value = '';
		viewName.value = '';
		await reloadViews();
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => JSON.stringify(props.query),
	() => restore(props.query),
	{ immediate: true }
);
watch(
	() => props.tableId,
	() => {
		epoch++;
		views.value = [];
		viewID.value = '';
		pending.value = undefined;
		const raw = sessionStorage.getItem(storage());
		if (raw) {
			try {
				pending.value = JSON.parse(raw);
				message.value = '存在未确认的视图保存，请查询原结果。';
			} catch {
				message.value = '本地视图请求记录无法恢复。';
			}
		}
		void reloadViews();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
defineExpose({ hasUnappliedChanges: () => dirty.value, hasUnsavedChanges: () => dirty.value || busy.value || !!pending.value });
</script>
<style scoped>
.query-filter {
	display: flex;
	flex-wrap: wrap;
	align-items: end;
	gap: 12px;
	margin: 12px 0;
}
label {
	display: block;
	margin: 8px 0;
}
label input,
label select {
	display: block;
	margin-top: 6px;
}
details input {
	display: inline;
}
details label {
	display: inline-block;
	margin-right: 16px;
}
</style>

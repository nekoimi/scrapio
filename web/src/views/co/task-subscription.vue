<template>
	<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
	<el-alert v-if="notice" :title="notice" type="info" :closable="false" />
	<p>来源表的正式新增、实际更新触发固定版本任务。每批是有限运行；活动运行期间积压等待，不跳过事件。试采、未变化记录不触发。</p>
	<template v-if="current">
		<el-descriptions :column="2" border>
			<el-descriptions-item label="来源表">{{ current.table_name }} · Schema {{ current.schema_version }}</el-descriptions-item>
			<el-descriptions-item label="状态"
				>{{ current.status === 'blocked' ? '已阻断' : current.enabled ? '订阅中' : '已暂停' }} · revision {{ current.revision }}</el-descriptions-item
			>
			<el-descriptions-item label="已交付游标">{{ current.cursor }} / {{ current.latest_sequence }}</el-descriptions-item>
			<el-descriptions-item label="待处理"
				>变化 {{ current.pending_changes }} · 回补 {{ current.backfill_count - current.backfill_cursor }}</el-descriptions-item
			>
		</el-descriptions>
		<el-alert v-if="current.error?.code" :title="current.error.code + '：' + current.error.message" type="error" :closable="false" />
		<details>
			<summary>冻结版本和输入映射</summary>
			<pre>{{ JSON.stringify({ task_version_id: current.task_version_id, mapping: current.mapping, batch_size: current.batch_size }, null, 2) }}</pre>
		</details>
		<el-button :disabled="busy || current.status === 'blocked'" @click="control(current.enabled ? 'pause' : 'resume')">{{
			current.enabled ? '暂停订阅' : '恢复订阅'
		}}</el-button>
		<el-button type="danger" :disabled="busy" @click="control('retire')">结束订阅</el-button>
		<p>暂停保留游标及未消费证据；结束不可恢复，释放未交付回补和游标保护。两者均不取消已生成运行。配置需修改时先结束，再明确创建新订阅。</p>
	</template>
	<el-form v-else label-position="top" :disabled="busy">
		<el-form-item label="固定任务版本"
			><el-select v-model="versionID" @change="loadFields"
				><el-option v-for="v in versions" :key="v.id" :label="'任务版本 ' + v.number" :value="v.id" /></el-select
		></el-form-item>
		<el-form-item label="订阅来源数据表"
			><el-select v-model="tableID" @change="resetMapping"
				><el-option v-for="t in tables" :key="t.id" :label="t.name + ' · Schema ' + t.schema_version" :value="t.id" /></el-select
			><el-button v-if="tableCursor" link :disabled="busy" @click="loadTables">更多数据表</el-button></el-form-item
		>
		<el-form-item v-for="f in fields" :key="f.key" :label="f.key + ' · ' + f.type + (f.required ? ' · 必填' : '')">
			<el-select v-model="mapping[f.key]" clearable placeholder="选择来源列，或使用采集器默认值"
				><el-option
					v-for="column in selectedTable?.schema.fields || []"
					:key="column.key"
					:value="column.key"
					:label="column.key + ' · ' + column.type"
			/></el-select>
		</el-form-item>
		<el-form-item label="初始输入"
			><el-radio-group v-model="initialMode"
				><el-radio value="backfill">回补当前记录 + 后续变化</el-radio><el-radio value="future">仅创建后的变化</el-radio></el-radio-group
			></el-form-item
		>
		<p>回补会冻结当前记录与变更水位，最多1000条/4MiB，超过上限会拒绝，不静默截断。仅未来不处理已有记录。</p>
		<el-form-item label="每批输入上限"><el-input-number v-model="batchSize" :min="1" :max="100" /></el-form-item>
		<el-checkbox v-model="enabled">创建后开始消费</el-checkbox>
		<el-button type="primary" :loading="busy" :disabled="!versionID || !tableID || dirty" @click="create">确认创建订阅</el-button>
		<p v-if="dirty">请先保存草稿。订阅固定已发布版本，发布新版本不会自动切换订阅。</p>
	</el-form>
	<el-button :disabled="busy" @click="load">刷新订阅与交付状态</el-button
	><el-button :disabled="busy" link @click="recover">查询上次订阅操作</el-button>
	<h4>最近20批交付</h4>
	<el-table :data="batches">
		<el-table-column label="来源"
			><template #default="{ row }">{{ row.kind === 'backfill' ? '初始回补' : '数据变化' }}</template></el-table-column
		><el-table-column label="数量" prop="input_count" />
		<el-table-column label="范围"
			><template #default="{ row }">{{ row.first_sequence }}..{{ row.last_sequence }}</template></el-table-column
		>
		<el-table-column label="运行"
			><template #default="{ row }"
				><router-link :to="'/app/co/runs/' + row.run_id">{{ statusLabel(row.run_status) }} · {{ row.run_id.slice(0, 8) }}</router-link></template
			></el-table-column
		>
	</el-table>
	<p>游标代表已交付持久运行，不代表采集成功。失败运行从运行详情重试；不会自动重建已交付批次。来源Schema变化或无效输入会阻断订阅并保留游标。</p>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { statusLabel } from './status';
import { ElMessageBox } from 'element-plus';
import { call, coApi, describe, type Field } from './api';
import { taskApi, taskMutation, recoverTaskMutation, type Task, type TaskVersion, type TaskStage, type DataTable } from './tasks-api';
const props = defineProps<{ task: Task; versions: TaskVersion[]; dirty: boolean }>();
const busy = ref(false),
	problem = ref(''),
	notice = ref(''),
	items = ref<any[]>([]),
	batches = ref<any[]>([]),
	tables = ref<DataTable[]>([]),
	tableCursor = ref('');
const versionID = ref(''),
	tableID = ref(''),
	fields = ref<Field[]>([]),
	mapping = ref<Record<string, string>>({}),
	initialMode = ref('backfill'),
	batchSize = ref(20),
	enabled = ref(true);
const current = computed(() => items.value.find((x) => x.status !== 'retired'));
const selectedTable = computed(() => tables.value.find((t) => t.id === tableID.value));
async function guard(fn: () => Promise<void>) {
	busy.value = true;
	problem.value = '';
	try {
		await fn();
		return true;
	} catch (e) {
		problem.value = describe(e);
		return false;
	} finally {
		busy.value = false;
	}
}
async function load() {
	await guard(async () => {
		const data = await call<any>('/tasks/' + props.task.id + '/subscriptions');
		items.value = data.items;
		batches.value = data.batches;
	});
}
async function loadTables() {
	await guard(async () => {
		const p = await taskApi.tables(tableCursor.value);
		tables.value.push(...p.items);
		tableCursor.value = p.next_cursor || '';
	});
}
async function loadFields() {
	await guard(async () => {
		const v = props.versions.find((x: TaskVersion) => x.id === versionID.value);
		if (!v) return;
		const m = v.manifest;
		const id = m.definition.stages.find((x: TaskStage) => x.id === m.definition.entry_stage)?.collector_version_id;
		if (!id) return;
		const collector = await coApi.version(id);
		fields.value = collector.definition.input_fields;
		resetMapping();
	});
}
function resetMapping() {
	mapping.value = Object.fromEntries(
		fields.value.filter((f) => selectedTable.value?.schema.fields.some((c) => c.key === f.key)).map((f) => [f.key, f.key])
	);
}
async function confirm(text: string) {
	try {
		await ElMessageBox.confirm(text, '确认订阅', { customClass: 'co-confirm', type: 'warning' });
		return true;
	} catch {
		return false;
	}
}
async function create() {
	if (!selectedTable.value || props.dirty) return;
	if (
		!(await confirm(initialMode.value === 'backfill' ? '确认冻结当前表记录并持续订阅后续新增、更新？' : '确认仅订阅创建后的变化？已有记录不会回补。'))
	)
		return;
	const saved = await guard(async () => {
		const t = selectedTable.value!;
		await taskMutation('createSubscription', props.task.id, '/tasks/' + props.task.id + '/subscriptions', {
			task_version_id: versionID.value,
			table_id: t.id,
			schema_version: t.schema_version,
			schema_hash: t.schema_hash,
			mapping: Object.fromEntries(Object.entries(mapping.value).filter(([, v]) => v)),
			initial_mode: initialMode.value,
			batch_size: batchSize.value,
			enabled: enabled.value,
			confirmed: true,
		});
		notice.value = '订阅已创建；完整输入与来源会冻结到每批运行。';
	});
	if (saved) await load();
}
async function control(action: string) {
	const sub = current.value;
	if (!sub) return;
	if (action === 'retire' && !(await confirm('结束订阅不可恢复，未交付回补将释放；已生成运行继续。后续重新创建需明确选择回补或仅未来。'))) return;
	const saved = await guard(async () => {
		await taskMutation('controlSubscription', props.task.id, '/tasks/' + props.task.id + '/subscriptions/control', {
			id: sub.id,
			expected_revision: sub.revision,
			action,
		});
	});
	if (saved) await load();
}
async function recover() {
	const found = await guard(async () => {
		for (const op of ['createSubscription', 'controlSubscription']) {
			const result = await recoverTaskMutation(op, props.task.id);
			if (result) notice.value = '原操作：' + JSON.stringify(result);
		}
	});
	if (found) await load();
}
watch(
	() => props.versions,
	async () => {
		if (!versionID.value && props.versions.length) {
			versionID.value = props.versions[0].id;
			await loadFields();
		}
	}
);
onMounted(async () => {
	versionID.value = props.versions[0]?.id || '';
	await load();
	await loadTables();
	await loadFields();
});
</script>

<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>{{ task?.name || '创建单阶段任务' }}</h1>
				<p>采集器负责一类页面；任务决定输入、保存规则和运行策略。</p>
			</div>
			<el-button @click="router.push('/app/co/tasks')">任务列表</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" /><el-alert
			v-if="notice"
			:title="notice"
			type="success"
			:closable="false"
		/>
		<el-button :disabled="busy" @click="recover">查询上次操作</el-button>
		<el-card
			><template #header>1 · 选择确定的采集器版本</template>
			<el-form label-position="top"
				><el-form-item label="任务名称"><el-input v-model="name" maxlength="64" :disabled="!!task" /></el-form-item>
				<div class="co-columns">
					<el-form-item label="独立采集器"
						><el-select v-model="collectorID" @change="loadVersions"
							><el-option v-for="c in collectors" :key="c.id" :label="c.name" :value="c.id" /></el-select
						><el-button v-if="collectorCursor" link @click="moreCollectors">加载更多采集器</el-button></el-form-item
					>
					<el-form-item label="发布版本"
						><el-select v-model="versionID" @change="versionChanged"
							><el-option
								v-for="v in collectorVersions"
								:key="v.id"
								:label="'版本 ' + v.number + ' · revision ' + v.revision"
								:value="v.id" /></el-select
					></el-form-item>
				</div>
				<p v-if="version">
					输入：{{ version.definition.input_fields.map((f) => f.key + ' (' + f.type + ')').join('、') }} → 输出：{{
						version.definition.output.fields.map((f) => f.key).join('、')
					}}
				</p>
			</el-form>
		</el-card>
		<el-card
			><template #header>2 · 输入快照</template>
			<el-radio-group v-model="inputMode"
				><el-radio-button label="urls">网址列表</el-radio-button><el-radio-button label="json">结构化 JSON</el-radio-button></el-radio-group
			>
			<p class="co-muted">最多100行。网址列表每行一个地址；有其他必填参数时使用JSON对象数组。相同网址的不同输入保留独立来源。</p>
			<el-input
				v-model="inputText"
				type="textarea"
				:rows="6"
				:placeholder="inputMode === 'urls' ? 'https://example.com/page' : JSON.stringify([{ url: 'https://example.com/page', movie_id: 'm001' }])"
			/>
			<el-button :disabled="!versionID || busy" @click="previewInputs">检查输入</el-button>
			<el-table v-if="inputPreview.length" :data="inputPreview"
				><el-table-column label="行" width="70"
					><template #default="{ row }">{{ row.ordinal + 1 }}</template></el-table-column
				><el-table-column label="检查结果"
					><template #default="{ row }"
						>{{ row.error?.message || '有效' }}<code>{{ JSON.stringify(row.values) }}</code></template
					></el-table-column
				></el-table
			>
		</el-card>
		<el-card v-if="version"
			><template #header>3 · 数据表与保存绑定</template>
			<el-form label-position="top"
				><el-form-item label="输出数据表"
					><el-select v-model="save.table_id" @change="bindTable"
						><el-option v-for="t in tables" :key="t.id" :value="t.id" :label="t.name + ' · Schema ' + t.schema_version" /></el-select
					><el-button v-if="tableCursor" link @click="moreTables">加载更多表</el-button></el-form-item
				>
				<div class="co-toolbar">
					<el-input v-model="tableName" placeholder="新数据表名称" /><el-button :disabled="busy" @click="createTable">按采集器输出新建表</el-button>
				</div>
				<p v-if="table">唯一键：{{ table.schema.unique_key.join(' + ') }} · Schema {{ save.schema_version }}</p>
				<el-table v-if="table" :data="table.schema.fields"
					><el-table-column prop="key" label="表字段" /><el-table-column prop="type" label="类型" width="100" /><el-table-column
						label="采集器输出字段"
						><template #default="{ row }"
							><el-tag v-if="save.mapping[row.key]?.kind === 'constant'">常量 {{ JSON.stringify(save.mapping[row.key].value) }}</el-tag
							><el-select v-model="mappingKeys[row.key]" clearable
								><el-option
									v-for="f in version.definition.output.fields"
									:key="f.key"
									:label="f.key + ' (' + f.type + ')'"
									:value="f.key" /></el-select></template></el-table-column
				></el-table>
				<div class="co-columns">
					<el-form-item label="更新策略"
						><el-select v-model="save.update_policy"
							><el-option label="合并本次出现的字段" value="merge_present" /><el-option
								label="替换为本次出现的字段"
								value="replace_present" /></el-select></el-form-item
					><el-form-item label="null 策略"
						><el-select v-model="save.null_policy"
							><el-option label="null 保留旧值" value="keep" /><el-option label="null 清空旧值" value="clear" /></el-select
					></el-form-item>
				</div>
				<el-button :disabled="busy || !table" @click="previewSave">用所选版本最近测试结果预演</el-button>
				<el-table v-if="savePreview.length" :data="savePreview"
					><el-table-column prop="identity" label="输出身份" /><el-table-column prop="decision" label="保存决策" width="140" /><el-table-column
						label="结果"
						><template #default="{ row }">{{ row.error?.message || JSON.stringify(row.values) }}</template></el-table-column
					></el-table
				>
			</el-form>
		</el-card>
		<el-card
			><template #header>4 · 预算与失败策略</template
			><el-form label-position="top"
				><div class="co-budget-grid">
					<el-form-item label="最多工作项"><el-input-number v-model="budget.max_work_items" :min="1" :max="10000" /></el-form-item>
					<el-form-item label="最多输出"><el-input-number v-model="budget.max_outputs" :min="1" :max="100000" /></el-form-item>
					<el-form-item label="最多待处理项"><el-input-number v-model="budget.max_pending" :min="1" :max="10000" /></el-form-item>
					<el-form-item label="任务浏览器槽位"><el-input-number v-model="budget.browser_slots" :min="1" :max="8" /></el-form-item>
					<el-form-item label="运行总时长（毫秒）"><el-input-number v-model="budget.max_duration_ms" :min="1000" :max="86400000" /></el-form-item>
					<el-form-item label="工作项失败后"
						><el-select v-model="onError"
							><el-option label="继续其他输入" value="continue" /><el-option label="暂停当前阶段" value="pause_stage" /></el-select
					></el-form-item>
				</div>
				<p class="co-muted">采集器的分页/记录/时长上限仍然有效。已有活动运行时跳过重复触发。</p></el-form
			></el-card
		>
		<div class="co-toolbar">
			<el-button type="primary" :loading="busy" :disabled="!version || !table" @click="saveDraft">保存任务草稿</el-button
			><el-button :disabled="busy || !task || dirty" @click="publish">发布当前 revision</el-button>
		</div>
		<el-card v-if="task"
			><template #header>5 · 使用已发布版本执行</template
			><el-select v-model="taskVersionID"
				><el-option v-for="v in taskVersions" :key="v.id" :label="'任务版本 ' + v.number + ' · revision ' + v.revision" :value="v.id"
			/></el-select>
			<p class="co-muted">试采会访问所有冻结输入并保存隔离的运行证据，正式记录写入为0。正式执行按此任务版本的表绑定写入。</p>
			<el-button :disabled="busy || !taskVersionID || dirty" @click="start('trial')">隔离试采</el-button
			><el-button type="primary" :disabled="busy || !taskVersionID || dirty" @click="start('formal')">正式执行</el-button></el-card
		>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { ElMessageBox } from 'element-plus';
import { call, coApi, describe, type Collector, type Version, type Candidate } from './api';
import {
	taskApi,
	taskMutation,
	recoverTaskMutation,
	type Task,
	type SaveBinding,
	type DataTable,
	type TaskDefinition,
	type TaskVersion,
} from './tasks-api';
const route = useRoute(),
	router = useRouter(),
	task = ref<Task>(),
	name = ref(''),
	collectors = ref<Collector[]>([]),
	collectorCursor = ref(''),
	collectorID = ref(String(route.query.collector_id || '')),
	collectorVersions = ref<Version[]>([]),
	versionID = ref(''),
	tables = ref<DataTable[]>([]),
	tableCursor = ref(''),
	tableName = ref(''),
	mappingKeys = ref<Record<string, string>>({}),
	busy = ref(false),
	problem = ref(''),
	notice = ref(''),
	inputMode = ref('urls'),
	inputText = ref(''),
	inputPreview = ref<any[]>([]),
	savePreview = ref<any[]>([]),
	taskVersions = ref<TaskVersion[]>([]),
	taskVersionID = ref(''),
	onError = ref('continue');
const save = ref<SaveBinding>({
		table_id: '',
		schema_version: 1,
		schema_hash: '',
		mapping: {},
		unique_key: [],
		update_policy: 'merge_present',
		null_policy: 'keep',
	}),
	budget = ref({ max_work_items: 100, max_outputs: 1000, max_pending: 10, browser_slots: 1, max_duration_ms: 600000 });
const version = computed(() => collectorVersions.value.find((v) => v.id === versionID.value)),
	table = computed(() => tables.value.find((t) => t.id === save.value.table_id));
function inputs() {
	const raw =
		inputMode.value === 'urls'
			? inputText.value
					.split(/\r?\n/)
					.map((s) => s.trim())
					.filter(Boolean)
					.map((url) => ({ url }))
			: JSON.parse(inputText.value);
	if (!Array.isArray(raw) || raw.length < 1 || raw.length > 100 || raw.some((v) => !v || Array.isArray(v) || typeof v !== 'object'))
		throw new Error('请输入1..100条JSON对象或网址');
	return raw;
}
function definition(): TaskDefinition {
	const mapping = { ...save.value.mapping };
	for (const f of table.value?.schema.fields || []) {
		if (mappingKeys.value[f.key]) mapping[f.key] = { kind: 'field', path: 'output.' + mappingKeys.value[f.key] };
		else if (mapping[f.key]?.kind !== 'constant') delete mapping[f.key];
	}
	return {
		definition_version: 'task.v1',
		entry_stage: task.value?.definition.entry_stage || 'entry',
		source: { kind: 'manual', inputs: inputs() },
		stages: [
			{
				id: task.value?.definition.entry_stage || 'entry',
				name: '入口阶段',
				collector_version_id: versionID.value,
				save: { ...save.value, mapping },
				on_error: onError.value,
			},
		],
		edges: [],
		budget: budget.value,
		overlap_policy: 'skip',
	};
}
const baseline = ref('');
const dirty = computed(() => {
	if (!version.value && !name.value && !inputText.value) return false;
	try {
		return baseline.value !== JSON.stringify({ name: name.value, definition: definition() });
	} catch {
		return true;
	}
});
async function guarded(fn: () => Promise<void>) {
	busy.value = true;
	problem.value = '';
	notice.value = '';
	try {
		await fn();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
async function moreCollectors() {
	const p = await coApi.list(collectorCursor.value);
	collectors.value.push(...p.items);
	collectorCursor.value = p.next_cursor || '';
}
async function moreTables() {
	const p = await taskApi.tables(tableCursor.value);
	tables.value.push(...p.items);
	tableCursor.value = p.next_cursor || '';
}
async function loadVersions() {
	try {
		collectorVersions.value = (await coApi.versions(collectorID.value)).items;
		versionID.value = collectorVersions.value[0]?.id || '';
		versionChanged();
	} catch (e) {
		problem.value = describe(e);
	}
}
function versionChanged() {
	inputPreview.value = [];
	savePreview.value = [];
	if (version.value) {
		tableName.value = name.value || '采集结果';
		if (!inputText.value) {
			inputText.value = JSON.stringify([Object.fromEntries(version.value.definition.input_fields.map((f) => [f.key, f.default ?? '']))], null, 2);
			inputMode.value = 'json';
		}
	}
}
function bindTable() {
	if (!table.value) return;
	save.value = {
		...save.value,
		table_id: table.value.id,
		schema_version: table.value.schema_version,
		schema_hash: table.value.schema_hash,
		unique_key: table.value.schema.unique_key,
		mapping: {},
	};
	mappingKeys.value = Object.fromEntries(
		table.value.schema.fields.map((f) => [f.key, version.value?.definition.output.fields.some((v) => v.key === f.key) ? f.key : ''])
	);
	savePreview.value = [];
}
async function createTable() {
	await guarded(async () => {
		if (!version.value) return;
		const out = version.value.definition.output;
		const t = await taskMutation<DataTable>('createTable', '', '/tables', {
			name: tableName.value,
			schema: { fields: out.fields, unique_key: out.identity_fields },
		});
		tables.value.unshift(t);
		save.value.table_id = t.id;
		bindTable();
	});
}
async function previewInputs() {
	await guarded(async () => {
		inputPreview.value = (await taskApi.inputPreview(versionID.value, inputs())).items;
	});
}
async function previewSave() {
	await guarded(async () => {
		if (!version.value) return;
		const tests = (await coApi.tests(collectorID.value)).items;
		const tested = tests.find((t) => t.result?.definition_hash === version.value?.definition_hash);
		if (!tested) throw new Error('所选采集器版本没有可用测试结果；可发布任务后先隔离试采。');
		const candidates: Candidate[] = tested.result.candidates.slice(0, 100);
		savePreview.value = (await taskApi.savePreview(versionID.value, definition().stages[0].save, candidates)).decisions;
	});
}
async function saveDraft() {
	await guarded(async () => {
		const d = definition();
		if (task.value) {
			task.value = await taskApi.save(task.value.id, task.value.revision, d);
		} else {
			task.value = await taskMutation<Task>('createTask', '', '/tasks', { name: name.value, definition: d });
			baseline.value = JSON.stringify({ name: name.value, definition: d });
			await router.replace('/app/co/tasks/' + task.value.id);
		}
		baseline.value = JSON.stringify({ name: name.value, definition: definition() });
		notice.value = '已保存任务 revision ' + task.value!.revision;
	});
}
async function publish() {
	await guarded(async () => {
		if (!task.value) return;
		const v = await taskMutation<TaskVersion>('publishTask', task.value.id, '/tasks/' + task.value.id + '/versions', {
			expected_revision: task.value.revision,
		});
		taskVersions.value = (await taskApi.versions(task.value.id)).items;
		taskVersionID.value = v.id;
		notice.value = '发布成功，输入、规则、Schema和预算已冻结';
	});
}
async function confirm(message: string) {
	try {
		await ElMessageBox.confirm(message, '确认执行', {
			type: 'warning',
			confirmButtonText: '确认',
			cancelButtonText: '取消',
			customClass: 'co-confirm',
		});
		return true;
	} catch {
		return false;
	}
}
async function start(namespace: string) {
	if (
		!task.value ||
		!(await confirm(
			namespace === 'formal' ? '将访问冻结输入并按发布版本写入数据表，是否执行？' : '将访问冻结输入并执行网页动作，正式记录写入为0，是否试采？'
		))
	)
		return;
	await guarded(async () => {
		const r = await taskMutation<any>('startTask', task.value!.id, '/tasks/' + task.value!.id + '/runs', {
			task_version_id: taskVersionID.value,
			namespace,
		});
		await router.push('/app/co/runs/' + (r.id || r.active_run.id));
	});
}
async function recover() {
	await guarded(async () => {
		for (const op of ['createTask', 'createTable', 'publishTask', 'startTask']) {
			const resource = ['createTask', 'createTable'].includes(op) ? '' : task.value?.id || '';
			const r = await recoverTaskMutation(op, resource);
			if (r) {
				notice.value = '已查询原操作：' + JSON.stringify(r);
				if (op === 'createTask') {
					baseline.value = JSON.stringify({ name: name.value, definition: definition() });
					task.value = r;
					await router.replace('/app/co/tasks/' + r.id);
				} else if (op === 'startTask') {
					await router.push('/app/co/runs/' + (r.id || r.active_run.id));
				} else if (op === 'createTable') {
					tables.value.unshift(r);
					save.value.table_id = r.id;
					bindTable();
				} else if (task.value) {
					taskVersions.value = (await taskApi.versions(task.value.id)).items;
					taskVersionID.value = r.id;
				}
			}
		}
	});
}
function beforeUnload(event: BeforeUnloadEvent) {
	if (dirty.value) {
		event.preventDefault();
		event.returnValue = '';
	}
}
window.addEventListener('beforeunload', beforeUnload);
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload));
onBeforeRouteLeave(async () => !dirty.value || (await confirm('有未保存的任务修改，确认离开？')));
onMounted(() =>
	guarded(async () => {
		await moreCollectors();
		await moreTables();
		if (route.params.id) {
			task.value = await taskApi.get(String(route.params.id));
			const d = task.value.definition;
			name.value = task.value.name;
			collectorVersions.value = [];
			const all = d.stages[0];
			versionID.value = all.collector_version_id;
			const versions = (taskVersions.value = (await taskApi.versions(task.value.id)).items);
			const selectedVersion = await coApi.version(versionID.value);
			collectorID.value = selectedVersion.collector_id || '';
			collectorVersions.value = [selectedVersion, ...(await coApi.versions(collectorID.value)).items.filter((v) => v.id !== selectedVersion.id)];
			if (!collectors.value.some((c) => c.id === collectorID.value)) collectors.value.push(await coApi.get(collectorID.value));
			if (!tables.value.some((t) => t.id === all.save.table_id)) tables.value.push(await call<DataTable>('/tables/' + all.save.table_id));
			save.value = all.save;
			mappingKeys.value = Object.fromEntries(
				Object.entries(all.save.mapping)
					.filter(([, v]) => v.kind === 'field')
					.map(([k, v]) => [k, v.path?.slice(7) || ''])
			);
			budget.value = d.budget;
			onError.value = all.on_error;
			inputMode.value = 'json';
			inputText.value = JSON.stringify(d.source.inputs, null, 2);
			taskVersionID.value = versions[0]?.id || '';
		} else if (collectorID.value) {
			await loadVersions();
		}
		baseline.value = JSON.stringify({ name: name.value, definition: version.value ? definition() : null });
	})
);
</script>
<style src="./style.css"></style>

<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>{{ task?.name || '创建采集任务' }}</h1>
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
		<div class="co-orchestration">
			<aside class="co-stage-list">
				<h3>任务阶段 {{ stageDefs.length }}/20</h3>
				<el-button
					v-for="(st, index) in stageDefs"
					:key="st.id"
					:style="{ paddingLeft: 12 + stageDepth(st.id) * 12 + 'px' }"
					:type="st.id === selectedID ? 'primary' : 'default'"
					:disabled="busy"
					@click="selectStage(st.id)"
				>
					<span
						>{{ index + 1 }} · {{ st.id === selectedID ? stageName : st.name
						}}<small>{{ st.id === entryID ? '手动入口' : '来自 ' + edges.find((e) => e.to === st.id)?.from }}</small></span
					>
				</el-button>
				<el-button :disabled="busy || stageDefs.length >= 20" @click="addStage">从当前阶段添加下游 / 分支</el-button>
				<el-button :disabled="busy || selectedID === entryID" type="danger" plain @click="removeStage">删除当前阶段及下游</el-button>
				<p class="co-muted">每个阶段使用独立采集器和数据表。一个上游可分支；暂不支持多上游合并。</p>
			</aside>
			<div class="co-stage-config">
				<el-card
					><template #header>配置阶段 · {{ stageName }}</template>
					<el-form label-position="top"
						><el-form-item label="阶段名称"><el-input v-model="stageName" maxlength="64" /></el-form-item
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
						<router-link v-if="collectorID" :to="'/app/co/collectors/' + collectorID">打开采集器工作台编辑提取规则</router-link>
						<p v-if="version">
							输入：{{ version.definition.input_fields.map((f) => f.key + ' (' + f.type + ')').join('、') }} → 输出：{{
								version.definition.output.fields.map((f) => f.key).join('、')
							}}
						</p>
					</el-form>
				</el-card>
				<el-card v-if="selectedID === entryID"
					><template #header>入口 · 输入来源与快照</template>
					<el-radio-group v-model="sourceKind"
						><el-radio-button label="manual">手动输入</el-radio-button><el-radio-button label="table">数据表输入</el-radio-button
						><el-radio-button label="file">文件输入</el-radio-button></el-radio-group
					>
					<SourcePicker
						v-if="sourceKind !== 'manual'"
						v-model="frozenSource"
						:kind="sourceKind"
						:version-i-d="versionID"
						:fields="version?.definition.input_fields || []"
					/>
					<template v-else>
						<el-radio-group v-model="inputMode"
							><el-radio-button label="urls">网址列表</el-radio-button><el-radio-button label="json">结构化 JSON</el-radio-button></el-radio-group
						>
						<p class="co-muted">最多100行。网址列表每行一个地址；有其他必填参数时使用JSON对象数组。相同网址的不同输入保留独立来源。</p>
						<el-input
							v-model="inputText"
							type="textarea"
							:rows="6"
							:placeholder="
								inputMode === 'urls' ? 'https://example.com/page' : JSON.stringify([{ url: 'https://example.com/page', movie_id: 'm001' }])
							"
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
					</template>
				</el-card>
				<el-card v-if="incoming && version">
					<template #header>上游连接 · {{ incoming.from }} → {{ selectedID }}</template>
					<el-form label-position="top">
						<el-form-item label="上游阶段"
							><el-select :model-value="incoming.from" @change="changeParent"
								><el-option v-for="st in parentOptions" :key="st.id" :label="st.name" :value="st.id" /></el-select
						></el-form-item>
						<el-form-item label="传递策略"
							><el-select v-model="incoming.emit"
								><el-option label="所有有效输出（含未变化）" value="valid" /><el-option label="仅新增" value="created" /><el-option
									label="新增及变化"
									value="changed" /></el-select
						></el-form-item>
						<p class="co-muted">仅新增/变化会跳过本次未变化记录的下游刷新；保存失败和冲突候选始终不传递。</p>
						<el-form-item label="数组展开"
							><el-select :model-value="incoming.expand?.path || ''" clearable @change="setExpand"
								><el-option
									v-for="f in upstream?.definition.output.fields.filter((f) => f.multiple) || []"
									:key="f.key"
									:label="f.key"
									:value="'output.' + f.key" /></el-select
						></el-form-item>
						<el-table :data="version.definition.input_fields">
							<el-table-column prop="key" label="下游输入" /><el-table-column prop="type" label="类型" width="90" />
							<el-table-column label="映射方式"
								><template #default="{ row }"
									><el-select :model-value="incoming.mapping[row.key]?.kind || ''" clearable @change="(kind) => setMappingKind(row.key, kind)"
										><el-option label="来源字段" value="field" /><el-option label="常量" value="constant" /><el-option
											label="文本模板"
											value="template" /></el-select></template
							></el-table-column>
							<el-table-column label="来源 / 值"
								><template #default="{ row }">
									<el-select v-if="incoming.mapping[row.key]?.kind === 'field'" v-model="incoming.mapping[row.key].path"
										><el-option v-for="f in mappingSources" :key="f.path" :label="f.label" :value="f.path"
									/></el-select>
									<el-input
										v-else-if="incoming.mapping[row.key]"
										:model-value="mappingText(incoming.mapping[row.key])"
										@input="(value) => setMappingValue(row.key, value)"
										:placeholder="incoming.mapping[row.key].kind === 'template' ? '例如 {{output.movie_id}}' : 'JSON值，例如 &quot;固定值&quot;'"
									/>
									<span v-else>使用输入默认值（必填项请配置）</span>
								</template></el-table-column
							>
						</el-table>
						<el-form-item label="过滤"
							><el-select :model-value="incoming.filter?.path || ''" clearable @change="setFilter"
								><el-option v-for="f in mappingSources" :key="f.path" :label="f.label" :value="f.path" /></el-select
						></el-form-item>
						<div v-if="incoming.filter" class="co-columns">
							<el-select v-model="incoming.filter.op"
								><el-option label="存在且非null" value="exists" /><el-option label="等于" value="equal" /><el-option
									label="不等于"
									value="not_equal" /></el-select
							><el-input
								v-if="incoming.filter.op !== 'exists'"
								:model-value="invalidMappingValues['filter:' + incoming.id] ?? JSON.stringify(incoming.filter.value)"
								placeholder="比较值（JSON）"
								@change="setFilterValue"
							/>
						</div>
						<el-divider>连接样例预览</el-divider>
						<el-button :disabled="busy || !upstream" @click="loadMappingSample">载入上游所选版本的测试样例</el-button>
						<el-form-item label="上游输入（JSON）"><el-input v-model="sampleInput" type="textarea" :rows="3" /></el-form-item>
						<el-form-item label="上游一条输出（JSON）"><el-input v-model="sampleOutput" type="textarea" :rows="4" /></el-form-item>
						<el-button :disabled="busy || !upstream" @click="previewMapping">预览下游输入</el-button>
						<el-table v-if="mappingPreview.length" :data="mappingPreview"
							><el-table-column prop="decision" label="映射决策" /><el-table-column label="下游值 / 错误"
								><template #default="{ row }">{{ row.error?.message || JSON.stringify(row.values) }}</template></el-table-column
							></el-table
						>
						<p class="co-muted">模板支持 output.key、input.key 与展开 element。预览不会写数据；空数组正常产生零项。</p>
					</el-form>
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
							<el-input v-model="tableName" placeholder="新数据表名称" /><el-button :disabled="busy" @click="createTable"
								>按采集器输出新建表</el-button
							>
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
							<el-form-item label="当前阶段成功输入刷新间隔（秒；0每次访问）"
								><el-input-number v-model="refreshSeconds" :min="0" :max="2592000"
							/></el-form-item>
							<el-form-item label="最多待分发元素"><el-input-number v-model="budget.max_dispatches" :min="1" :max="10000" /></el-form-item
							><el-form-item label="最多工作项"><el-input-number v-model="budget.max_work_items" :min="1" :max="10000" /></el-form-item>
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
			</div>
		</div>
		<div class="co-toolbar">
			<el-button type="primary" :loading="busy" :disabled="!version || !table" @click="saveDraft">保存任务草稿</el-button
			><el-button :disabled="busy || !task || dirty" @click="publish">发布当前 revision</el-button>
		</div>
		<TaskDaily
			v-if="task"
			:task="task"
			:versions="taskVersions"
			:selected-stage="selectedID"
			:collector-version="versionID"
			:dirty="dirty"
			@upgraded="reloadTask"
		/>
		<el-card v-if="task"
			><template #header>5 · 使用已发布版本执行</template
			><el-select v-model="taskVersionID"
				><el-option v-for="v in taskVersions" :key="v.id" :label="'任务版本 ' + v.number + ' · revision ' + v.revision" :value="v.id"
			/></el-select>
			<p class="co-muted">试采会访问所有冻结输入并保存隔离的运行证据，正式记录写入为0。正式执行按此任务版本的表绑定写入。</p>
			<el-radio-group v-model="trialMode"
				><el-radio-button label="live">实时试采</el-radio-button><el-radio-button label="offline">离线样例</el-radio-button></el-radio-group
			>
			<p class="co-muted">试采最多100工作项、500输出、每项3页/100条、总计120秒；受限或缺页会如实标记。</p>
			<el-input
				v-if="trialMode === 'offline'"
				v-model="trialDocuments"
				type="textarea"
				:rows="6"
				placeholder='按阶段ID提供页面数组：{"entry":[{"url":"https://example.com","format":"html","content":"..."}]}'
			/>
			<el-button v-if="trialMode === 'offline'" :disabled="busy || !taskVersionID" @click="loadTrialSamples">载入各阶段所选版本的页面样例</el-button>
			<el-button :disabled="busy || !taskVersionID || dirty" @click="start('trial')">完整任务隔离试采</el-button
			><el-button type="primary" :disabled="busy || !taskVersionID || dirty" @click="start('formal')">正式执行</el-button></el-card
		>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { ElMessageBox } from 'element-plus';
import SourcePicker from './source-picker.vue';
import TaskDaily from './task-daily.vue';
import type { TaskSource } from './tasks-api';
import { call, coApi, describe, type Collector, type Version, type Candidate, type Binding } from './api';
import {
	taskApi,
	taskMutation,
	recoverTaskMutation,
	type Task,
	type SaveBinding,
	type DataTable,
	type TaskDefinition,
	type TaskVersion,
	type TaskStage,
	type TaskEdge,
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
const sourceKind = ref<'manual' | 'table' | 'file'>(route.query.source_table ? 'table' : 'manual'),
	frozenSource = ref<TaskSource>({ kind: 'table', inputs: [] }),
	refreshSeconds = ref(0);
const save = ref<SaveBinding>({
		table_id: '',
		schema_version: 1,
		schema_hash: '',
		mapping: {},
		unique_key: [],
		update_policy: 'merge_present',
		null_policy: 'keep',
	}),
	budget = ref({ max_work_items: 100, max_outputs: 1000, max_pending: 10, browser_slots: 1, max_duration_ms: 600000, max_dispatches: 10000 });
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
const entryID = ref('entry'),
	selectedID = ref('entry'),
	stageName = ref('入口阶段');
const stageDefs = ref<TaskStage[]>([
	{ id: 'entry', name: '入口阶段', collector_version_id: '', save: JSON.parse(JSON.stringify(save.value)), on_error: 'continue' },
]);
const edges = ref<TaskEdge[]>([]),
	knownVersions = ref<Record<string, Version>>({});
const incoming = computed(() => edges.value.find((e) => e.to === selectedID.value));
const upstream = computed(() => {
	const st = stageDefs.value.find((s) => s.id === incoming.value?.from);
	return st ? knownVersions.value[st.collector_version_id] : undefined;
});
const sampleInput = ref('{}'),
	sampleOutput = ref('{}'),
	mappingPreview = ref<any[]>([]);
const mappingSources = computed(() => [
	...(upstream.value?.definition.output.fields || []).map((f) => ({ path: 'output.' + f.key, label: '输出 · ' + f.key + ' (' + f.type + ')' })),
	...(upstream.value?.definition.input_fields || []).map((f) => ({ path: 'input.' + f.key, label: '输入透传 · ' + f.key })),
	...(incoming.value?.expand ? [{ path: 'element', label: '当前展开元素' }] : []),
]);
function selectedStage(): TaskStage {
	const mapping = { ...save.value.mapping };
	for (const f of table.value?.schema.fields || []) {
		if (mappingKeys.value[f.key]) mapping[f.key] = { kind: 'field', path: 'output.' + mappingKeys.value[f.key] };
		else if (mapping[f.key]?.kind !== 'constant') delete mapping[f.key];
	}
	return {
		id: selectedID.value,
		name: stageName.value,
		collector_version_id: versionID.value,
		save: { ...save.value, mapping },
		on_error: onError.value,
		refresh_seconds: refreshSeconds.value,
	};
}
function persistSelected() {
	stageDefs.value = stageDefs.value.map((st) => (st.id === selectedID.value ? selectedStage() : st));
	if (version.value) knownVersions.value[versionID.value] = version.value;
}
function definition(): TaskDefinition {
	return {
		definition_version: 'task.v1',
		entry_stage: entryID.value,
		source: sourceKind.value === 'manual' ? { kind: 'manual', inputs: inputs() } : { ...frozenSource.value, kind: sourceKind.value },
		stages: stageDefs.value.map((st) => (st.id === selectedID.value ? selectedStage() : st)),
		edges: edges.value,
		budget: budget.value,
		overlap_policy: 'skip',
	};
}
function descendants(id: string): Set<string> {
	const out = new Set([id]);
	for (let i = 0; i < 20; i++) for (const e of edges.value) if (out.has(e.from)) out.add(e.to);
	return out;
}
const parentOptions = computed(() => stageDefs.value.filter((s) => !descendants(selectedID.value).has(s.id)));
function stageDepth(id: string) {
	let depth = 0;
	for (let i = 0; i < 20; i++) {
		const e = edges.value.find((e) => e.to === id);
		if (!e) break;
		depth++;
		id = e.from;
	}
	return depth;
}
async function selectStage(id: string) {
	if (Object.keys(invalidMappingValues.value).length) {
		problem.value = '请先修复当前阶段无效的映射常量';
		return;
	}
	persistSelected();
	selectedID.value = id;
	const st = stageDefs.value.find((s) => s.id === id)!;
	invalidMappingValues.value = {};
	stageName.value = st.name;
	save.value = JSON.parse(JSON.stringify(st.save));
	onError.value = st.on_error;
	refreshSeconds.value = st.refresh_seconds || 0;
	mappingKeys.value = Object.fromEntries(
		Object.entries(st.save.mapping)
			.filter(([, b]) => b.kind === 'field')
			.map(([k, b]) => [k, b.path?.slice(7) || ''])
	);
	versionID.value = st.collector_version_id;
	collectorVersions.value = [];
	collectorID.value = '';
	savePreview.value = [];
	mappingPreview.value = [];
	sampleInput.value = '{}';
	sampleOutput.value = '{}';
	if (versionID.value)
		await guarded(async () => {
			const v = knownVersions.value[versionID.value] || (await coApi.version(versionID.value));
			knownVersions.value[v.id] = v;
			collectorID.value = v.collector_id || '';
			collectorVersions.value = [v, ...(await coApi.versions(collectorID.value)).items.filter((x) => x.id !== v.id)];
		});
}
async function addStage() {
	if (Object.keys(invalidMappingValues.value).length) {
		problem.value = '请修复当前阶段无效的映射值';
		return;
	}
	persistSelected();
	let n = stageDefs.value.length + 1;
	while (stageDefs.value.some((s) => s.id === 'stage_' + n)) n++;
	const id = 'stage_' + n;
	stageDefs.value.push({
		id,
		name: '阶段 ' + n,
		collector_version_id: '',
		save: { table_id: '', schema_version: 1, schema_hash: '', mapping: {}, unique_key: [], update_policy: 'merge_present', null_policy: 'keep' },
		on_error: 'continue',
	});
	edges.value.push({ id: 'to_' + id, from: selectedID.value, to: id, port: 'records', emit: 'valid', mapping: {} });
	await selectStage(id);
}
async function removeStage() {
	if (!(await confirm('删除当前阶段及全部下游？草稿修改需保存后生效。'))) return;
	invalidMappingValues.value = {};
	const gone = descendants(selectedID.value);
	stageDefs.value = stageDefs.value.filter((s) => !gone.has(s.id));
	edges.value = edges.value.filter((e) => !gone.has(e.from) && !gone.has(e.to));
	await selectStage(entryID.value);
}
function changeParent(id: string) {
	if (incoming.value) incoming.value.from = id;
	mappingPreview.value = [];
}
function setExpand(path: string) {
	if (incoming.value) {
		if (path) incoming.value.expand = { path };
		else delete incoming.value.expand;
	}
	mappingPreview.value = [];
}
function setFilter(path: string) {
	delete invalidMappingValues.value['filter:' + (incoming.value?.id || '')];
	if (incoming.value) {
		if (path) incoming.value.filter = { path, op: 'exists' };
		else delete incoming.value.filter;
	}
	mappingPreview.value = [];
}
function setFilterValue(raw: string) {
	const slot = 'filter:' + (incoming.value?.id || '');
	try {
		if (incoming.value?.filter) incoming.value.filter.value = JSON.parse(raw);
		delete invalidMappingValues.value[slot];
	} catch {
		invalidMappingValues.value[slot] = raw;
		problem.value = '过滤比较值需要有效JSON';
	}
	mappingPreview.value = [];
}
function setMappingKind(key: string, kind: string) {
	delete invalidMappingValues.value[(incoming.value?.id || '') + ':' + key];
	if (!incoming.value) return;
	if (!kind) delete incoming.value.mapping[key];
	else
		incoming.value.mapping[key] =
			kind === 'field' ? { kind, path: '' } : { kind: kind as 'constant' | 'template', value: kind === 'template' ? '' : null };
	mappingPreview.value = [];
}
const invalidMappingValues = ref<Record<string, string>>({});
function mappingText(b: Binding) {
	const slot = (incoming.value?.id || '') + ':' + Object.keys(incoming.value?.mapping || {}).find((k) => incoming.value?.mapping[k] === b);
	if (slot in invalidMappingValues.value) return invalidMappingValues.value[slot];
	return b.kind === 'template' ? String(b.value ?? '') : JSON.stringify(b.value);
}
function setMappingValue(key: string, raw: string) {
	if (!incoming.value) return;
	const slot = incoming.value.id + ':' + key;
	try {
		const b = incoming.value.mapping[key];
		b.value = b.kind === 'template' ? raw : JSON.parse(raw);
		delete invalidMappingValues.value[slot];
	} catch {
		invalidMappingValues.value[slot] = raw;
		problem.value = '常量需要有效JSON值';
	}
	mappingPreview.value = [];
}
async function loadMappingSample() {
	await guarded(async () => {
		if (!upstream.value) return;
		const test = (await coApi.tests(upstream.value.collector_id!)).items.find((t) => t.result?.definition_hash === upstream.value!.definition_hash);
		if (!test?.result.candidates.length) throw new Error('上游所选版本没有可用样例输出，请先在采集器工作台测试或填入真实样例');
		sampleInput.value = JSON.stringify(test.result.input || {}, null, 2);
		sampleOutput.value = JSON.stringify(test.result.candidates[0].values, null, 2);
	});
}
async function previewMapping() {
	await guarded(async () => {
		if (!incoming.value || !upstream.value) return;
		if (Object.keys(invalidMappingValues.value).length) throw new Error('请修复无效的映射常量');
		mappingPreview.value = (
			await taskApi.mappingPreview(upstream.value.id, versionID.value, incoming.value, JSON.parse(sampleInput.value), JSON.parse(sampleOutput.value))
		).items;
	});
}
watch(
	() => JSON.stringify(incoming.value),
	() => {
		mappingPreview.value = [];
	}
);
watch(
	() => JSON.stringify([versionID.value, save.value, mappingKeys.value]),
	() => {
		savePreview.value = [];
	}
);
const trialMode = ref('live'),
	trialDocuments = ref('{}');
async function loadTrialSamples() {
	await guarded(async () => {
		const selected = taskVersions.value.find((v) => v.id === taskVersionID.value);
		if (!selected) return;
		const docs: Record<string, unknown> = {};
		for (const st of selected.manifest.definition.stages) {
			const v = knownVersions.value[st.collector_version_id] || (await coApi.version(st.collector_version_id));
			const t = (await coApi.tests(v.collector_id!)).items.find((t) => t.result?.definition_hash === v.definition_hash);
			docs[st.id] = t?.result.snapshots || [];
		}
		trialDocuments.value = JSON.stringify(docs, null, 2);
	});
}
const baseline = ref('');
const dirty = computed(() => {
	if (Object.keys(invalidMappingValues.value).length) return true;
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
	busy.value = true;
	try {
		collectorVersions.value = (await coApi.versions(collectorID.value)).items;
		versionID.value = collectorVersions.value[0]?.id || '';
		versionChanged();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
function versionChanged() {
	inputPreview.value = [];
	savePreview.value = [];
	if (version.value) {
		knownVersions.value[version.value.id] = version.value;
		if (incoming.value)
			for (const f of version.value.definition.input_fields) {
				if (!incoming.value.mapping[f.key] && upstream.value?.definition.output.fields.some((o) => o.key === f.key))
					incoming.value.mapping[f.key] = { kind: 'field', path: 'output.' + f.key };
			}
		tableName.value = name.value || '采集结果';
		if (!inputText.value && selectedID.value === entryID.value) {
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
		inputPreview.value = (
			await taskApi.inputPreview(
				selectedID.value === entryID.value ? versionID.value : stageDefs.value.find((s) => s.id === entryID.value)!.collector_version_id,
				inputs()
			)
		).items;
	});
}
async function previewSave() {
	await guarded(async () => {
		if (!version.value) return;
		const tests = (await coApi.tests(collectorID.value)).items;
		const tested = tests.find((t) => t.result?.definition_hash === version.value?.definition_hash);
		if (!tested) throw new Error('所选采集器版本没有可用测试结果；可发布任务后先隔离试采。');
		const candidates: Candidate[] = tested.result.candidates.slice(0, 100);
		savePreview.value = (await taskApi.savePreview(versionID.value, selectedStage().save, candidates)).decisions;
	});
}
async function saveDraft() {
	await guarded(async () => {
		if (Object.keys(invalidMappingValues.value).length) throw new Error('请修复无效的映射常量');
		const d = definition();
		if (task.value) {
			task.value = await taskApi.save(task.value.id, task.value.revision, d);
		} else {
			task.value = await taskMutation<Task>('createTask', '', '/tasks', { name: name.value, definition: d });
			baseline.value = JSON.stringify({ name: name.value, definition: d });
			await router.replace('/app/co/tasks/' + task.value.id);
		}
		baseline.value = JSON.stringify({ name: task.value!.name, definition: d });
		notice.value = '已保存任务 revision ' + task.value!.revision;
	});
}
async function publish() {
	await guarded(async () => {
		if (!task.value || dirty.value) throw new Error('请先保存全部阶段修改后发布');
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
		if (dirty.value) throw new Error('草稿在确认期间已变化，请保存后再执行');
		const r = await taskMutation<any>('startTask', task.value!.id, '/tasks/' + task.value!.id + '/runs', {
			task_version_id: taskVersionID.value,
			namespace,
			...(namespace === 'trial'
				? { trial: { mode: trialMode.value, ...(trialMode.value === 'offline' ? { documents: JSON.parse(trialDocuments.value) } : {}) } }
				: {}),
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
					baseline.value = JSON.stringify({ name: r.name, definition: r.definition });
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
function reloadTask() {
	baseline.value = JSON.stringify({name:name.value,definition:version.value?definition():null});
	window.location.reload();
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
			entryID.value = d.entry_stage;
			selectedID.value = d.entry_stage;
			stageDefs.value = d.stages;
			edges.value = d.edges;
			const all = d.stages.find((s) => s.id === d.entry_stage)!;
			stageName.value = all.name;
			for (const st of d.stages) {
				knownVersions.value[st.collector_version_id] = await coApi.version(st.collector_version_id);
				if (!tables.value.some((t) => t.id === st.save.table_id)) tables.value.push(await call<DataTable>('/tables/' + st.save.table_id));
			}
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
			budget.value = { ...d.budget, max_dispatches: d.budget.max_dispatches || 10000 };
			onError.value = all.on_error;
			refreshSeconds.value = all.refresh_seconds || 0;
			sourceKind.value = d.source.kind;
			frozenSource.value = d.source;
			inputMode.value = 'json';
			inputText.value = JSON.stringify(d.source.inputs || [], null, 2);
			taskVersionID.value = versions[0]?.id || '';
		} else if (collectorID.value) {
			await loadVersions();
		}
		baseline.value = JSON.stringify({ name: name.value, definition: version.value ? definition() : null });
	})
);
</script>
<style src="./style.css"></style>

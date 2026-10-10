<template>
	<section class="app-card regression-panel" id="sample-regression">
		<div class="pane-heading">
			<h2>全样例回归与版本比较</h2>
			<button :disabled="busy" @click="reload">刷新任务</button>
		</div>
		<p class="muted">
			使用提交时保存的全部样例及预期；不访问网站，不测试导航、登录或动作，也不写入正式数据。旧版本与新规则使用同一批输入。规则有变化后需重新回归。
		</p>
		<div class="command-form-grid">
			<label
				>比较基准<select v-model="baseID" :disabled="busy || !!pending">
					<option value="">请选择发布版本</option>
					<option v-for="v in versions" :key="v.version_id" :value="v.version_id">v{{ v.number }}{{ v.version_id===draft.published_version_id ? '（当前发布）' : '' }} · {{ v.note }}</option>
				</select></label
			>
			<label
				>比较目标<select v-model="targetID" :disabled="busy || !!pending">
					<option value="">当前已保存草稿 · revision {{ draft.revision }}</option>
					<option v-for="v in versions" :key="v.version_id" :value="v.version_id">v{{ v.number }} · {{ v.note }}</option>
				</select></label
			>
		</div>
		<p class="muted">版本选项来自下方版本列表，可加载更多版本后比较。历史版本复制不继承输出确认、样例或调度，原草稿完整保留。</p>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !!pending" @click="start('regression')">回归当前草稿的全部样例</button
			><button :disabled="busy || blocked || !!pending || !baseID" @click="start('comparison')">比较同批输入与规则</button
			><button :disabled="busy || blocked || !!restorePending || !baseID" @click="restore">复制基准版本为新草稿</button>
		</div>
		<p v-if="pending" class="status-warn">
			任务创建响应未确认。<button :disabled="busy" @click="recover">查询原任务</button
			><button :disabled="busy" @click="clearUnknown('job')">结束本地等待</button>
		</p>
		<p v-if="restorePending" class="status-warn">
			复制响应未确认。<button :disabled="busy" @click="recoverRestore">查询原复制</button
			><button :disabled="busy" @click="clearUnknown('restore')">结束本地等待</button>
		</p>
		<p v-if="restored" class="status-good">
			新草稿已创建：<router-link :to="`/app/collectors/${restored.target_collector_id}`">打开新草稿，重新确认样例与输出</router-link>
		</p>
		<p v-if="message" role="status" class="app-error">{{ message }}</p>
		<label v-if="jobs.length"
			>任务历史<select :value="selected?.job_id || ''" :disabled="busy" @change="select(($event.target as HTMLSelectElement).value)">
				<option value="">选择任务</option>
				<option v-for="j in jobs" :key="j.job_id" :value="j.job_id">
					{{ j.kind === 'comparison' ? '比较' : '回归' }} · revision {{ j.collector_revision }} · {{ j.status }} ·
					{{ new Date(j.created_at).toLocaleString() }}
				</option>
			</select></label
		>
		<section v-if="selected">
			<p>
				任务 {{ selected.job_id }} · {{ selected.status }} · {{ selected.completed }}/{{ selected.total }} 样例 ·
				{{ selected.report.verdict || selected.error_code || '等待结果' }}
			</p>
			<p class="muted">冻结时间 {{ new Date(selected.created_at).toLocaleString() }} · 样例批哈希 {{ selected.snapshot_hash }}</p>
			<p v-if="selected.kind==='comparison'">基准 {{ versionLabel(selected.base_version_id) }} → 目标 {{ selected.target_version_id ? versionLabel(selected.target_version_id) : `草稿 revision ${selected.collector_revision}` }}</p>
			<p v-if="!selected.total" class="status-warn">没有保存样例，未验证提取结果；规则差异仍可查看。</p>
			<p v-if="stale" class="status-warn">当前草稿 revision 已变化；此报告保留为历史，不代表当前规则。</p>
			<div class="editor-actions">
				<button :disabled="busy" @click="refreshSelected">查询任务结果</button><button v-if="active" :disabled="busy" @click="cancel">取消任务</button
				><button v-else :disabled="busy" @click="remove">删除此历史任务</button>
			</div>
			<details v-if="selected.report.definition" open>
				<summary>规则差异</summary>
				<DifferenceView :diff="selected.report.definition" />
			</details>
			<p v-if="selected.kind === 'comparison'" class="muted">记录按样例内的索引位置对齐；顺序变化也会显示差异。这不代表正式数据新增、更新或删除。</p>
			<ul class="regression-samples">
				<li v-for="s in selected.report.samples || []" :key="s.sample_id">
					<button @click="sampleID = s.sample_id">
						{{ s.name }} · {{ s.kind }} · {{ s.step_id }}/{{ s.stage }} · 样例 revision {{ s.revision }} · {{ s.target.status }}
						{{ s.target.error_code }}</button
					><small v-if="s.base"> · 基准 {{ s.base.status }} {{ s.base.error_code }}</small>
				</li>
			</ul>
			<section v-if="currentSample">
				<h3>{{ currentSample.name }} · 提取结果</h3>
				<button @click="emit('focus-field', currentSample!.step_id, '', currentSample!.stage)">定位提取步骤</button>
				<p>
					目标：记录 {{ currentSample.target.record_count }} / 有效 {{ currentSample.target.valid_count }} / 无效
					{{ currentSample.target.invalid_count }} · {{ currentSample.target.comparison?.assertion_count || 0 }} 条断言
				</p>
				<p v-if="currentSample.base">
					基准：记录 {{ currentSample.base.record_count }} / 有效 {{ currentSample.base.valid_count }} / 无效 {{ currentSample.base.invalid_count }}
				</p>
				<p v-if="currentSample.target.status === 'unconfigured'" class="status-warn">没有明确预期，不能作为通过证据。</p>
				<ul>
					<li v-for="(d, i) in currentSample.target.comparison?.differences || []" :key="i">
						<button @click="emit('focus-field', currentSample!.step_id, d.field_key || '', currentSample!.stage)">
							{{ d.code }} · 记录 {{ d.record_index ?? '全部' }} · {{ d.field_key }}</button
						><small v-if="d.clipped" :title="`预期 ${d.expected_hash} / 实际 ${d.actual_hash}`"> · 长值已裁剪，悬停查看完整值哈希</small>
						<pre>
预期 {{ d.expected_json }}
实际 {{ d.actual_json }}</pre>
					</li>
				</ul>
				<details v-if="currentSample.base?.comparison">
					<summary>基准预期判断</summary>
					<pre>{{ JSON.stringify(currentSample.base.comparison, null, 2) }}</pre>
				</details>
				<DifferenceView v-if="currentSample.records" :diff="currentSample.records" />
			</section>
			<label v-if="reviewEligible"
				><input
					v-model="acknowledged"
					type="checkbox"
				/>我已查看当前草稿相对当前发布版本的规则及样例差异，确认用于本次发布。实时试采、全样例与输出检查仍需通过。</label
			>
		</section>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { appApi, type Collector, type PublishedVersion, type RegressionJob, type VersionRestore } from './api';
import DifferenceView from './difference-view.vue';
const props = defineProps<{ draft: Collector; versions: PublishedVersion[]; blocked: boolean }>();
const emit = defineEmits<{ (event: 'focus-field', step: string, key: string, stage: string): void }>();
const baseID = ref(props.draft.published_version_id || ''),
	targetID = ref(''),
	jobs = ref<RegressionJob[]>([]),
	selected = ref<RegressionJob>(),
	sampleID = ref(''),
	busy = ref(false),
	message = ref(''),
	pending = ref(''),
	restorePending = ref(''),
	restored = ref<VersionRestore>(),
	acknowledged = ref(false);
let disposed = false,
	timer: ReturnType<typeof setTimeout> | undefined;
const storage = (kind: string) => `scrapio:v22:regression:${props.draft.id}:${kind}`;
const active = computed(() => selected.value && ['queued', 'running'].includes(selected.value.status));
const stale = computed(() => !!selected.value && !selected.value.target_version_id && selected.value.collector_revision !== props.draft.revision);
const currentSample = computed(() => selected.value?.report.samples?.find((s) => s.sample_id === sampleID.value));
const reviewEligible = computed(
	() =>
		!props.blocked &&
		selected.value?.kind === 'comparison' &&
		selected.value.status === 'succeeded' &&
		!stale.value &&
		!selected.value.target_version_id &&
		selected.value.base_version_id === props.draft.published_version_id &&
		!!selected.value.report.definition &&
		!selected.value.report.definition.truncated
);
watch(
	() => [props.draft.revision, props.draft.published_version_id, props.blocked],
	() => (acknowledged.value = false)
);
function error(c: any) {
	return c?.error?.message || c?.message || '请求未确认，请查询原请求';
}
function versionLabel(id:string){const v=props.versions.find(v=>v.version_id===id);return v?`v${v.number}`:id}
function clear(kind: 'job' | 'restore') {
	if (kind === 'job') pending.value = '';
	else restorePending.value = '';
	sessionStorage.removeItem(storage(kind));
}
function clearUnknown(kind: 'job' | 'restore') {
	if (window.confirm('结束本地等待不会撤销服务器操作；请先查询原请求。确认？')) clear(kind);
}
function apply(j: RegressionJob) {
	if (disposed) return;
	const different = selected.value?.job_id !== j.job_id;
	selected.value = j;
	if (different) {
		acknowledged.value = false;
		sampleID.value = j.report.samples?.[0]?.sample_id || '';
	} else if (!sampleID.value) sampleID.value = j.report.samples?.[0]?.sample_id || '';
	jobs.value = [j, ...jobs.value.filter((v) => v.job_id !== j.job_id)];
	schedulePoll();
}
function schedulePoll() {
	if (timer) clearTimeout(timer);
	if (active.value && !disposed)
		timer = setTimeout(() => {
			if (!busy.value) void refreshSelected();
			else schedulePoll();
		}, 2000);
}
async function reload() {
	if (busy.value) return;
	busy.value = true;
	try {
		const page = await appApi.regressions(props.draft.id);
		if (!disposed) jobs.value = page.items;
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function select(id: string) {
	if (!id || busy.value) return;
	busy.value = true;
	try {
		apply(await appApi.regression(id));
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function refreshSelected() {
	if (!selected.value || busy.value) return;
	const id = selected.value.job_id;
	busy.value = true;
	try {
		const j = await appApi.regression(id);
		if (!disposed && selected.value?.job_id === id) apply(j);
	} catch (c) {
		message.value = error(c);
		if (timer) clearTimeout(timer);
		message.value += '；自动查询已暂停，可手动查询。';
	} finally {
		busy.value = false;
	}
}
async function start(kind: 'regression' | 'comparison') {
	if (busy.value || props.blocked || pending.value || (kind === 'comparison' && !baseID.value)) return;
	busy.value = true;
	message.value = '';
	const key = crypto.randomUUID();
	pending.value = key;
	sessionStorage.setItem(storage('job'), key);
	try {
		const j = await appApi.createRegression(
			props.draft.id,
			kind,
			{
				expected_revision: props.draft.revision,
				...(kind === 'comparison' ? { base_version_id: baseID.value, target_version_id: targetID.value } : {}),
			},
			key
		);
		if (!disposed) {
			clear('job');
			apply(j);
		}
	} catch (c: any) {
		if (c?.error && !['INTERNAL', 'REGRESSION_TIMEOUT'].includes(c.error.code)) clear('job');
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function recover() {
	if (busy.value || !pending.value) return;
	busy.value = true;
	try {
		const j = await appApi.regressionByKey(pending.value);
		if (!disposed) {
			apply(j);
			clear('job');
		}
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function cancel() {
	if (!selected.value || busy.value) return;
	busy.value = true;
	try {
		apply(await appApi.cancelRegression(selected.value.job_id));
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function remove() {
	if (
		!selected.value ||
		active.value ||
		busy.value ||
		!window.confirm('删除此离线报告及其冻结输入？已发布版本保留差异确认摘要；删除后原任务键无法查询。')
	)
		return;
	busy.value = true;
	const id = selected.value.job_id;
	try {
		await appApi.deleteRegression(id);
		if (!disposed) {
			jobs.value = jobs.value.filter((j) => j.job_id !== id);
			selected.value = undefined;
			acknowledged.value = false;
		}
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function restore() {
	if (
		busy.value ||
		props.blocked ||
		restorePending.value ||
		!baseID.value ||
		!window.confirm('复制历史版本为独立新草稿，原草稿、版本、运行及调度保持原样。新草稿不继承样例与输出确认，需重新配置检查。确认？')
	)
		return;
	busy.value = true;
	const key = crypto.randomUUID();
	restorePending.value = key;
	sessionStorage.setItem(storage('restore'), key);
	try {
		const v = await appApi.restoreVersion(
			props.draft.id,
			{ expected_revision: props.draft.revision, version_id: baseID.value, confirmed: true },
			key
		);
		if (!disposed) {
			restored.value = v;
			clear('restore');
		}
	} catch (c: any) {
		if (c?.error && !['INTERNAL', 'REGRESSION_TIMEOUT'].includes(c.error.code)) clear('restore');
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
async function recoverRestore() {
	if (busy.value || !restorePending.value) return;
	busy.value = true;
	try {
		const v = await appApi.versionRestoreByKey(restorePending.value);
		if (!disposed) {
			restored.value = v;
			clear('restore');
		}
	} catch (c) {
		message.value = error(c);
	} finally {
		busy.value = false;
	}
}
onMounted(async () => {
	pending.value = sessionStorage.getItem(storage('job')) || '';
	restorePending.value = sessionStorage.getItem(storage('restore')) || '';
	await reload();
	if (!disposed && jobs.value.length) void select(jobs.value[0].job_id);
});
onBeforeUnmount(() => {
	disposed = true;
	if (timer) clearTimeout(timer);
});
defineExpose({
	reviewID: () => (reviewEligible.value && acknowledged.value ? selected.value!.job_id : ''),
	hasUnsavedChanges: () => busy.value || !!pending.value || !!restorePending.value,
});
</script>
<style scoped>
.regression-panel select {
	max-width: 100%;
	padding: 8px;
}
.regression-samples {
	padding-left: 18px;
}
.regression-samples li {
	margin: 8px 0;
}
.regression-panel pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
	max-height: 240px;
	overflow: auto;
	font-size: 12px;
}
</style>

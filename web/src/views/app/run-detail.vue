<template>
	<div class="app-page">
		<router-link to="/app/runs">← 所有运行</router-link>
		<h1>运行详情</h1>
		<button :disabled="loading" @click="reload">刷新状态和证据</button>
		<p v-if="loading" role="status">读取中…</p>
		<p v-if="message" class="app-error">{{ message }}</p>
		<template v-if="run"
			><section class="app-card">
				<h2>{{ runStatus(run.status) }} · 固定发布 v{{ run.version_number }}</h2>
				<p>
					来源：{{ run.retry_of ? '重试' : { manual: '手动', schedule: '定时', api: 'API' }[run.trigger_source] }} · {{ run.run_id }} ·
					{{ new Date(run.created_at).toLocaleString() }}
				</p>
				<p>
					文档 {{ run.summary.pages || 0 }} · 候选 {{ run.summary.candidates || 0 }} ·
					{{ run.summary.committed ? '正式数据已提交' : '尚未提交本批正式数据' }}
				</p>
				<p>
					为什么结束：{{ run.summary.stop_reason || '执行中' }} · 缺失路径：{{ run.summary.failed_step_id || '无已报告失败' }} /
					{{ run.summary.failed_stage || '—' }}
				</p>
				<p v-for="w in run.summary.warnings || []" :key="w" class="status-warn">{{ w }}</p>
				<router-link :to="`/app/collectors/${run.collector_id}`">打开采集方案</router-link>
				<button v-if="run.controls?.can_cancel" :disabled="loading || run.cancel_requested" @click="cancelConfirm = true">
					{{ run.cancel_requested ? '正在停止…' : '停止后续执行' }}
				</button>
				<p v-if="run.cancel_requested && run.status === 'running'" role="status">
					取消请求已接受，等待当前尝试结束；旧尝试已不能继续保存证据或写入。
				</p>
				<div v-if="cancelConfirm" class="status-warn">
					<p>确认停止后续执行和本批写入？取消不能撤销网站动作；若提交已完成，会返回已完成状态。</p>
					<button :disabled="loading" @click="cancel">确认停止</button><button :disabled="loading" @click="cancelConfirm = false">返回</button>
				</div>
				<p class="muted">停止不能撤销已发生的网站动作。执行中可手动刷新，完成后结果持久保留。</p>
			</section>
			<RunCompletion :run="run" />
			<RunDiagnostics :key="`diagnostics:${run.run_id}:${run.event_seq}`" :run="run" />
			<RunRetry :run="run" />
			<RunCheckpoints :run-id="run.run_id" :event-seq="run.event_seq" />
			<details :open="route.query.version === '1'">
				<summary>固定发布版本与规则</summary>
				<template v-if="version"
					><p>{{ version.name }} · 发布 v{{ version.number }} · {{ version.created_at }}</p>
					<p>{{ version.note || '无发布说明' }}</p>
					<pre>{{ JSON.stringify(version.definition, null, 2) }}</pre>
					<pre>{{ JSON.stringify(version.output_schema, null, 2) }}</pre>
				</template>
			</details>
			<h2>运行尝试</h2>
			<p v-if="!attempts.length">尚未开始执行。</p>
			<article v-for="a in attempts" :key="a.attempt" class="trace-item">
				attempt {{ a.attempt }} · {{ runStatus(a.status) }} · {{ a.started_at }} → {{ a.finished_at || '执行中' }}
				<details>
					<summary>尝试摘要</summary>
					<pre>{{ a.summary_json }}</pre>
				</details>
			</article>
			<button v-if="attemptCursor" :disabled="loading" @click="moreAttempts">更多运行尝试</button>
			<RunInspector :key="`pages:${run.run_id}:${run.event_seq}`" :run-id="run.run_id" />
			<details>
				<summary>运行摘要与缺失诊断</summary>
				<pre>{{ JSON.stringify({ ...run.summary, writes: undefined }, null, 2) }}</pre>
			</details></template
		>
	</div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type FormalRun, type PublishedVersion, type DataRow } from './api';
import RunCompletion from './run-completion.vue';
import RunInspector from './run-inspector.vue';
import RunDiagnostics from './run-diagnostics.vue';
import RunRetry from './run-retry.vue';
import RunCheckpoints from './run-checkpoints.vue';
import { readError, runStatus } from './data-read';
const route = useRoute(),
	run = ref<FormalRun>(),
	version = ref<PublishedVersion>(),
	attempts = ref<DataRow[]>([]),
	attemptCursor = ref(''),
	cancelConfirm = ref(false),
	loading = ref(false),
	message = ref('');
let epoch = 0;
async function reload() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	try {
		const id = String(route.params.id);
		const [r, a] = await Promise.all([appApi.run(id), appApi.runAttempts(id)]);
		if (token !== epoch) return;
		run.value = r;
		attemptCursor.value = a.next_cursor;
		attempts.value = a.items;
		version.value = undefined;
		const v = await appApi.version(r.version_id);
		if (token === epoch) version.value = v;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function moreAttempts() {
	if (loading.value || !run.value) return;
	const token = epoch;
	loading.value = true;
	try {
		const p = await appApi.runAttempts(run.value.run_id, attemptCursor.value);
		if (token === epoch) {
			attempts.value.push(...p.items);
			attemptCursor.value = p.next_cursor;
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function cancel() {
	if (!run.value || loading.value) return;
	const token = epoch;
	loading.value = true;
	try {
		const r = await appApi.cancelRun(run.value.run_id);
		if (token === epoch) {
			run.value = r;
			cancelConfirm.value = false;
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
watch(
	() => route.params.id,
	() => {
		run.value = undefined;
		version.value = undefined;
		attempts.value = [];
		attemptCursor.value = '';
		cancelConfirm.value = false;
		void reload();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>

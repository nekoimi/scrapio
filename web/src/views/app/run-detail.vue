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
					来源：{{ { manual: '手动', schedule: '定时', api: 'API' }[run.trigger_source] }} · {{ run.run_id }} ·
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
				<button v-if="['queued', 'running'].includes(run.status)" :disabled="loading || run.cancel_requested" @click="cancel">
					{{ run.cancel_requested ? '正在停止…' : '停止后续执行' }}
				</button>
				<p class="muted">停止不能撤销已发生的网站动作。执行中可手动刷新，完成后结果持久保留。</p>
			</section>
			<RunCompletion :run="run" />
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
			<h2>实际页面证据</h2>
			<p class="muted">仅展示已持久化的文档。动作失败但未取得文档的页面不出现在此列表，失败原因见运行诊断。没有截图时不展示截图。</p>
			<p v-if="!pages.length">暂无保存的页面文档。</p>
			<article v-for="p in pages" :key="p.page_id" class="trace-item">
				<router-link :to="`/app/pages/${p.page_id}`">{{ meta(p).stage }} · {{ meta(p).step_id }} · 列表页 {{ meta(p).list_page }}</router-link>
				<p class="source-url">{{ meta(p).source_url }}</p>
				<small>attempt {{ p.attempt }} · {{ p.evidence_status === 'stored' ? '文档已保存，读取时校验' : '文档不可用' }}</small>
			</article>
			<button v-if="cursor" :disabled="loading" @click="morePages">更多页面证据</button>
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
import RunCheckpoints from './run-checkpoints.vue';
import { readError, runStatus, parseJSON } from './data-read';
const route = useRoute(),
	run = ref<FormalRun>(),
	version = ref<PublishedVersion>(),
	pages = ref<DataRow[]>([]),
	attempts = ref<DataRow[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref('');
let epoch = 0;
const meta = (p: DataRow) => parseJSON<Record<string, any>>(p.metadata_json, {});
async function reload() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	try {
		const id = String(route.params.id);
		const [r, p, a] = await Promise.all([appApi.run(id), appApi.pages(id), appApi.runAttempts(id)]);
		if (token !== epoch) return;
		run.value = r;
		pages.value = p.items;
		cursor.value = p.next_cursor;
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
async function morePages() {
	if (loading.value || !run.value) return;
	const token = epoch;
	loading.value = true;
	try {
		const p = await appApi.pages(run.value.run_id, cursor.value);
		if (token === epoch) {
			pages.value.push(...p.items);
			cursor.value = p.next_cursor;
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
		if (token === epoch) run.value = r;
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
		pages.value = [];
		attempts.value = [];
		cursor.value = '';
		void reload();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>

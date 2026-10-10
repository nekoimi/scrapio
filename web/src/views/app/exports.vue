<template>
	<div class="app-page">
		<router-link :to="`/app/data/${id}`">← 数据表</router-link>
		<div class="eyebrow">SCRAPIO / EXPORTS</div>
		<h1>导出任务</h1>
		<p class="lead">导出确认时的固定查询结果。文件保留 24 小时，任务记录保留 7 天。</p>
		<button :disabled="loading" @click="reload">刷新状态</button>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<p v-if="loading">读取中…</p>
		<p v-if="!loading && !items.length">暂无导出任务。在数据表应用筛选后创建导出。</p>
		<article v-for="job in items" :key="job.export_id" class="app-card" :id="`export-${job.export_id}`">
			<h2>{{ label(job.status) }} · {{ job.format.toUpperCase() }}</h2>
			<p>{{ job.export_id }} · {{ job.progress }} / {{ job.row_count }} 条 · {{ job.file_bytes }} 字节</p>
			<p>冻结 {{ new Date(job.captured_at).toLocaleString() }} · 文件有效至 {{ new Date(job.expires_at).toLocaleString() }}</p>
			<p v-if="job.error_code" class="app-error">{{ job.error_code }}。可回数据表重新查询并创建新任务；原任务不会自动重试。</p>
			<button v-if="job.status === 'succeeded'" :disabled="loading || working === job.export_id" @click="download(job)">下载文件</button
			><button v-if="['queued', 'running'].includes(job.status)" :disabled="loading || !!working" @click="cancel(job)">取消导出</button>
			<details>
				<summary>冻结条件、列与 Schema</summary>
				<pre>{{ JSON.stringify({ query: job.query, schema: job.schema }, null, 2) }}</pre>
			</details>
		</article>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更多</button>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type DataExport } from './api';
import { readError } from './data-read';
const route = useRoute(),
	id = computed(() => String(route.params.id)),
	items = ref<DataExport[]>([]),
	cursor = ref(''),
	loading = ref(false),
	working = ref(''),
	message = ref('');
let epoch = 0;
const label = (s: string) =>
	(({ queued: '排队中', running: '生成中', succeeded: '可下载', failed: '失败', cancelled: '已取消', expired: '已过期' }) as Record<string, string>)[
		s
	] || s;
async function load(more = false) {
	if (loading.value) return;
	loading.value = true;
	const token = epoch;
	try {
		const page = await appApi.dataExports(id.value, more ? cursor.value : '');
		if (token !== epoch) return;
		items.value = more ? [...items.value, ...page.items] : page.items;
		cursor.value = page.next_cursor;
		if (!more && route.query.export && !items.value.some((j) => j.export_id === route.query.export)) {
			const job = await appApi.dataExport(String(route.query.export));
			if (token === epoch && job.table_id === id.value) items.value.unshift(job);
		}
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
function reload() {
	if (working.value) return;
	epoch++;
	loading.value = false;
	message.value = '';
	void load();
}
async function cancel(job: DataExport) {
	if (working.value || !window.confirm('取消导出将停止文件生成，不影响正式记录。确认取消？')) return;
	working.value = job.export_id;
	const token = epoch;
	try {
		const result = await appApi.cancelDataExport(job.export_id);
		if (token === epoch) items.value = items.value.map((j) => (j.export_id === result.export_id ? result : j));
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	} finally {
		if (token === epoch) working.value = '';
	}
}
async function download(job: DataExport) {
	working.value = job.export_id;
	const token = epoch;
	try {
		const blob = await appApi.downloadDataExport(job.export_id);
		if (token !== epoch) return;
		const url = URL.createObjectURL(blob),
			link = document.createElement('a');
		link.href = url;
		link.download = `scrapio-${job.export_id}.${job.format}`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	} catch (cause: any) {
		if (token === epoch) {
			if (cause?.response?.data instanceof Blob) {
				const text = await cause.response.data.text();
				try {
					message.value = JSON.parse(text)?.error?.message || '下载失败';
				} catch {
					message.value = '下载失败，请刷新状态';
				}
			} else message.value = readError(cause);
		}
	} finally {
		if (token === epoch) working.value = '';
	}
}
watch(
	id,
	() => {
		epoch++;
		working.value = '';
		loading.value = false;
		items.value = [];
		cursor.value = '';
		void load();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>
<style scoped>
pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
}
</style>

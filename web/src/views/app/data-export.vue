<template>
	<section class="app-card">
		<h2>导出当前查询</h2>
		<router-link :to="`/app/data/${tableId}/exports`">查看导出任务</router-link>
		<p v-if="snapshot">
			{{ snapshot.total }} 条 · {{ snapshot.query.columns.length }} 列 · 冻结时间 {{ new Date(snapshot.captured_at).toLocaleString() }} · 快照有效至
			{{ new Date(snapshot.expires_at).toLocaleString() }}
		</p>
		<p v-else>先成功应用筛选，取得可确认的查询快照。</p>
		<label
			>格式<select v-model="format" :disabled="busy || !!pending">
				<option value="csv">CSV（UTF-8，含表头）</option>
				<option value="json">JSON（保留原始数字精度）</option>
			</select></label
		>
		<details v-if="snapshot">
			<summary>本次筛选、排序和列</summary>
			<pre>{{ JSON.stringify(snapshot.query, null, 2) }}</pre>
		</details>
		<label><input v-model="confirmed" type="checkbox" :disabled="busy || !!pending || !snapshot" />确认导出以上快照，不纳入后续采集更新</label>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !snapshot || !confirmed || !!pending" @click="create">创建导出任务</button
			><button v-if="pending" :disabled="busy" @click="recover">查询原请求</button
			><button v-if="pending" :disabled="busy" @click="discard">结束本地等待</button>
		</div>
		<p class="muted">
			最多 10000 条/16 MiB。后台生成，文件保留 24 小时，任务记录保留 7 天。CSV 文本中的公式起始符会加单引号；JSON
			不修改原值。修改条件后先应用，再确认导出。
		</p>
		<p v-if="result">
			{{ status(result.status) }} · {{ result.row_count }} 条
			<router-link :to="`/app/data/${tableId}/exports?export=${result.export_id}`">查看任务与下载</router-link>
		</p>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type QueryPage, type DataExport } from './api';
import { readError } from './data-read';
const props = defineProps<{ tableId: string; snapshot?: QueryPage; blocked: boolean }>();
const format = ref<'csv' | 'json'>('csv'),
	confirmed = ref(false),
	busy = ref(false),
	pending = ref(''),
	result = ref<DataExport>(),
	message = ref('');
let epoch = 0;
const storage = () => `scrapio:v22:export:${props.tableId}`;
const status = (s: string) =>
	(
		({ queued: '排队中', running: '正在生成', succeeded: '可下载', failed: '生成失败', cancelled: '已取消', expired: '已过期' }) as Record<
			string,
			string
		>
	)[s] || s;
async function create() {
	const snap = props.snapshot;
	if (busy.value || props.blocked || !snap || !confirmed.value || pending.value) return;
	busy.value = true;
	const token = epoch,
		key = crypto.randomUUID();
	pending.value = key;
	sessionStorage.setItem(storage(), key);
	try {
		const value = await appApi.createDataExport(
			props.tableId,
			{ format: format.value, snapshot_id: snap.snapshot_id, query: snap.query, confirmed: true },
			key
		);
		if (token !== epoch) return;
		result.value = value;
		pending.value = '';
		sessionStorage.removeItem(storage());
		message.value = '已创建任务；完成后在任务页下载。';
	} catch (cause: any) {
		if (token !== epoch) return;
		message.value = readError(cause) + '；结果不确定时只查询原请求。';
		if (cause?.error && !['INTERNAL', 'DATA_TIMEOUT'].includes(cause.error.code)) {
			pending.value = '';
			sessionStorage.removeItem(storage());
		}
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function recover() {
	if (busy.value || !pending.value) return;
	busy.value = true;
	const token = epoch;
	try {
		const value = await appApi.dataExportByKey(pending.value);
		if (token !== epoch) return;
		if (value.table_id !== props.tableId) throw Error('原任务不属于此数据表');
		result.value = value;
		pending.value = '';
		sessionStorage.removeItem(storage());
		message.value = '已恢复原任务，没有创建新导出。';
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function discard() {
	if (window.confirm('结束本地等待不会撤销导出。请先查询原请求并检查任务列表，确认结束？')) {
		pending.value = '';
		sessionStorage.removeItem(storage());
	}
}
watch([() => props.snapshot?.snapshot_id, format], () => (confirmed.value = false));
watch(
	() => props.tableId,
	() => {
		epoch++;
		busy.value = false;
		result.value = undefined;
		pending.value = sessionStorage.getItem(storage()) || '';
		message.value = pending.value ? '有未确认的导出请求，请先查询。' : '';
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
defineExpose({ hasUnsavedChanges: () => busy.value || !!pending.value });
</script>
<style scoped>
label {
	display: block;
	margin: 12px 0;
}
pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
}
</style>

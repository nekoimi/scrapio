<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / SETTINGS / OPERATIONS</div>
		<h1>操作记录</h1>
		<p class="lead">查看当前账号的变更请求、操作者和结果。Schema 与保留策略的提交记录和变更在同一事务中保存。</p>
		<p class="muted">
			“请求成功”表示接口成功响应；“已提交”表示事务提交证据。“处理中 /
			结果未知”需要回到对应页面查询原请求。这里保留标识和数量，不保存请求正文、凭据值或采集内容。
		</p>
		<div class="editor-actions"><button :disabled="loading" @click="load(false)">刷新记录</button></div>
		<p v-if="error" class="app-error" role="alert">{{ error }}</p>
		<p v-if="loading" role="status">读取中…</p>
		<div v-if="rows.length" class="record-preview-table">
			<table>
				<thead>
					<tr>
						<th>时间</th>
						<th>操作者</th>
						<th>动作</th>
						<th>对象</th>
						<th>结果</th>
						<th>请求/安全摘要</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="r in rows" :key="r.id">
						<td>{{ new Date(r.created_at).toLocaleString() }}</td>
						<td>
							{{ r.username }}<small>{{ r.actor_id }}</small>
						</td>
						<td>{{ r.action }}</td>
						<td>{{ r.resource_id || '—' }}</td>
						<td>{{ label(r.status) }} {{ r.http_status !== '0' ? r.http_status : '' }}</td>
						<td>
							{{ r.request_id }}
							<pre v-if="r.details !== '{}'">{{ r.details }}</pre>
						</td>
					</tr>
				</tbody>
			</table>
		</div>
		<p v-else-if="!loading && !error" class="app-card">尚无操作记录。启用此版本后的新产品变更会出现在这里。</p>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更早记录</button>
	</div>
</template>
<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { appApi, type DataRow } from './api';
import { readError } from './data-read';
const rows = ref<DataRow[]>([]),
	cursor = ref(''),
	loading = ref(false),
	error = ref('');
let epoch = 0;
const label = (v: string) =>
	({ started: '处理中 / 待核实', succeeded: '请求成功', rejected: '请求被拒绝', outcome_unknown: '结果未知', committed: '已提交' })[v] || v;
async function load(more: boolean) {
	if (loading.value) return;
	const token = ++epoch;
	loading.value = true;
	error.value = '';
	try {
		const r = await appApi.operations(more ? cursor.value : '');
		if (token === epoch) {
			rows.value = more ? [...rows.value, ...r.items] : r.items;
			cursor.value = r.next_cursor;
		}
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
onMounted(() => void load(false));
onBeforeUnmount(() => epoch++);
</script>

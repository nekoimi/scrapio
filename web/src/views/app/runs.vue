<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / RUNS</div>
		<h1>运行</h1>
		<p class="lead">查看正式采集的状态、结果和结束原因。</p>
		<button :disabled="loading" @click="reload">刷新</button>
		<p v-if="loading" role="status">正在读取…</p>
		<p v-if="message" class="app-error">{{ message }}</p>
		<div class="record-preview-table">
			<table>
				<thead>
					<tr>
						<th>运行</th>
						<th>固定版本</th>
						<th>来源</th>
						<th>状态</th>
						<th>写入决策</th>
						<th>结束原因</th>
						<th>创建时间</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="r in items" :key="r.run_id">
						<td>
							<router-link :to="`/app/runs/${r.run_id}`">{{ r.run_id.slice(0, 8) }}</router-link>
						</td>
						<td>
							<router-link :to="`/app/collectors/${r.collector_id}`">v{{ r.version_number }} · 方案 {{ r.collector_id }}</router-link>
						</td>
						<td>{{ { manual: '手动', schedule: '定时', api: 'API' }[r.trigger_source] }}</td>
						<td>{{ runStatus(r.status) }}</td>
						<td>
							{{ r.summary.committed ? '已提交' : '未提交' }} · 新增 {{ r.summary.counts?.created || 0 }} / 更新
							{{ r.summary.counts?.updated || 0 }} / 未变 {{ r.summary.counts?.unchanged || 0 }}
						</td>
						<td>{{ r.summary.stop_reason || '等待结束' }}</td>
						<td>{{ new Date(r.created_at).toLocaleString() }}</td>
					</tr>
				</tbody>
			</table>
		</div>
		<section v-if="!loading && !items.length" class="app-card">
			<h2>还没有正式运行</h2>
			<p>发布一个采集方案，并确认范围后运行。</p>
			<router-link to="/app/collectors">打开采集方案</router-link>
		</section>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更多</button>
	</div>
</template>
<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { appApi, type FormalRun } from './api';
import { readError, runStatus } from './data-read';
const items = ref<FormalRun[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref('');
let epoch = 0;
async function load(more = false) {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	message.value = '';
	try {
		const r = await appApi.runs('', more ? cursor.value : '');
		if (token === epoch) {
			items.value = more ? [...items.value, ...r.items] : r.items;
			cursor.value = r.next_cursor;
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
function reload() {
	epoch++;
	loading.value = false;
	void load();
}
onMounted(reload);
onBeforeUnmount(() => epoch++);
</script>

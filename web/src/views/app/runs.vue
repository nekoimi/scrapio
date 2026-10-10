<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / RUNS</div>
		<h1>运行中心</h1>
		<p class="lead">查看正式采集的状态、结果和结束原因。</p>
		<section v-if="completionWindow" class="app-card">
			<strong>仅查看指定时间的已提交运行</strong>
			<p>{{ completionWindow.since }} 至 {{ completionWindow.until }}；按完成时间筛选，与首页提交统计同范围。</p>
			<button :disabled="loading" @click="clearWindow">清除时间及提交筛选</button>
		</section>
		<section class="app-card">
			<p v-if="stats">
				保留历史 {{ stats.total }} · 排队 {{ stats.queued }} · 执行中 {{ stats.running }} · 完成 {{ stats.succeeded }} · 有限覆盖
				{{ stats.limited }} · 未完整通过 {{ stats.partial }} · 失败 {{ stats.failed }} · 取消 {{ stats.cancelled }}
			</p>
			<p class="muted">统计为当前方案范围的全部保留历史，与状态/来源筛选和已加载分页无关。</p>
			<div class="command-form-grid">
				<label>方案 ID<input v-model="collectorFilter" placeholder="留空查看全部" /></label>
				<label
					>状态<select v-model="statusFilter">
						<option value="">全部</option>
						<option v-for="s in statuses" :key="s" :value="s">{{ runStatus(s) }}</option>
					</select></label
				>
				<label
					>来源<select v-model="sourceFilter">
						<option value="">全部</option>
						<option value="manual">手动</option>
						<option value="schedule">定时</option>
						<option value="api">API</option>
						<option value="retry">重试</option>
					</select></label
				>
			</div>
			<button :disabled="loading" @click="reload">应用筛选 / 刷新</button>
		</section>
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
						<td>{{ r.retry_of ? '重试' : { manual: '手动', schedule: '定时', api: 'API' }[r.trigger_source] }}</td>
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
		<section v-if="!loading && !message && !items.length" class="app-card">
			<h2>当前条件没有运行</h2>
			<p>发布一个采集方案，并确认范围后运行。</p>
			<router-link to="/app/collectors">打开采集方案</router-link>
		</section>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更多</button>
	</div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { appApi, type FormalRun } from './api';
import { readError, runStatus } from './data-read';
const items = ref<FormalRun[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref(''),
	collectorFilter = ref(''),
	statusFilter = ref(''),
	sourceFilter = ref(''),
	stats = ref<Record<string, number>>();
const statuses = ['queued', 'running', 'succeeded', 'limited', 'partial', 'failed', 'cancelled'];
let applied = { collector: '', status: '', source: '' };
const route = useRoute(),
	router = useRouter();
const completionWindow = ref<{ committed: boolean; since: string; until: string }>();
let epoch = 0;
async function load(more = false) {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	message.value = '';
	try {
		const [r, counts] = await Promise.all([
			appApi.runs(applied.collector, more ? cursor.value : '', applied.status, applied.source, completionWindow.value),
			appApi.runStatistics(applied.collector),
		]);
		if (token === epoch) stats.value = counts.counts;
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
	items.value = [];
	cursor.value = '';
	stats.value = undefined;
	if (collectorFilter.value && !/^[1-9][0-9]*$/.test(collectorFilter.value)) {
		message.value = '方案 ID 需为正整数';
		return;
	}
	applied = { collector: collectorFilter.value, status: statusFilter.value, source: sourceFilter.value };
	void load();
}
function clearWindow() {
	void router.replace({ query: { collector_id: collectorFilter.value, status: statusFilter.value, source: sourceFilter.value } });
}
watch(
	() => route.query,
	() => {
		collectorFilter.value = typeof route.query.collector_id === 'string' ? route.query.collector_id : '';
		statusFilter.value = typeof route.query.status === 'string' ? route.query.status : '';
		sourceFilter.value = typeof route.query.source === 'string' ? route.query.source : '';
		completionWindow.value =
			route.query.committed === '1' ? { committed: true, since: String(route.query.since || ''), until: String(route.query.until || '') } : undefined;
		reload();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>

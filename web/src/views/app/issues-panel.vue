<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>异常待办</h2>
			<button :disabled="busy" @click="load(false)">刷新问题</button>
		</div>
		<label
			>状态<select v-model="status" :disabled="busy" @change="load(false)">
				<option value="active">未解决</option>
				<option value="open">待处理</option>
				<option value="ready">待确认恢复</option>
				<option value="resolved">已恢复</option>
				<option value="all">全部</option>
			</select></label
		>
		<p class="muted">同一方案的同类问题聚合；修复后先回归/比较/发布，再用正式运行验证恢复。观察可能有延迟，暂无问题不代表最近运行已评估。</p>
		<p v-if="error" class="app-error">{{ error }}</p>
		<article v-for="i in items" :key="i.issue_id" class="issue-row">
			<router-link :to="`/app/issues/${i.issue_id}`"
				><strong>{{ i.message }}</strong></router-link
			>
			<p>
				{{ i.code }} · {{ i.status === 'ready' ? '待确认恢复' : i.status === 'resolved' ? '已恢复' : '待处理' }} · 出现 {{ i.occurrences }} 次 ·
				正常证据 {{ i.recovery_streak }} 次
			</p>
			<router-link :to="`/app/collectors/${i.collector_id}`">打开受影响方案</router-link>
			<router-link v-if="i.last_run_id" :to="`/app/runs/${i.last_run_id}`">查看实际失败/异常运行</router-link>
		</article>
		<p v-if="!busy && !error && !items.length">暂无匹配的已记录问题。</p>
		<button v-if="cursor" :disabled="busy" @click="load(true)">加载更多</button>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type QualityIssue } from './api';
const props = defineProps<{ collectorId?: string }>();
const items = ref<QualityIssue[]>([]),
	status = ref('active'),
	cursor = ref(''),
	busy = ref(false),
	error = ref('');
let epoch = 0;
async function load(more: boolean) {
	if (busy.value) return;
	busy.value = true;
	error.value = '';
	const token = ++epoch;
	try {
		const v = await appApi.qualityIssues(props.collectorId || '', status.value, more ? cursor.value : '');
		if (token === epoch) {
			items.value = more ? [...items.value, ...v.items] : v.items;
			cursor.value = v.next_cursor;
		}
	} catch (e: any) {
		if (token === epoch) error.value = e?.error?.message || e?.message || '问题列表不可用';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => props.collectorId,
	() => {
		epoch++;
		busy.value = false;
		items.value = [];
		cursor.value = '';
		void load(false);
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>
<style scoped>
.issue-row {
	padding: 16px 0;
	border-bottom: 1px solid #e5e9f0;
}
.issue-row a {
	margin-right: 14px;
}
.issue-row p {
	font-size: 13px;
	color: #697890;
}
</style>

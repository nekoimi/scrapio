<template>
	<section class="run-checkpoints">
		<div class="pane-heading">
			<h3>连续运行检查点</h3>
			<button :disabled="loading" @click="reload">刷新检查点</button>
		</div>
		<p class="muted">检查点是已保存的执行证据，正式记录是否提交以运行结果为准。中断后保留最后状态，不自动重放点击或请求。</p>
		<p v-if="loading" role="status">读取中…</p>
		<p v-if="message" class="app-error">{{ message }}</p>
		<p v-if="!loading && !message && !items.length">暂无连续运行检查点；旧运行不会补造检查点。</p>
		<article v-for="item in items" :key="item.checkpoint_id" class="trace-item">
			<strong>{{ stateLabel(item.state) }} · {{ cp(item).step_id }} · 列表轮次 {{ cp(item).list_page }}</strong>
			<p>
				文档 {{ cp(item).pages }} · 输出候选 {{ cp(item).candidates }} · 已访问详情 {{ cp(item).details }} · 跳过重复候选
				{{ cp(item).duplicate_records }}
			</p>
			<p class="muted">{{ item.created_at }} · attempt {{ item.attempt }} · {{ cp(item).stop_reason || '执行进度' }}</p>
			<p v-if="item.state === 'action_pending'" class="status-warn">
				此记录位于动作派发前。仅凭检查点无法判断网站动作是否已发生，请结合后续文档和事件。
			</p>
			<p v-if="item.state === 'awaiting_page'" class="status-warn">动作回执已确认，下一轮文档尚未保存。</p>
			<p v-if="cp(item).next_target" class="source-url">
				下一目标：{{ cp(item).next_target === 'click' ? '页面上的翻页 / 加载更多按钮' : cp(item).next_target }}
			</p>
			<router-link :to="`/app/pages/${item.document_id}`">查看对应列表证据</router-link>
		</article>
		<button v-if="cursor" :disabled="loading" @click="load(true)">更早的检查点</button>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type DataRow, type RunCheckpoint } from './api';
import { parseJSON, readError } from './data-read';
const props = defineProps<{ runId: string; eventSeq?: number }>();
const items = ref<DataRow[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref('');
let epoch = 0;
const cp = (row: DataRow) => parseJSON<Partial<RunCheckpoint>>(row.checkpoint_json, {});
const stateLabel = (s: string) =>
	(
		({
			page_captured: '本轮列表已保存',
			page_complete: '本轮范围处理结束',
			action_pending: '准备派发动作',
			awaiting_page: '等待下一轮页面',
			stopped: '循环已停止',
		}) as Record<string, string>
	)[s] || s;
async function load(more = false) {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	message.value = '';
	try {
		const r = await appApi.runCheckpoints(props.runId, more ? cursor.value : '');
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
watch(
	() => props.runId,
	() => {
		items.value = [];
		cursor.value = '';
		reload();
	},
	{ immediate: true }
);
watch(
	() => props.eventSeq,
	() => {
		if (!loading.value) reload();
	}
);
onBeforeUnmount(() => epoch++);
</script>

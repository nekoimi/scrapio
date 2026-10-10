<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>覆盖与执行诊断</h2>
			<button :disabled="busy" @click="load(false)">刷新诊断</button>
		</div>
		<p v-if="message" class="app-error">{{ message }}</p>
		<div v-if="coverage" class="coverage-counts">
			<span
				>已保存文档 <strong>{{ coverage.captured_documents }}</strong></span
			>
			<span
				>已提取记录 <strong>{{ coverage.extracted_records }}</strong></span
			>
			<span
				>有效 <strong>{{ coverage.valid_records }}</strong></span
			>
			<span
				>无效 <strong>{{ coverage.invalid_records }}</strong></span
			>
			<span
				>本次写入 <strong>{{ written }}</strong></span
			>
			<span>未覆盖 <strong>未知</strong></span>
		</div>
		<p class="muted">
			提取计数覆盖所有已保存列表/详情文档，包含中间结果和重复项；输出候选
			{{ run.summary.candidates || 0 }} 为输出步骤去重后的统计。站点总量和未覆盖页面数未知，不推算完成率。
		</p>
		<p :class="run.status === 'succeeded' ? 'status-good' : 'status-warn'">{{ explanation }}</p>
		<p v-if="run.summary.failed_step_id || run.summary.failed_stage" class="status-error">
			失败定位：{{ run.summary.failed_step_id || '入口' }} / {{ run.summary.failed_stage || '未知阶段' }} · {{ run.summary.stop_reason }}
		</p>
		<p>
			最后执行位置：{{ run.current_step_id || '入口' }} / {{ run.current_stage || '排队' }}。{{
				run.summary.committed ? '本批输出已原子提交。' : '本批尚未提交正式记录。'
			}}
		</p>
		<p v-if="run.status === 'limited'">有限覆盖保留已知边界：{{ run.summary.stop_reason }}；实际写入以写入计数为准，不能等同于采集完整网站。</p>
		<p v-if="run.status === 'partial'">已有页面证据，但路径或输出校验未完整通过；不会把中间候选冒充正式数据。</p>
		<details v-if="run.summary.output">
			<summary>输出映射与写入检查证据</summary>
			<pre>{{ JSON.stringify(run.summary.output, null, 2) }}</pre>
		</details>
		<h3>持久事件与动作回执</h3>
		<p class="muted">
			事件按序号从最新向前分页。动作仅记录类型及操作编号，不记录输入值。动作只有开始事件而无终结回执时，结果可能未知；不要据此自动重放。旧运行没有动作级日志时不会补造回执。
		</p>
		<p v-if="!events.length && !busy && !message">暂无事件。</p>
		<div class="record-preview-table">
			<table>
				<thead>
					<tr>
						<th>序号 / 尝试</th>
						<th>步骤 / 阶段</th>
						<th>动作</th>
						<th>状态 / 原因</th>
						<th>时间</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="e in events" :key="e.sequence">
						<td>#{{ e.sequence }} / {{ e.attempt }}</td>
						<td>{{ payload(e).step_id || '入口' }} / {{ payload(e).stage || '—' }}</td>
						<td>{{ payload(e).action ? `${payload(e).action.type} #${payload(e).action.operation_id}` : '—' }}</td>
						<td>{{ runStatus(payload(e).status) }} · {{ payload(e).code || '—' }}</td>
						<td>{{ e.created_at }}</td>
					</tr>
				</tbody>
			</table>
		</div>
		<button v-if="cursor" :disabled="busy" @click="load(true)">更早的诊断</button>
	</section>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { appApi, type FormalRun, type DataRow } from './api';
import { readError, parseJSON, runStatus } from './data-read';
const props = defineProps<{ run: FormalRun }>();
const events = ref<DataRow[]>([]),
	coverage = ref<DataRow>(),
	cursor = ref(''),
	busy = ref(false),
	message = ref('');
let epoch = 0;
const payload = (e: DataRow) => parseJSON<Record<string, any>>(e.event_json, {});
const written = computed(() => Object.values(props.run.summary.counts || {}).reduce<number>((a, b) => a + Number(b), 0));
const explanation = computed(
	() =>
		(
			({
				queued: '尚未执行，等待后台领取。',
				running: '正在执行。查看最后阶段与保存证据；可刷新状态。',
				succeeded: '本次配置的执行范围已完成。',
				limited: '因时间、页数、候选、详情或分页限制停止，属于有限覆盖。',
				partial: '执行或输出校验未完整通过，已有文档可用于排查。',
				failed: '运行失败，结束原因和事件可用于定位。',
				cancelled: '已取消后续执行；已发生的网站动作不能撤销。',
			}) as Record<string, string>
		)[props.run.status] || props.run.status
);
async function load(more = false) {
	if (busy.value) return;
	const token = epoch;
	busy.value = true;
	message.value = '';
	try {
		const [p, c] = await Promise.all([appApi.runDiagnostics(props.run.run_id, more ? cursor.value : ''), appApi.runCoverage(props.run.run_id)]);
		if (token !== epoch) return;
		events.value = more ? [...events.value, ...p.items] : p.items;
		cursor.value = p.next_cursor;
		coverage.value = c;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => props.run.run_id,
	() => {
		epoch++;
		events.value = [];
		coverage.value = undefined;
		cursor.value = '';
		busy.value = false;
		void load();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>
<style scoped>
.coverage-counts {
	display: flex;
	flex-wrap: wrap;
	gap: 20px;
	margin: 16px 0;
}
.coverage-counts strong {
	display: block;
	font-size: 24px;
}
pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
}
</style>

<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>运行 {{ id.slice(0, 8) }}</h1>
				<p v-if="run">
					{{ statusLabel(run.status) }} · {{ run.namespace === 'trial' ? '隔离试采 · 正式写入0' : '正式运行' }} · 输入已准备
					{{ run.preparing_cursor }}/{{ run.input_count }} · 有效输出 {{ run.output_count }}
				</p>
			</div>
			<ReplayStage v-if="run && !active" :run="run" :stages="stages" :items="items" :outputs="outputs" />
			<el-button :loading="busy" @click="refresh">刷新原运行</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" /><el-alert
			v-if="pollError"
			title="状态连接暂时中断，运行继续；可刷新原运行恢复事件，不会重复执行。"
			type="warning"
			:closable="false"
		/>
		<template v-if="run"
			><div class="co-toolbar">
				<el-button v-if="['running', 'preparing'].includes(run.status)" :disabled="busy" @click="control('pause')">暂停</el-button
				><el-button v-if="run.status === 'paused'" :disabled="busy" @click="control('resume')">恢复</el-button
				><el-button v-if="active" type="danger" :disabled="busy" @click="control('cancel')">取消</el-button
				><el-button
					v-else-if="run.manifest.definition.stages.length === 1"
					:disabled="busy || !counts.some((c) => c.status === 'failed')"
					@click="retryFailed"
					>仅失败输入补采</el-button
				><el-button :disabled="busy" @click="recover">查询上次控制操作</el-button>
			</div>
			<p v-if="run.retry_of">
				本次为有限补采；冻结任务版本 {{ run.task_version_id }}，范围 {{ run.retry_scope.join('、') }}。原运行：<router-link
					:to="'/app/co/runs/' + run.retry_of"
					>{{ run.retry_of }}</router-link
				>
			</p>
			<div class="co-toolbar">
				<el-tag v-for="c in counts" :key="c.status">{{ statusLabel(c.status) }} {{ c.count }}</el-tag>
			</div>
			<el-card
				><template #header>阶段进度 · 输入关闭后仍需等待工作项与分发完成</template>
				<el-table :data="stages">
					<el-table-column prop="stage_id" label="阶段" /><el-table-column label="状态"
						><template #default="{ row }"
							>{{ statusLabel(row.status) }}<small>{{ row.blocked_reason || row.close_reason }}</small></template
						></el-table-column
					>
					<el-table-column label="上游输入"
						><template #default="{ row }"
							><el-tag v-if="run.manifest.definition.stages.find((s) => s.id === row.stage_id)?.input_mode === 'union'">联合输入</el-tag>
							<el-tag v-if="['join', 'batch'].includes(run.manifest.definition.stages.find((s) => s.id === row.stage_id)?.input_mode || '')"
								>汇聚 · {{ row.gather_sealed ? '已收齐' : '等待来源' }}</el-tag
							>
							<p v-if="row.buffered_rows">缓存 {{ row.buffered_rows }} 行 · {{ JSON.stringify(row.gather_counts) }}</p>
							<p v-for="up in row.upstreams || []" :key="up.edge_id">{{ up.stage_id }} / {{ up.port }} · {{ statusLabel(up.status) }}</p>
							<small>{{ row.input_closed ? '输入已关闭' : '等待剩余来源及分发' }}</small></template
						></el-table-column
					><el-table-column prop="source_count" label="来源输入" width="90" /><el-table-column
						prop="input_count"
						label="工作项"
						width="80"
					/><el-table-column prop="merged_inputs" label="合并减少" width="90" /><el-table-column prop="output_count" label="输出" width="70" />
					<el-table-column label="输入策略"
						><template #default="{ row }">{{
							run.manifest.definition.stages.find((s) => s.id === row.stage_id)?.input_policy === 'merge_identical' ? '相同输入合并' : '全部独立执行'
						}}</template></el-table-column
					>
					<el-table-column prop="delivered" label="已派发" width="80" /><el-table-column
						prop="pending_dispatches"
						label="待派发"
						width="80"
					/><el-table-column prop="merged_dispatches" label="分发合并" width="90" /><el-table-column
						prop="filtered"
						label="过滤"
						width="70"
					/><el-table-column prop="dispatch_errors" label="分发异常" width="90" />
					<el-table-column label="工作项 / 保存决策"
						><template #default="{ row }"
							><code>{{ JSON.stringify(row.counters) }} / {{ JSON.stringify(row.save_decisions) }}</code></template
						></el-table-column
					>
				</el-table>
			</el-card>
			<el-tabs v-model="tab" @tab-change="loadTab"
				><el-tab-pane label="工作项与尝试" name="work-items"
					><el-table :data="items"
						><el-table-column label="阶段"
							><template #default="{ row }">{{ stages.find((s) => s.id === row.stage_run_id)?.stage_id }}</template></el-table-column
						><el-table-column label="输入"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.input_values) }}</code></template
							></el-table-column
						><el-table-column label="状态"
							><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column
						><el-table-column prop="attempt" label="尝试" width="80" /><el-table-column label="检查点"
							><template #default="{ row }"
								>第 {{ row.checkpoint.pages || 0 }} 页{{ row.checkpoint.done ? ' · 已提交结束标记' : '' }}</template
							></el-table-column
						><el-table-column label="错误"
							><template #default="{ row }">{{ row.error?.code }} {{ row.error?.message }}</template></el-table-column
						><el-table-column label="详情"
							><template #default="{ row }"
								><el-button link type="primary" @click="openItem(row.id)">输入与尝试</el-button></template
							></el-table-column
						></el-table
					></el-tab-pane
				>
				<el-tab-pane label="不可变输出" name="outputs"
					><el-table :data="outputs"
						><el-table-column prop="output_identity" label="输出身份" /><el-table-column prop="port" label="端口" width="110" /><el-table-column
							label="保存决策"
							><template #default="{ row }">{{ row.decision === 'passed' ? '仅传递' : row.decision }}</template></el-table-column
						><el-table-column label="本次原始值"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.values) }}</code><details v-if="row.diagnostic?.fields"><summary>字段证据与转换诊断</summary><pre class="co-json">{{ JSON.stringify(row.diagnostic.fields,null,2) }}</pre></details></template
							></el-table-column
						><el-table-column label="来源"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.source) }}</code></template
							></el-table-column
						></el-table
					></el-tab-pane
				>
				<el-tab-pane label="候选与诊断" name="observations"
					><el-table :data="observations"
						><el-table-column prop="port" label="端口" width="110" /><el-table-column label="保存决策"
							><template #default="{ row }">{{ row.decision === 'passed' ? '仅传递' : row.decision }}</template></el-table-column
						><el-table-column label="候选"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.values) }}</code></template
							></el-table-column
						><el-table-column label="诊断"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.diagnostic) }}</code></template
							></el-table-column
						><el-table-column label="记录"
							><template #default="{ row }"
								><router-link v-if="row.record_id" :to="'/app/co/data?record_id=' + row.record_id">来源与修订</router-link></template
							></el-table-column
						></el-table
					></el-tab-pane
				><el-tab-pane label="分发与映射诊断" name="dispatches"
					><el-table :data="dispatches">
						<el-table-column prop="edge_id" label="连接" /><el-table-column label="结果"
							><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column
						><el-table-column prop="expansion_ordinal" label="展开序号" width="90" />
						<el-table-column label="冻结下游输入 / 错误"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.input_snapshot) }}</code>
								<p>{{ row.error?.message || row.error?.code }}</p><details v-if="row.transforms"><summary>转换前后与诊断</summary><pre class="co-json">{{ JSON.stringify(row.transforms,null,2) }}</pre></details></template
							></el-table-column
						>
						<el-table-column label="下游项"
							><template #default="{ row }"
								><el-button v-if="row.child_item_id" link @click="openItem(row.child_item_id)">查看来源链</el-button></template
							></el-table-column
						>
					</el-table></el-tab-pane
				><el-tab-pane label="配对与批次诊断" name="gather-groups"
					><el-table :data="gatherGroups"
						><el-table-column label="阶段"
							><template #default="{ row }">{{
								stages.find((s) => s.id === row.stage_run_id)?.stage_id || row.stage_run_id
							}}</template></el-table-column
						><el-table-column prop="group_key" label="分组身份" show-overflow-tooltip /><el-table-column label="状态"
							><template #default="{ row }">{{
								row.status === 'skipped' ? '缺少来源，已跳过' : row.status === 'invalid' ? '汇聚未通过' : statusLabel(row.status)
							}}</template></el-table-column
						><el-table-column label="输入 / 诊断"
							><template #default="{ row }"
								><code>{{ JSON.stringify(row.values_json) }}</code>
								<p>{{ row.error?.message || row.error?.code }}</p></template
							></el-table-column
						><el-table-column label="工作项"
							><template #default="{ row }"
								><el-button v-if="row.child_item_id" link @click="openItem(row.child_item_id)">查看来源链</el-button></template
							></el-table-column
						></el-table
					></el-tab-pane
				></el-tabs
			><el-button v-if="cursors[tab]" @click="loadTab(true)">加载更多</el-button>
			<el-card
				><template #header>有序事件 · 已读取至 #{{ sequence }}</template>
				<ol class="co-events">
					<li v-for="e in events" :key="e.sequence">
						#{{ e.sequence }} {{ e.type }} <code>{{ JSON.stringify(e.data) }}</code>
					</li>
				</ol>
				<el-button v-if="sequence < latestSequence" @click="loadEvents">继续读取事件</el-button></el-card
			>
			<details>
				<summary>冻结任务、采集器与保存规则</summary>
				<pre>{{ JSON.stringify(run.manifest, null, 2) }}</pre>
			</details>
		</template>
		<el-drawer v-model="drawer" title="工作项输入与尝试" size="min(760px, 94vw)"
			><template v-if="selected"
				><p>{{ statusLabel(selected.item.status) }} · {{ selected.item.error?.message }}</p>
				<pre>{{ JSON.stringify(selected.input, null, 2) }}</pre>
				<h3>全部输入来源 · 相同输入共享此工作项及结果</h3>
				<el-table :data="selected.sources"
					><el-table-column prop="source_key" label="来源身份" /><el-table-column label="处理方式"
						><template #default="{ row }">{{ row.merged ? '合并到此工作项' : '创建此工作项' }}</template></el-table-column
					><el-table-column label="冻结来源"
						><template #default="{ row }"
							><code>{{ JSON.stringify(row.source) }}</code></template
						></el-table-column
					></el-table
				>
				<el-button v-if="selected.next_source_cursor" :disabled="busy" @click="moreSources">继续加载来源（每页100条）</el-button>
				<h3>来源链 · 向上游追溯</h3>
				<p v-if="selected.lineage_has_more">此处展示前100条上游关系；可从输入来源和上游工作项继续追溯，完整证据仍保留。</p>
				<p v-if="!selected.lineage?.length">此项来自入口快照。</p>
				<el-card
					v-for="link in selected.lineage"
					:key="[link.target_item_id, link.source_output_id, link.edge_id, link.element_key, link.depth].join(':')"
					><template #header>{{ link.depth }} · {{ link.source_stage }} → {{ link.edge_id }}</template>
					<p>映射 {{ link.mapping_hash }} · 元素 {{ link.element_key }}</p>
					<p>来源输入</p>
					<pre>{{ JSON.stringify(link.source_input, null, 2) }}</pre>
					<p>不可变输出</p>
					<pre>{{ JSON.stringify(link.output_values, null, 2) }}</pre>
					<el-button link @click="openItem(link.source_item_id)">查看上游工作项与尝试</el-button></el-card
				>
				<div v-if="selected.item.status === 'awaiting_confirmation'" class="co-toolbar">
					<el-button type="danger" @click="resolve('fail')">确认失败，停止此项</el-button
					><el-button @click="resolve('restart')">确认从入口重采</el-button>
				</div>
				<el-card v-for="a in selected.attempts" :key="a.id"
					><template #header>尝试 {{ a.number }} · {{ statusLabel(a.status) }}</template>
					<pre>{{ JSON.stringify(a.error, null, 2) }}</pre>
					<div v-for="command in a.command_refs" :key="command.command_id + command.status">
						<code>{{ command.command_id }} · {{ command.status }}</code
						><el-button link @click="receipt(a.id, command.command_id)">查询原回执</el-button>
					</div></el-card
				>
				<pre v-if="receiptValue">{{ JSON.stringify(receiptValue, null, 2) }}</pre>
			</template></el-drawer
		>
	</section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ElMessageBox } from 'element-plus';
import { call, describe } from './api';
import { taskApi, taskMutation, recoverTaskMutation, type Run, type WorkItem } from './tasks-api';
import { statusLabel } from './status';
import ReplayStage from './replay-stage.vue';
const route = useRoute(),
	router = useRouter(),
	id = String(route.params.id),
	run = ref<Run>(),
	counts = ref<{ status: string; count: number }[]>([]),
	items = ref<WorkItem[]>([]),
	outputs = ref<any[]>([]),
	observations = ref<any[]>([]),
	stages = ref<any[]>([]),
	dispatches = ref<any[]>([]),
	gatherGroups = ref<any[]>([]),
	tab = ref('work-items'),
	cursors = ref<Record<string, string>>({}),
	busy = ref(false),
	problem = ref(''),
	pollError = ref(false),
	drawer = ref(false),
	selected = ref<Awaited<ReturnType<typeof taskApi.item>>>(),
	receiptValue = ref<unknown>(),
	events = ref<{ sequence: number; type: string; data: unknown }[]>([]),
	sequence = ref(0),
	latestSequence = ref(0);
let alive = true,
	timer: ReturnType<typeof setTimeout> | undefined;
const active = computed(() => !!run.value && !['succeeded', 'partial', 'limited', 'failed', 'cancelled'].includes(run.value.status));
async function guarded(fn: () => Promise<void>) {
	busy.value = true;
	problem.value = '';
	try {
		await fn();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
async function state() {
	const d = await taskApi.run(id, !!run.value);
	if (!alive) return;
	run.value = { ...run.value, ...d.run };
	counts.value = d.counts;
	stages.value = d.stages;
}
async function loadTab(more: unknown = false) {
	const append = more === true;
	await guarded(async () => {
		const p = await taskApi.runPage<any>(id, tab.value, append ? cursors.value[tab.value] || '' : '');
		cursors.value[tab.value] = p.next_cursor || '';
		if (tab.value === 'work-items') items.value = append ? [...items.value, ...p.items] : p.items;
		else if (tab.value === 'outputs') outputs.value = append ? [...outputs.value, ...p.items] : p.items;
		else if (tab.value === 'dispatches') dispatches.value = append ? [...dispatches.value, ...p.items] : p.items;
		else if (tab.value === 'gather-groups') gatherGroups.value = append ? [...gatherGroups.value, ...p.items] : p.items;
		else observations.value = append ? [...observations.value, ...p.items] : p.items;
	});
}
async function loadEvents() {
	const p = await taskApi.events(id, sequence.value);
	if (!alive) return;
	events.value = [...events.value, ...p.items].slice(-300);
	sequence.value = p.next_sequence;
	latestSequence.value = p.latest_sequence;
}
async function refresh() {
	await guarded(async () => {
		await state();
		await loadEvents();
	});
	await loadTab();
}
async function confirm(text: string) {
	try {
		await ElMessageBox.confirm(text, '确认操作', { type: 'warning', confirmButtonText: '确认', cancelButtonText: '取消', customClass: 'co-confirm' });
		return true;
	} catch {
		return false;
	}
}
async function control(action: string) {
	if (action === 'cancel' && !(await confirm('取消将停止后续处理；已提交记录和来源保留，已发出的网页动作不能撤销。'))) return;
	await guarded(async () => {
		await taskMutation('controlRun', id, '/runs/' + id + '/control', { action, expected_control_epoch: run.value!.control_epoch });
		await state();
		await loadEvents();
	});
}
async function retryFailed() {
	if (!(await confirm('仅将原运行状态为failed的输入作为新运行，复用原版本并再次访问/保存；不会重跑成功项或未知动作项。'))) return;
	await guarded(async () => {
		const r = await taskMutation('retryFailed', id, '/runs/' + id + '/retry-failed', { confirmed: true });
		await router.push('/app/co/runs/' + r.id);
	});
}
async function recover() {
	await guarded(async () => {
		for (const op of ['controlRun', 'retryFailed', 'resolveItem']) {
			const r = await recoverTaskMutation(op, id);
			if (r && op === 'retryFailed') {
				await router.push('/app/co/runs/' + r.id);
				return;
			}
		}
		await state();
		await loadEvents();
	});
}
async function openItem(item: string) {
	await guarded(async () => {
		selected.value = await taskApi.item(id, item);
		receiptValue.value = undefined;
		drawer.value = true;
	});
}
async function moreSources() {
	if (!selected.value?.next_source_cursor) return;
	await guarded(async () => {
		const current = selected.value!;
		const page = await taskApi.item(id, current.item.id, current.next_source_cursor);
		if (selected.value?.item.id === current.item.id) selected.value = { ...page, sources: [...current.sources, ...page.sources] };
	});
}
async function resolve(action: string) {
	if (
		!selected.value ||
		!(await confirm(
			action === 'restart'
				? '此操作由你确认从入口重采，可能再次执行原网页动作；已保存输出身份保持不变，内容变化会报冲突。'
				: '确认将此项标记失败，其他输入继续。'
		))
	)
		return;
	await guarded(async () => {
		await taskMutation('resolveItem', id, '/runs/' + id + '/work-items/' + selected.value!.item.id + '/resolve', { action, confirmed: true });
		await state();
		selected.value = await taskApi.item(id, selected.value!.item.id);
	});
}
async function receipt(attempt: string, command: string) {
	await guarded(async () => {
		receiptValue.value = await call('/runs/' + id + '/work-items/' + selected.value!.item.id + '/attempts/' + attempt + '/commands/' + command);
	});
}
async function poll() {
	try {
		if (!document.hidden && !busy.value) {
			const previous = sequence.value;
			await state();
			await loadEvents();
			if (sequence.value !== previous && tab.value === 'work-items' && !drawer.value) {
				const p = await taskApi.runPage<WorkItem>(id, 'work-items');
				if (alive) {
					items.value = p.items;
					cursors.value['work-items'] = p.next_cursor || '';
				}
			}
			pollError.value = false;
		}
	} catch {
		pollError.value = true;
	} finally {
		if (alive && active.value) timer = setTimeout(poll, pollError.value ? 8000 : 2500);
	}
}
onMounted(async () => {
	await refresh();
	if (route.query.item_id) await openItem(String(route.query.item_id));
	if (alive && active.value) timer = setTimeout(poll, 2500);
});
onBeforeUnmount(() => {
	alive = false;
	if (timer) clearTimeout(timer);
});
</script>
<style src="./style.css"></style>

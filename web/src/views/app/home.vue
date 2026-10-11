<template>
	<div class="app-page home-workspace" :aria-busy="loading">
		<header class="home-heading">
			<div>
				<h1>{{ isEmpty ? '把网页变成你的数据' : '采集工作台' }}</h1>
				<p>
					{{
						isEmpty
							? '从一个网址开始，采集并保存你的第一批数据。'
							: needsFirstResult
								? '继续配置方案，试采后保存第一批数据。'
								: '继续你的采集，查看结果与需要处理的事项。'
					}}
				</p>
			</div>
			<div class="home-heading-actions">
				<button :disabled="loading" @click="reloadHome">{{ loading ? '读取中…' : '刷新' }}</button
				><button v-if="!isEmpty" class="primary-action" @click="openStarter">新建采集</button>
			</div>
		</header>
		<div class="home-meta">
			<span v-if="home">更新于 {{ dateText(home.as_of) }}</span
			><span v-else>{{ loading ? '正在读取工作台…' : '工作台暂不可读' }}</span>
			<router-link
				class="browser-status"
				:class="{ unavailable: capError || (capabilities && !capabilities.editor_service_connected) }"
				to="/app/settings?tab=environment"
				>{{
					capLoading ? '浏览器：检查中' : capError ? '浏览器：状态未知' : capabilities?.editor_service_connected ? '浏览器：已连接' : '浏览器：未连接'
				}}</router-link
			>
		</div>
		<p v-if="homeError" class="home-notice app-error" role="alert">
			概览读取失败：{{ homeError }}。{{ home ? '下面保留上次读取结果。' : '请重试，或进入方案与数据页面。'
			}}<router-link to="/app/collectors">采集方案</router-link> · <router-link to="/app/data">查看数据</router-link>
		</p>
		<p v-if="!capLoading && (capError || (capabilities && !capabilities.editor_service_connected))" class="home-notice" role="status">
			{{ capError ? '暂时无法确认浏览器连接状态。' : '浏览器编辑服务未连接，网页点选暂不可用。' }}已有数据和运行记录仍可查看，JSON
			入口可独立使用。<router-link to="/app/settings?tab=environment">检查连接</router-link>
		</p>
		<section v-if="isEmpty" class="app-card first-collection">
			<h2>开始一个采集方案</h2>
			<CollectorStarter input-id="entry-url" />
			<ol class="getting-started" aria-label="首次采集流程">
				<li><strong>1 · 打开目标</strong><span>输入网页或接口网址</span></li>
				<li><strong>2 · 选择内容</strong><span>配置要采集的记录与字段</span></li>
				<li><strong>3 · 试采保存</strong><span>核对结果，发布并运行</span></li>
			</ol>
		</section>
		<template v-else-if="home">
			<nav class="home-summary" aria-label="采集状态摘要">
				<router-link to="/app/runs"
					><strong>{{ home.counts.active_runs }}</strong
					><span>排队 / 运行中</span></router-link
				>
				<router-link to="/app/collectors/health?attention=1"
					><strong>{{ home.counts.attention }}</strong
					><span>待处理方案</span></router-link
				>
				<router-link :to="activityLink"
					><strong>{{ home.activity.created }}</strong
					><span>最近 24 小时新增记录</span></router-link
				>
			</nav>
			<div class="home-panels">
				<div class="home-panel-column">
					<section class="app-card collectors-panel">
						<div class="pane-heading">
							<h2>{{ needsFirstResult ? '继续完成第一个方案' : '我的采集方案' }}</h2>
							<router-link to="/app/collectors">全部方案</router-link>
						</div>
						<p class="section-caption">
							{{ needsFirstResult ? '完成字段与输出配置，试采通过后发布并运行。' : '最近更新或运行的方案，最多展示 5 个。' }}
						</p>
						<ul v-if="collectors.length" class="home-list">
							<li v-for="item in collectors" :key="item.collector_id" class="collector-row">
								<div class="row-content">
									<router-link class="row-title" :to="`/app/collectors/${item.collector_id}`">{{ item.name }}</router-link>
									<p>
										{{ item.entry_type === 'json' ? 'JSON 接口' : '网页采集' }}<template v-if="item.table_name"> · {{ item.table_name }}</template>
									</p>
									<p v-if="item.latest_effective">最近采集 {{ dateText(item.latest_effective.finished_at) }}</p>
								</div>
								<div class="row-next">
									<span class="home-tag" :class="{ warning: item.issues.length > 0 }">{{ collectorStatus(item) }}</span
									><router-link class="home-action-link" :to="collectorAction(item).to">{{ collectorAction(item).label }}</router-link>
								</div>
							</li>
						</ul>
						<p v-else class="home-empty">暂无未归档方案。可以新建采集，已保存的数据仍可查看。</p>
					</section>
					<section class="app-card tables-panel">
						<div class="pane-heading">
							<h2>最近数据表</h2>
							<router-link to="/app/data">全部数据</router-link>
						</div>
						<ul v-if="home.recent_tables.length" class="home-list">
							<li v-for="table in home.recent_tables.slice(0, 3)" :key="table.table_id" class="table-row">
								<div class="row-content">
									<router-link class="row-title" :to="`/app/data/${table.table_id}`">{{ table.name }}</router-link>
									<p>
										{{ table.record_count }} 条记录 · {{ table.last_observed_at ? `最近采集 ${dateText(table.last_observed_at)}` : '尚未采集数据' }}
									</p>
								</div>
								<router-link class="home-action-link" :to="`/app/data/${table.table_id}`">查看数据</router-link>
							</li>
						</ul>
						<p v-else class="home-empty">尚无数据表。在编辑器中确认输出后，试采、发布并运行即可保存数据。</p>
					</section>
				</div>
				<div class="home-panel-column">
					<section class="app-card attention-panel">
						<div class="pane-heading">
							<h2>需要处理</h2>
							<router-link to="/app/collectors/health?attention=1">查看全部</router-link>
						</div>
						<ul v-if="attention.length" class="home-list">
							<li v-for="item in attention" :key="item.collector_id" class="attention-row">
								<strong class="row-title">{{ item.name }}</strong>
								<p class="attention-description">{{ attentionText(item) }}</p>
								<router-link :to="attentionAction(item).to">{{ attentionAction(item).label }}</router-link>
							</li>
						</ul>
						<p v-else class="home-empty">当前未归档方案暂无需处理项。</p>
						<router-link class="section-footer" to="/app/issues">全部异常与恢复确认 →</router-link>
					</section>
					<section class="app-card activity-panel">
						<div class="pane-heading">
							<h2>运行动态</h2>
							<router-link to="/app/runs">运行中心</router-link>
						</div>
						<ul v-if="activity.length" class="home-list">
							<li v-for="item in activity" :key="item.key" class="activity-row">
								<span class="activity-state" :class="item.tone">{{ item.status }}</span
								><router-link class="row-title" :to="item.to">{{ item.name }}</router-link>
								<p>{{ item.description }}</p>
							</li>
						</ul>
						<p v-else class="home-empty">暂无运行动态。发布方案后可手动运行或设置定时采集。</p>
						<router-link v-if="home.counts.active_runs !== '0'" class="section-footer" to="/app/runs"
							>查看全部 {{ home.counts.active_runs }} 个排队或运行中的任务 →</router-link
						>
					</section>
				</div>
			</div>
		</template>
		<dialog ref="starterDialog" class="home-starter-dialog" aria-labelledby="new-collector-title" @cancel="cancelStarter">
			<div class="pane-heading">
				<h2 id="new-collector-title">新建采集方案</h2>
				<button :disabled="creating" @click="closeStarter">取消</button>
			</div>
			<CollectorStarter input-id="new-entry-url" @busy="creating = $event" />
		</dialog>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { appApi, type Capabilities, type HomeState, type CollectorHealth, type OverviewRun } from './api';
import CollectorStarter from './collector-starter.vue';
import { dateText, outcomeText, issueText } from './health-read';
import { readError, runStatus } from './data-read';
const home = ref<HomeState>(),
	loading = ref(false),
	homeError = ref('');
const capabilities = ref<Capabilities>(),
	capLoading = ref(false),
	capError = ref('');
const starterDialog = ref<HTMLDialogElement>(),
	creating = ref(false);
let epoch = 0,
	capEpoch = 0;
const isEmpty = computed(() => home.value?.status === 'empty');
const needsFirstResult = computed(() => home.value?.counts.records === '0' && home.value.pending_drafts.some((item) => !item.published_version_id));
const activityLink = computed(() => ({ path: '/app/runs', query: { committed: '1', since: home.value?.window_start, until: home.value?.as_of } }));
function uniqueCollectors(items: CollectorHealth[]) {
	return [...new Map(items.map((item) => [item.collector_id, item])).values()].filter((item) => !item.archived);
}
const allCollectors = computed(() =>
	home.value
		? uniqueCollectors([...home.value.next_schedules, ...home.value.attention, ...home.value.pending_drafts, ...home.value.recent_collectors])
		: []
);
const collectors = computed(() => {
	const items = uniqueCollectors([...(home.value?.pending_drafts || []), ...(home.value?.recent_collectors || [])]);
	const updated = (item: CollectorHealth) => Math.max(Date.parse(item.updated_at) || 0, Date.parse(item.latest_run?.created_at || '') || 0);
	return items
		.sort((a, b) =>
			needsFirstResult.value && !a.published_version_id !== !b.published_version_id ? (!a.published_version_id ? -1 : 1) : updated(b) - updated(a)
		)
		.slice(0, 5);
});
const attention = computed(() => uniqueCollectors(home.value?.attention || []).slice(0, 3));
function collectorStatus(item: CollectorHealth) {
	if (item.active_runs) return '排队 / 采集中';
	if (item.issues.length) return '需处理';
	if (!item.published_version_id) return '待配置';
	if (item.pending_draft) return '有未发布修改';
	return item.outcome === 'healthy' ? '已完成' : outcomeText(item.outcome);
}
function collectorAction(item: CollectorHealth) {
	if (item.pending_draft) return { label: '继续配置', to: `/app/collectors/${item.collector_id}` };
	const table = item.latest_effective?.table_id;
	return table ? { label: '查看数据', to: `/app/data/${table}` } : { label: '打开方案', to: `/app/collectors/${item.collector_id}` };
}
function primaryIssue(item: CollectorHealth) {
	return item.issues.find((issue) => issue.severity === 'error') || item.issues[0];
}
function attentionText(item: CollectorHealth) {
	const issue = primaryIssue(item);
	if (!issue) return outcomeText(item.outcome);
	if (issue.status === 'ready') return '修复已验证，等待确认恢复';
	return issue.kind === 'quality_issue' ? '采集质量需核对' : issueText(issue.kind);
}
function attentionAction(item: CollectorHealth) {
	const issue = primaryIssue(item);
	if (issue?.issue_id) return { label: issue.status === 'ready' ? '确认恢复' : '查看问题', to: `/app/issues/${issue.issue_id}` };
	if (issue?.kind === 'schedule_blocked') return { label: '检查计划', to: `/app/collectors/${item.collector_id}/schedule` };
	return issue?.run_id ? { label: '查看运行', to: `/app/runs/${issue.run_id}` } : { label: '检查方案', to: `/app/collectors/${item.collector_id}` };
}
const activity = computed(() => {
	if (!home.value) return [];
	const names = new Map(allCollectors.value.map((item) => [item.collector_id, item.name]));
	const runs = [
		...new Map(
			[...allCollectors.value.flatMap((item) => (item.latest_run ? [item.latest_run] : [])), ...home.value.recent_runs].map((run) => [
				run.run_id,
				run,
			])
		).values(),
	];
	const active = (run: OverviewRun) => ['queued', 'running'].includes(run.status);
	const runItems = (items: OverviewRun[]) =>
		items.map((run) => ({
			key: `run-${run.run_id}`,
			status: runStatus(run.status),
			tone: active(run) ? 'running' : ['failed', 'partial', 'limited'].includes(run.status) ? 'warning' : '',
			name: names.get(run.collector_id) || `方案 ${run.collector_id}`,
			to: `/app/runs/${run.run_id}`,
			description: `v${run.version_number} · ${dateText(run.finished_at || run.created_at)}`,
		}));
	const running = runs.filter(active).sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
	const scheduled = home.value.next_schedules
		.filter((item) => item.schedule?.enabled && item.schedule.next_at)
		.slice(0, 2)
		.map((item) => ({
			key: `schedule-${item.collector_id}`,
			status: '即将采集',
			tone: '',
			name: item.name,
			to: `/app/collectors/${item.collector_id}/schedule`,
			description: `预计 ${dateText(item.schedule!.next_at)}`,
		}));
	const recent = home.value.recent_runs.filter((run) => !active(run));
	return [...runItems(running.slice(0, 2)), ...scheduled, ...runItems(recent.slice(0, 2))].slice(0, 4);
});
function openStarter() {
	starterDialog.value?.showModal();
	starterDialog.value?.querySelector<HTMLInputElement>('input')?.focus();
}
function closeStarter() {
	if (!creating.value) starterDialog.value?.close();
}
function cancelStarter(event: Event) {
	if (creating.value) event.preventDefault();
}
async function reloadHome() {
	const token = ++epoch;
	loading.value = true;
	homeError.value = '';
	try {
		const result = await appApi.home();
		if (token === epoch) home.value = result;
	} catch (cause) {
		if (token === epoch) homeError.value = readError(cause);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function reloadCapabilities() {
	const token = ++capEpoch;
	capLoading.value = true;
	capError.value = '';
	try {
		const result = await appApi.capabilities();
		if (token === capEpoch) capabilities.value = result;
	} catch (cause) {
		if (token === capEpoch) capError.value = readError(cause);
	} finally {
		if (token === capEpoch) capLoading.value = false;
	}
}
onMounted(() => {
	void reloadHome();
	void reloadCapabilities();
});
onBeforeUnmount(() => {
	epoch++;
	capEpoch++;
});
</script>
<style scoped src="./home.css"></style>

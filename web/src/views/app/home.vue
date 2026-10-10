<template>
	<div class="app-page home-workspace">
		<div class="eyebrow">SCRAPIO / YOUR DATA WORKSPACE</div>
		<div class="pane-heading">
			<h1>{{ home?.counts.records !== '0' && home ? '你的采集工作台' : '把网页变成你的数据' }}</h1>
			<button :disabled="loading" @click="reloadHome">刷新概览</button>
		</div>
		<p class="lead">
			{{ home?.status === 'empty' ? '从目标网址开始，配置、试采并保存第一批数据。' : '找到最近的数据、需要核对的结果和下一次采集。' }}
		</p>
		<p v-if="loading" role="status">正在读取新产品数据…</p>
		<p v-if="homeError" class="app-error" role="alert">
			{{ homeError }}。概览不可用不代表没有数据或没有问题。<button @click="reloadHome">重新读取概览</button>
		</p>
		<p v-if="home" class="muted">截至 {{ dateText(home.as_of) }} · 只统计当前用户的新产品数据 · 尚未配置质量基线，不显示推测趋势</p>
		<section class="app-card starter">
			<h2>开始一个采集方案</h2>
			<label for="entry-url">目标网址</label>
			<div class="url-row">
				<input id="entry-url" v-model="entryUrl" type="url" placeholder="https://example.com/list" @keyup.enter="start" /><select
					v-model="entryType"
					aria-label="入口类型"
				>
					<option value="web">网页</option>
					<option value="json">JSON 接口</option></select
				><button :disabled="creating || !entryUrl" @click="start">{{ creating ? '创建中…' : '开始采集' }}</button>
			</div>
			<p class="muted">创建草稿后配置动作、字段与输出，试采通过后发布并运行。JSON 入口不依赖浏览器编辑服务。</p>
			<p v-if="createError" class="app-error" role="status">{{ createError }}</p>
		</section>
		<template v-if="home && home.status !== 'empty'">
			<section class="app-card">
				<div class="pane-heading">
					<h2>
						需要处理或核对 <small>{{ home.counts.attention }} 个方案</small>
					</h2>
					<router-link to="/app/collectors/health?attention=1">查看全部</router-link>
				</div>
				<HealthCard v-for="h in home.attention" :key="h.collector_id" :health="h" />
				<p v-if="!home.attention.length" class="status-good">当前没有需处理或核对的运行结果。</p>
				<p class="muted">依据最近完成结果和调度阻断，有限覆盖单独提示；取消、零新增和重叠跳过不自动报警。</p>
			</section>
			<section class="app-card">
				<div class="pane-heading">
					<h2>最近 24 小时的有效采集</h2>
					<router-link :to="activityLink">查看这段时间的提交运行</router-link>
				</div>
				<div class="activity-metrics">
					<router-link :to="activityLink"
						>新增记录<strong>{{ home.activity.created }}</strong></router-link
					><router-link :to="activityLink"
						>更新决策<strong>{{ home.activity.updated }}</strong></router-link
					><router-link :to="activityLink"
						>未变决策<strong>{{ home.activity.unchanged }}</strong></router-link
					>
				</div>
				<p class="muted">
					{{ home.activity.effective_runs }} 次已提交运行，范围 {{ dateText(home.window_start) }} 至
					{{ dateText(home.as_of) }}。统计正式写入决策，同一记录多次更新会多次计数；全部未变也是有效采集。
				</p>
			</section>
			<section class="app-card">
				<div class="pane-heading">
					<h2>最近的数据表</h2>
					<router-link to="/app/data">全部数据表 · {{ home.counts.tables }}</router-link>
				</div>
				<article v-for="t in home.recent_tables" :key="t.table_id" class="home-table-row">
					<div>
						<router-link :to="`/app/data/${t.table_id}`"
							><strong>{{ t.name }}</strong></router-link
						>
						<p>{{ t.record_count }} 条当前记录</p>
					</div>
					<div>
						<p>最近有效观察 {{ dateText(t.last_observed_at) }}</p>
						<p class="muted">最近实际变化 {{ dateText(t.last_changed_at) }}</p>
						<router-link v-if="t.latest_run_id" :to="`/app/runs/${t.latest_run_id}`">查看最近提交证据</router-link>
					</div>
				</article>
				<p v-if="!home.recent_tables.length">还没有数据表，继续配置草稿并确认输出。</p>
				<p v-else-if="home.counts.records === '0'" class="status-warn">数据表已建立，但尚无正式记录。继续试采、发布并运行；试采不会写入正式数据。</p>
			</section>
			<div class="app-grid">
				<section class="app-card">
					<div class="pane-heading">
						<h2>最近方案</h2>
						<router-link to="/app/collectors/health">查看健康</router-link>
					</div>
					<HealthCard v-for="h in home.recent_collectors" :key="h.collector_id" :health="h" />
					<p v-if="!home.recent_collectors.length">没有未归档方案，已保存的数据仍可查看。</p>
				</section>
				<section class="app-card">
					<div class="pane-heading">
						<h2>接下来的采集</h2>
						<router-link to="/app/collectors">全部方案</router-link>
					</div>
					<article v-for="h in home.next_schedules" :key="h.collector_id" class="home-table-row">
						<div>
							<router-link :to="`/app/collectors/${h.collector_id}/schedule`"
								><strong>{{ h.name }}</strong></router-link
							>
							<p>{{ dateText(h.schedule?.next_at) }}</p>
							<p class="muted">计划时区 {{ h.schedule?.timezone }} · 固定发布 v{{ h.schedule?.version_number }}</p>
						</div>
					</article>
					<p v-if="!home.next_schedules.length">暂无启用的定时计划，可在方案中设置运行计划。</p>
					<p class="muted">时间按当前设备时区显示，是预计触发时点；排队、重叠和容量可能影响执行。</p>
				</section>
			</div>
			<section class="app-card" v-if="home.pending_drafts.length">
				<div class="pane-heading">
					<h2>继续草稿 · {{ home.counts.pending_drafts }}</h2>
					<router-link to="/app/collectors">全部方案</router-link>
				</div>
				<div v-for="h in home.pending_drafts" :key="h.collector_id" class="home-table-row">
					<router-link :to="`/app/collectors/${h.collector_id}`"
						>{{ h.name }} · {{ h.published_version_id ? '有未发布修改' : '尚未发布' }}</router-link
					><small>{{ dateText(h.updated_at) }}</small>
				</div>
			</section>
			<section class="app-card">
				<div class="pane-heading">
					<h2>最近运行</h2>
					<router-link to="/app/runs">运行中心 · {{ home.counts.active_runs }} 个活动运行</router-link>
				</div>
				<div v-for="r in home.recent_runs" :key="r.run_id" class="home-table-row">
					<router-link :to="`/app/runs/${r.run_id}`">{{ runStatus(r.status) }} · 方案 {{ r.collector_id }} · v{{ r.version_number }}</router-link
					><span>{{ r.committed ? '已提交正式数据' : '未提交本批数据' }} · {{ dateText(r.created_at) }}</span>
				</div>
				<p v-if="!home.recent_runs.length">还没有正式运行。发布草稿后确认范围并执行。</p>
			</section>
		</template>
		<section class="app-card capability-card">
			<div class="pane-heading">
				<h2>浏览器编辑能力</h2>
				<button :disabled="capLoading" @click="reloadCapabilities">重新检查能力</button>
			</div>
			<p v-if="capLoading" role="status">正在检查编辑服务，不影响数据概览读取…</p>
			<p v-if="capError" class="app-error">{{ capError }}。数据和运行入口仍可使用。</p>
			<template v-if="capabilities"
				><p :class="capabilities.editor_service_connected ? 'status-good' : 'status-wait'">
					{{ capabilities.editor_service_connected ? '编辑服务已连接' : '编辑服务未连接' }}
				</p>
				<p class="muted">{{ capabilities.reason }}</p></template
			>
			<div class="editor-actions">
				<router-link to="/app/data">查看数据</router-link><router-link to="/app/runs">运行中心</router-link
				><router-link to="/app/collectors">继续方案</router-link>
			</div>
		</section>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { useRouter } from 'vue-router';
import { appApi, type Capabilities, type HomeState } from './api';
import HealthCard from './health-card.vue';
import { dateText } from './health-read';
import { readError, runStatus } from './data-read';
const router = useRouter(),
	entryUrl = ref(''),
	entryType = ref<'web' | 'json'>('web'),
	creating = ref(false),
	createError = ref(''),
	home = ref<HomeState>(),
	loading = ref(false),
	homeError = ref(''),
	capabilities = ref<Capabilities>(),
	capLoading = ref(false),
	capError = ref('');
let epoch = 0,
	capEpoch = 0,
	disposed = false;
const activityLink = computed(() => ({ path: '/app/runs', query: { committed: '1', since: home.value?.window_start, until: home.value?.as_of } }));
async function reloadHome() {
	const token = ++epoch;
	loading.value = true;
	home.value = undefined;
	homeError.value = '';
	try {
		const h = await appApi.home();
		if (token === epoch) home.value = h;
	} catch (e) {
		if (token === epoch) homeError.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function reloadCapabilities() {
	const token = ++capEpoch;
	capLoading.value = true;
	capabilities.value = undefined;
	capError.value = '';
	try {
		const c = await appApi.capabilities();
		if (token === capEpoch) capabilities.value = c;
	} catch (e) {
		if (token === capEpoch) capError.value = readError(e);
	} finally {
		if (token === capEpoch) capLoading.value = false;
	}
}
async function start() {
	if (!entryUrl.value || creating.value) return;
	creating.value = true;
	createError.value = '';
	try {
		const c = await appApi.createCollector({ entry_url: entryUrl.value, entry_type: entryType.value });
		if (!disposed) await router.push(`/app/collectors/${c.id}`);
	} catch (e) {
		if (!disposed) createError.value = readError(e);
	} finally {
		if (!disposed) creating.value = false;
	}
}
onMounted(() => {
	void reloadHome();
	void reloadCapabilities();
});
onBeforeUnmount(() => {
	disposed = true;
	epoch++;
	capEpoch++;
});
</script>
<style scoped>
.home-workspace .pane-heading {
	gap: 16px;
	flex-wrap: wrap;
}
.home-workspace a {
	color: #4567c4;
}
.home-workspace h2 small {
	font-size: 13px;
	font-weight: 400;
	color: #718097;
}
.activity-metrics {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 16px;
	margin: 20px 0;
}
.activity-metrics a {
	background: #f5f7fc;
	padding: 18px;
	border-radius: 10px;
	color: #596981;
	text-decoration: none;
}
.activity-metrics strong {
	display: block;
	font-size: 30px;
	color: #26334b;
	margin-top: 8px;
}
.home-table-row {
	display: flex;
	justify-content: space-between;
	align-items: center;
	gap: 18px;
	padding: 16px 0;
	border-bottom: 1px solid #edf0f5;
	flex-wrap: wrap;
}
.home-table-row:last-child {
	border: 0;
}
.home-table-row p,
.home-table-row span,
.home-table-row small {
	font-size: 13px;
	color: #718097;
}
.capability-card {
	margin-top: 28px;
}
.activity-metrics a:focus-visible {
	outline: 2px solid #4567c4;
}
@media (max-width: 650px) {
	.activity-metrics {
		grid-template-columns: 1fr;
	}
	.url-row {
		flex-wrap: wrap;
	}
}
</style>

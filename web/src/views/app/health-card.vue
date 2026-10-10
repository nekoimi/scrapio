<template>
	<article class="health-card" :data-outcome="health.outcome">
		<div class="pane-heading">
			<strong>{{ health.name }}</strong
			><span>{{ outcomeText(health.outcome) }}</span>
		</div>
		<p v-if="health.active_runs">另有 {{ health.active_runs }} 个排队或执行中的运行，尚未改变最近完成结果。</p>
		<ul v-if="health.issues.length">
			<li v-for="(issue, i) in health.issues" :key="i" :class="issue.severity === 'error' ? 'status-error' : 'status-warn'">
				<router-link v-if="issue.issue_id" :to="`/app/issues/${issue.issue_id}`">{{ issue.status==='ready'?'待确认恢复':'质量异常待办' }}</router-link>
				<router-link v-else-if="issue.run_id" :to="`/app/runs/${issue.run_id}`">{{ issueText(issue.kind) }}</router-link>
				<router-link v-else :to="`/app/collectors/${health.collector_id}/schedule`">{{ issueText(issue.kind) }}</router-link> ·
				{{ issue.kind === 'schedule_blocked' ? decisionText(issue.code) : issue.code || '请查看运行证据' }}
			</li>
		</ul>
		<p v-if="health.latest_effective">
			最近有效采集 {{ dateText(health.latest_effective.finished_at) }} ·
			<router-link :to="`/app/runs/${health.latest_effective.run_id}`">v{{ health.latest_effective.version_number }} 的实际提交</router-link>
		</p>
		<p v-else class="muted">尚无正式提交数据；试采结果不计入有效采集。</p>
		<p v-if="health.archived" class="muted">方案已归档，不再触发新的定时运行。</p>
		<p v-else-if="health.schedule?.enabled">
			预计下次触发 {{ dateText(health.schedule.next_at) }} · 计划时区 {{ health.schedule.timezone }} · 固定 v{{ health.schedule.version_number }}
		</p>
		<p v-else class="muted">{{ health.schedule ? '定时计划已暂停或停用' : '尚未配置定时计划' }}</p>
		<p v-if="health.pending_draft" class="muted">
			{{ health.published_version_id ? '有修改后的草稿；运行仍使用发布版本' : '尚未发布，继续配置、试采并发布' }}
		</p>
		<div class="editor-actions">
			<router-link :to="`/app/collectors/${health.collector_id}`">{{ health.pending_draft ? '继续配置' : '打开方案' }}</router-link>
			<router-link
				v-if="health.latest_effective?.table_id || health.table_id"
				:to="`/app/data/${health.latest_effective?.table_id || health.table_id}`"
				>查看数据</router-link
			>
			<router-link v-if="health.latest_run" :to="`/app/runs/${health.latest_run.run_id}`">最新运行</router-link>
			<router-link :to="`/app/collectors/${health.collector_id}/schedule`">运行计划</router-link>
		</div>
		<template v-if="detailed">
			<p v-if="health.latest_terminal">
				最近完成：{{ runStatus(health.latest_terminal.status) }} · v{{ health.latest_terminal.version_number }} ·
				{{ dateText(health.latest_terminal.finished_at) }} · {{ health.latest_terminal.stop_reason }}
			</p>
			<p v-if="health.latest_terminal">
				{{ health.latest_terminal.committed ? '已提交' : '未提交本批正式数据' }} · 新增 {{ health.latest_terminal.counts.created || 0 }} / 更新
				{{ health.latest_terminal.counts.updated || 0 }} / 未变 {{ health.latest_terminal.counts.unchanged || 0 }}
			</p>
			<p v-if="health.schedule">最近调度决定：{{ decisionText(health.schedule.last_decision) }} · {{ dateText(health.schedule.decision_at) }}</p>
			<p v-if="health.table_id">
				当前发布目标表：<router-link :to="`/app/data/${health.table_id}`">{{ health.table_name }}</router-link
				>。上方“查看数据”优先进入最近有效运行实际写入的表。
			</p>
			<p class="muted">
				截至
				{{
					dateText(health.as_of)
				}}。基线状态 {{ health.baseline_status }}；质量待办按正式证据保留，完整成功不会自动关闭待确认问题。零新增不算异常，取消不算采集故障。
			</p>
		</template>
	</article>
</template>
<script setup lang="ts">
import type { CollectorHealth } from './api';
import { dateText, outcomeText, issueText, decisionText } from './health-read';
import { runStatus } from './data-read';
defineProps<{ health: CollectorHealth; detailed?: boolean }>();
</script>
<style scoped>
.health-card {
	padding: 16px 0;
	border-bottom: 1px solid #e5e9f0;
}
.health-card:last-child {
	border-bottom: 0;
}
.health-card p {
	line-height: 1.6;
	font-size: 13px;
}
.health-card .pane-heading {
	gap: 14px;
	flex-wrap: wrap;
}
.health-card .pane-heading span {
	color: #697890;
	font-size: 13px;
}
.health-card[data-outcome='needs_attention'] .pane-heading span {
	color: #a34b38;
}
.health-card[data-outcome='limited'] .pane-heading span {
	color: #9b701b;
}
ul {
	padding-left: 18px;
}
li {
	margin: 8px 0;
}
a {
	color: #4567c4;
}
</style>

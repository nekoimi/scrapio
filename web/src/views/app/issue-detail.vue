<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / ISSUE</div>
		<h1>异常证据与恢复确认</h1>
		<button :disabled="busy" @click="load">刷新问题证据</button>
		<p v-if="error" class="app-error" role="status">{{ error }}</p>
		<section v-if="issue" class="app-card">
			<h2>{{ issue.message }}</h2>
			<p>{{ issue.code }} · {{ issue.category }} · {{ issue.status }} · 出现 {{ issue.occurrences }} 次</p>
			<p>revision {{ issue.revision }} · 后续完整正常运行 {{ issue.recovery_streak }} 次</p>
			<div class="editor-actions">
				<router-link :to="`/app/collectors/${issue.collector_id}`">方案、策略与修复回归</router-link
				><router-link v-if="issue.last_run_id" :to="`/app/runs/${issue.last_run_id}`">实际异常运行与修复入口</router-link
				><router-link v-if="issue.recovery_run_id" :to="`/app/runs/${issue.recovery_run_id}`">候选恢复运行</router-link
				><router-link :to="`/app/collectors/${issue.collector_id}/schedule`">运行计划</router-link>
			</div>
			<p>
				有效 {{ issue.evidence.valid_records }} / 无效 {{ issue.evidence.invalid_records }} · 页面 {{ issue.evidence.pages }} ·
				{{ issue.evidence.reason }}
			</p>
			<p>
				失败步骤 {{ issue.evidence.failed_step_id }} / {{ issue.evidence.failed_stage }} · 字段
				{{ issue.evidence.field_keys?.join('、') || '无具体字段证据' }}
			</p>
			<p v-if="issue.status === 'open'" class="status-warn">
				尚无足够恢复证据。离线回归通过或发布成功不能替代正式运行；基线不可比较、运行有限覆盖或调度仍阻断时不能确认。
			</p>
			<p v-if="issue.status === 'ready'">达到恢复计数，仍需核对正式证据；服务端会再次检查当前版本、策略、未评估/活动运行和调度状态。</p>
			<button v-if="issue.status === 'ready'" :disabled="busy" @click="resolve">核对证据并确认恢复</button>
			<p v-if="issue.status === 'resolved'" class="status-good">已确认恢复 · {{ issue.resolved_at }}。以后同类异常仍会重新打开。</p>
		</section>
	</div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type QualityIssue } from './api';
const route = useRoute(),
	issue = ref<QualityIssue>(),
	busy = ref(false),
	error = ref('');
let epoch = 0;
async function load() {
	if (busy.value) return;
	const token = ++epoch;
	busy.value = true;
	error.value = '';
	try {
		const v = await appApi.qualityIssue(String(route.params.issueID));
		if (token === epoch) issue.value = v;
	} catch (e: any) {
		if (token === epoch) error.value = e?.error?.message || e?.message || '问题证据不可用';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function resolve() {
	const i = issue.value;
	if (!i || busy.value || !window.confirm('已阅读正式恢复运行证据，确认当前问题恢复？新异常仍会重新打开。')) return;
	busy.value = true;
	const token = epoch;
	try {
		const v = await appApi.resolveQualityIssue(i.issue_id, { expected_revision: i.revision, recovery_run_id: i.recovery_run_id, confirmed: true });
		if (token === epoch) issue.value = v;
	} catch (e: any) {
		if (token === epoch) error.value = (e?.error?.message || e?.message || '响应未确认') + '；请刷新问题核对，勿自动重复提交。';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => route.params.issueID,
	() => {
		epoch++;
		issue.value = undefined;
		busy.value = false;
		void load();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>

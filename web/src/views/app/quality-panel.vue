<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>异常策略与基线</h2>
			<button :disabled="busy" @click="reload">读取当前策略</button>
		</div>
		<p class="muted">
			按正式提取的有效记录和错误判断，全部未变也算有效；不按新增数量报警。实时成功、当前发布版本和同范围证据才能用于恢复，回归/试采/发布不解除问题。
		</p>
		<p v-if="error" class="app-error" role="status">{{ error }}</p>
		<template v-if="state && form"
			><p>策略 revision {{ revision }} · 待评估正式运行 {{ state.pending_evaluations }}</p>
			<label><input v-model="form.enabled" type="checkbox" />启用异常观察（停用不会关闭已有问题）</label>
			<div class="command-form-grid">
				<label>最低有效提取记录数（0 禁用）<input v-model.number="form.min_valid_records" type="number" min="0" max="1000" /></label
				><label>最大无效记录比例 %<input v-model.number="form.max_invalid_percent" type="number" min="0" max="100" /></label
				><label>相对基线最大下降 %（0 禁用）<input v-model.number="form.drop_percent" type="number" min="0" max="100" /></label
				><label>连续失败告警次数<input v-model.number="form.failure_runs" type="number" min="1" max="10" /></label
				><label>连续正常恢复次数<input v-model.number="form.recovery_runs" type="number" min="1" max="10" /></label
				><label>调度到期宽限（分钟）<input v-model.number="form.schedule_grace_minutes" type="number" min="1" max="1440" /></label>
			</div>
			<label
				>基线正式运行 ID（留空表示取消基线）<input v-model="baselineID" class="full-input" placeholder="粘贴本方案完整已提交的实时运行 ID"
			/></label>
			<p v-if="state.policy.baseline">
				基线有效记录 {{ state.policy.baseline.valid_records }} · 列表页 {{ state.policy.baseline.list_pages }} ·
				<router-link :to="`/app/runs/${state.policy.baseline.run_id}`">查看基线证据</router-link>
			</p>
			<p v-else class="muted">暂无基线；不会推测记录骤降。可以从正常正式运行中选取。</p>
			<button :disabled="busy || !dirty" @click="save">保存策略与基线</button>
			<p class="muted">
				改策略后从后续完成的运行评估；不重判历史或关闭旧问题，恢复计数重新开始。修改目标/范围/预算/输出或路径后，旧基线可能不再可比较。
			</p>
			<template v-if="state.latest"
				><h3>最近已评估的正式证据</h3>
				<p v-if="state.latest.policy_revision !== String(revision)" class="status-warn">
					此证据按历史策略评估，等待后续正式运行验证当前策略；保存策略不会解除已有问题。
				</p>
				<router-link :to="`/app/runs/${state.latest.run_id}`">{{ state.latest.run_id }}</router-link>
				<p>
					有效 {{ state.latest.facts.valid_records }} / 无效 {{ state.latest.facts.invalid_records }} · {{ state.latest.facts.status }} · 连续失败
					{{ state.latest.result.failure_streak }}
				</p>
				<p>基线：{{ state.latest.result.baseline_status }} · {{ state.latest.result.recovery_eligible ? '本次可计入恢复证据' : '本次不证明恢复' }}</p>
				<ul>
					<li v-for="f in state.latest.result.findings" :key="f.code">{{ f.code }} · {{ f.message }}</li>
				</ul></template
			> </template
		><IssuesPanel :collector-id="collectorId" />
	</section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type QualityState, type QualityPolicy } from './api';
import IssuesPanel from './issues-panel.vue';
const props = defineProps<{ collectorId: string }>();
const state = ref<QualityState>(),
	form = ref<QualityPolicy>(),
	revision = ref(0),
	baselineID = ref(''),
	busy = ref(false),
	error = ref(''),
	saved = ref('');
let epoch = 0;
const fingerprint = () => JSON.stringify([form.value, baselineID.value]);
const dirty = computed(() => !!form.value && fingerprint() !== saved.value);
function apply(v: QualityState) {
	state.value = v;
	form.value = { ...v.policy.policy };
	revision.value = v.policy.revision;
	baselineID.value = v.policy.baseline?.run_id || '';
	saved.value = fingerprint();
}
const message = (e: any) => e?.error?.message || e?.message || '响应未确认，请重新读取';
async function reload() {
	if (busy.value || (dirty.value && !window.confirm('重新读取会覆盖本地策略编辑，确认？'))) return;
	const token = ++epoch;
	busy.value = true;
	error.value = '';
	try {
		const v = await appApi.quality(props.collectorId);
		if (token === epoch) apply(v);
	} catch (e) {
		if (token === epoch) error.value = message(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function save() {
	if (!form.value || busy.value) return;
	const token = epoch;
	busy.value = true;
	try {
		const v = await appApi.saveQualityPolicy(props.collectorId, {
			expected_revision: revision.value,
			policy: form.value,
			baseline_run_id: baselineID.value.trim(),
		});
		if (token === epoch && state.value) {
			apply({ ...state.value, policy: v });
			error.value = '策略已保存，后续正式运行开始使用。';
		}
	} catch (e) {
		if (token === epoch) error.value = message(e) + '；本地编辑保留。请读取服务器策略核对是否已保存。';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => props.collectorId,
	() => {
		epoch++;
		state.value = undefined;
		form.value = undefined;
		busy.value = false;
		void reload();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
defineExpose({ hasUnsavedChanges: () => dirty.value || busy.value });
const leave = () => (!dirty.value && !busy.value) || window.confirm('质量策略尚未保存或请求未确认，确认离开？服务器已接受的保存仍可能完成。');
onBeforeRouteLeave(leave);
onBeforeRouteUpdate(leave);
</script>

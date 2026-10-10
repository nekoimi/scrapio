<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>失败输入修复</h2>
			<button :disabled="busy" @click="load">刷新修复来源</button>
		</div>
		<p v-if="message" class="app-error" role="alert">{{ message }}</p>
		<p v-if="busy" role="status">正在读取修复上下文…</p>
		<template v-if="context">
			<p>
				<router-link :to="`/app/runs/${context.run_id}`">原运行 v{{ context.version_number }}</router-link> ·
				{{ context.failed_step_id || '无具体失败步骤' }} / {{ context.failed_stage }} · {{ context.reason }}
			</p>
			<p>带入输入：{{ context.step_id || '无保存输入步骤' }} / {{ context.stage }}</p>
			<p>
				{{ context.mode === 'fork' ? '从失败版本复制的独立草稿；请重新确认输出' : '继续当前草稿，规则没有被原版本覆盖' }}。准备修复时 revision
				{{ context.target_revision }}，当前 {{ draft.revision }}。
			</p>
			<p :class="context.evidence_status === 'available' ? 'status-good' : 'status-warn'">{{ evidenceText(context.evidence_status) }}</p>
			<p v-if="!compatible" class="status-warn">
				当前步骤/角色或入口类型已变化，不能把历史输入当作当前步骤的有效验证。请核对配置，或回原运行另建修复草稿。
			</p>
			<div class="editor-actions">
				<button v-if="context.capture_id" :disabled="busy || blocked || !compatible || context.evidence_status !== 'available'" @click="bringInput">
					带入保存输入并定位步骤</button
				><button :disabled="blocked" @click="focus('')">定位原步骤</button
				><button v-for="key in context.field_keys" :key="key" :disabled="blocked" @click="focus(key)">定位字段 {{ key }}</button
				><router-link v-if="context.document_id" :to="`/app/pages/${context.document_id}`">查看原页面证据</router-link>
			</div>
			<p class="muted">
				带入的快照是离线输入，不执行网页脚本或导航。修复后仍需保存、试采/样例检查、确认输出、发布，再独立正式验证；这里不会解除健康问题。
			</p>
		</template>
	</section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type Collector, type RepairContext, type InputCapture } from './api';
import { readError } from './data-read';
import { evidenceText } from './repair-read';
const props = defineProps<{ draft: Collector; repairId: string; blocked: boolean }>();
const emit = defineEmits<{
	(e: 'input', input: InputCapture, step: string, stage: string): void;
	(e: 'focus-field', step: string, key: string, stage: string): void;
}>();
const context = ref<RepairContext>(),
	busy = ref(false),
	message = ref('');
let epoch = 0;
const compatible = computed(() => {
	const h = context.value;
	if (!h) return false;
	const steps = Array.isArray(props.draft.definition.steps) ? props.draft.definition.steps : [];
	const step = steps.find((s: any) => s?.step_id === h.step_id);
	return (
		(step?.type === 'json_records' && h.stage === 'list' && h.format === 'json') ||
		(step?.type === 'record_set' && h.format === 'html' && (h.stage === 'list' || (h.stage === 'detail' && !!step.config?.detail)))
	);
});
async function load() {
	const token = ++epoch;
	busy.value = true;
	message.value = '';
	context.value = undefined;
	try {
		const value = await appApi.repair(props.repairId);
		if (token !== epoch) return;
		if (value.target_collector_id !== props.draft.id) throw Error('修复上下文不属于当前草稿');
		context.value = value;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function bringInput() {
	const h = context.value;
	if (!h || busy.value || props.blocked || !compatible.value) return;
	const token = epoch,
		revision = props.draft.revision;
	busy.value = true;
	message.value = '';
	try {
		const input = await appApi.capture(h.capture_id);
		if (token !== epoch) return;
		if (props.blocked || props.draft.revision !== revision) {
			message.value = '草稿已变化，请重新确认带入。';
			return;
		}
		if (input.collector_id !== props.draft.id || input.status !== 'succeeded' || input.format !== h.format)
			throw Error('保存输入不可用或不属于当前草稿');
		emit('input', input, h.step_id, h.stage);
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function focus(key: string) {
	if (context.value && !props.blocked) emit('focus-field', context.value.step_id, key, context.value.stage);
}
watch(() => [props.repairId, props.draft.id], load, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>

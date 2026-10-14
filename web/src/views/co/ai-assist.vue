<template>
	<el-card class="co-ai-assist">
		<template #header>AI 辅助 · {{ labels[kind] }}</template>
		<p class="co-muted">先预览发送范围，再调用模型；验证、接受、采用分开确认。采用只更新草稿，需要重新实时测试和发布。</p>
		<p v-if="kind === 'selectors'" class="co-muted">先声明输出字段及元素映射；AI 只调整已有字段的定位和提取方式。</p>
		<el-alert v-if="!capabilities.available" type="info" title="AI 辅助尚未启用。请在部署配置 ai_assist 中配置网关及模型。" :closable="false" />
		<el-alert v-if="problem" type="warning" :title="problem" :closable="false" />
		<el-alert v-if="notice" type="success" :title="notice" :closable="false" />
		<div v-if="kind !== 'failure'">
			<el-form-item label="明确选择保存样例">
				<el-select v-model="sampleID" placeholder="选择用于建议和验证的样例" :disabled="busy || !!pending">
					<el-option v-for="s in samples" :key="s.id" :value="s.id" :label="s.id.slice(0, 8) + ' · revision ' + s.revision" />
				</el-select>
				<el-button link :disabled="busy" @click="loadSamples">刷新样例</el-button>
			</el-form-item>
			<el-form-item v-if="kind === 'selectors'" label="建议作用的输出端口">
				<el-select v-model="port"><el-option v-for="p in ports" :key="p" :value="p" :label="p" /></el-select>
			</el-form-item>
			<el-form-item v-else label="上游样例预期输出行（从0开始）"><el-input-number v-model="outputIndex" :min="0" :max="99" /></el-form-item>
		</div>
		<p>
			网关 {{ capabilities.gateway || '未配置' }} · 模型 {{ capabilities.model || '未配置' }} · 每账号每日最多
			{{ capabilities.max_requests_per_day || 10 }} 次 · 最多1000输出 tokens。费用未配置，金额未知。
		</p>
		<div class="co-toolbar">
			<el-button :disabled="busy || !!pending || disabled || !capabilities.available || (kind !== 'failure' && !sampleID)" @click="prepare"
				>准备发送预览</el-button
			>
			<el-button :disabled="busy" @click="refresh">查询原建议 / 刷新历史</el-button>
			<el-button v-if="pending" :disabled="busy" @click="recover">查询原请求</el-button>
			<el-button v-if="pending" :disabled="busy" @click="endWaiting">确认后清除本地等待</el-button>
		</div>
		<el-select v-model="selectedID" placeholder="最近20条建议" clearable @change="loadSelected">
			<el-option v-for="s in history" :key="s.id" :value="s.id" :label="s.id.slice(0, 8) + ' · ' + s.status + ' · revision ' + s.base_revision" />
		</el-select>
		<template v-if="suggestion">
			<p>
				<el-tag>{{ suggestion.status }}</el-tag> · {{ suggestion.model }} · 基于 revision {{ suggestion.base_revision }} · 提示版本
				{{ suggestion.prompt_version }}
			</p>
			<p v-if="suggestion.prompt?.source">
				作用于 {{ suggestion.prompt.source.edge_id || suggestion.prompt.source.item_id || suggestion.prompt.source.port }} · 样例
				{{ suggestion.prompt.source.sample_id || '结构化故障信息' }}
			</p>
			<el-alert v-if="suggestion.evidence_expired" title="此建议的样例与候选证据已清理，调用记录仍保留。" type="info" :closable="false" />
			<details v-else>
				<summary>将发送的脱敏内容 · {{ suggestion.prompt_bytes }} bytes · {{ suggestion.prompt_hash }}</summary>
				<pre class="co-json">{{ JSON.stringify(suggestion.prompt, null, 2) }}</pre>
			</details>
			<el-button
				v-if="suggestion.status === 'prepared'"
				type="primary"
				:disabled="busy || !!pending || disabled || (kind !== 'failure' && suggestion.base_revision !== revision)"
				@click="invoke"
				>确认发送并调用 AI</el-button
			>
			<el-alert
				v-if="['calling', 'unknown'].includes(suggestion.status)"
				title="原模型调用尚未确认；可能已计费。请查询原建议，不自动重新调用。"
				type="warning"
				:closable="false"
			/>
			<template v-if="suggestion.result?.candidate">
				<p>{{ suggestion.result.candidate.explanation }}</p>
				<el-table :data="suggestion.result.candidate.selectors || []">
					<el-table-column prop="field" label="字段" /><el-table-column label="候选定位 / 提取"
						><template #default="{ row }"
							>{{ row.locator.strategy }} · {{ row.locator.expression }} · {{ row.extract }} {{ row.attribute }}</template
						></el-table-column
					><el-table-column prop="reason" label="原因" />
				</el-table>
				<el-table :data="suggestion.result.candidate.bindings || []"
					><el-table-column prop="field" label="下游字段" /><el-table-column prop="path" label="候选来源路径" /><el-table-column
						prop="reason"
						label="原因"
				/></el-table>
				<p>输入 tokens {{ tokens(suggestion.result.input_tokens) }} · 输出 tokens {{ tokens(suggestion.result.output_tokens) }} · 金额未知</p>
				<template v-if="kind !== 'failure'">
					<el-alert
						:type="suggestion.result.validation?.passed ? 'success' : 'warning'"
						:title="suggestion.result.validation?.passed ? '确定性样例检查通过；请核对候选差异和实际结果' : '候选未通过样例检查，不可采用'"
						:closable="false"
					/>
					<details>
						<summary>候选差异与样例证据</summary>
						<pre class="co-json">{{ JSON.stringify(suggestion.result.validation, null, 2) }}</pre>
					</details>
					<div class="co-toolbar">
						<el-button
							:disabled="busy || !!pending || disabled || suggestion.status !== 'ready' || !suggestion.result.validation?.passed"
							@click="control('validate')"
							>确认验证结果</el-button
						>
						<el-button
							:disabled="busy || !!pending || disabled || suggestion.status !== 'ready' || !suggestion.validated_hash"
							@click="control('accept')"
							>接受候选</el-button
						>
						<el-button
							type="primary"
							:disabled="busy || !!pending || disabled || suggestion.status !== 'accepted' || suggestion.base_revision !== revision"
							@click="control('apply')"
							>采用到当前草稿</el-button
						>
					</div>
				</template>
			</template>
			<el-alert v-if="suggestion.result?.error" :title="suggestion.result.error.message" type="warning" :closable="false" />
			<el-button
				v-if="['prepared', 'ready', 'accepted', 'failed', 'unknown'].includes(suggestion.status)"
				:disabled="busy || !!pending"
				@click="control('reject')"
				>结束此建议</el-button
			>
			<el-button
				v-if="['rejected', 'applied', 'failed'].includes(suggestion.status) && !suggestion.evidence_expired"
				:disabled="busy || !!pending"
				@click="cleanup"
				>清理候选与样例证据</el-button
			>
		</template>
	</el-card>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { ElMessageBox } from 'element-plus';
import { call, coApi, describe, key, type Sample, type Operation } from './api';
const props = withDefaults(
	defineProps<{
		kind: 'selectors' | 'mapping' | 'failure';
		resourceId: string;
		revision?: number;
		sampleCollectorId?: string;
		ports?: string[];
		edgeId?: string;
		itemId?: string;
		disabled?: boolean;
	}>(),
	{ revision: 0, ports: () => ['records'], disabled: false }
);
const emit = defineEmits<{ (e: 'applied'): void }>();
const labels = { selectors: '字段定位建议', mapping: '连接映射建议', failure: '失败解释（仅供审阅）' };
const capabilities = ref<any>({ available: false }),
	samples = ref<Sample[]>([]),
	sampleID = ref(''),
	port = ref('records'),
	outputIndex = ref(0),
	history = ref<any[]>([]),
	suggestion = ref<any>(),
	selectedID = ref(''),
	busy = ref(false),
	problem = ref(''),
	notice = ref('');
const storageKey = `co:ai:${props.kind}:${props.resourceId}:${props.edgeId || props.itemId || ''}`;
const pending = ref<{ key: string; operation: string; resource: string } | null>(null);
try {
	pending.value = JSON.parse(localStorage.getItem(storageKey) || 'null');
} catch {
	/* Invalid local state is ignored. */
}
const tokens = (n?: number) => (n == null || n < 0 ? '未知' : n);
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
function persist() {
	if (pending.value) localStorage.setItem(storageKey, JSON.stringify(pending.value));
	else localStorage.removeItem(storageKey);
}
async function loadSamples() {
	await guarded(async () => {
		samples.value = props.sampleCollectorId ? (await coApi.samples(props.sampleCollectorId)).items : [];
	});
}
async function loadSelected() {
	await guarded(async () => {
		suggestion.value = selectedID.value ? await call('/ai/suggestions/' + selectedID.value) : undefined;
	});
}
async function refresh() {
	await guarded(async () => {
		const resourceKind = props.kind === 'selectors' ? 'collector' : props.kind === 'mapping' ? 'task' : 'run';
		const rows: any[] = await call('/ai/suggestions?resource_kind=' + resourceKind + '&resource_id=' + encodeURIComponent(props.resourceId));
		history.value = rows.filter(
			(s) =>
				s.kind === props.kind && (props.kind !== 'mapping' || s.edge_id === props.edgeId) && (props.kind !== 'failure' || s.item_id === props.itemId)
		);
		if (selectedID.value) suggestion.value = await call('/ai/suggestions/' + selectedID.value);
	});
}
async function mutation(operation: string, resource: string, path: string, payload: unknown, method = 'post') {
	if (pending.value) throw new Error('原请求尚未确认，请先查询。');
	pending.value = { key: key(), operation, resource };
	persist();
	let result: any;
	try {
		result = await call(path, method, payload, pending.value.key, operation === 'callAIAssist' ? 25000 : 8000);
	} catch (e: any) {
		if (e?.response?.status && e.response.status < 500) {
			pending.value = null;
			persist();
		}
		throw e;
	}
	pending.value = null;
	persist();
	if (result.id && operation === 'prepareAIAssist') selectedID.value = result.id;
	if (selectedID.value) suggestion.value = await call('/ai/suggestions/' + selectedID.value);
	return result;
}
async function cleanup() {
	if (!(await confirm('清理此建议的样例与候选证据；调用次数与计费风险记录仍保留。此操作不会撤销已采用的草稿。'))) return;
	await guarded(async () => {
		await mutation('deleteAIAssist', suggestion.value.id, '/ai/suggestions/' + suggestion.value.id, { confirmed: true }, 'delete');
	});
}
async function prepare() {
	await guarded(async () => {
		suggestion.value = undefined;
		selectedID.value = '';
		await mutation('prepareAIAssist', props.resourceId, '/ai/prepare', {
			kind: props.kind,
			resource_id: props.resourceId,
			expected_revision: props.revision,
			...(props.kind === 'failure'
				? { item_id: props.itemId }
				: {
						sample_id: sampleID.value,
						...(props.kind === 'selectors' ? { port: port.value } : { edge_id: props.edgeId, output_index: outputIndex.value }),
					}),
		});
	});
}
async function confirm(text: string) {
	try {
		await ElMessageBox.confirm(text, 'AI 辅助确认', { type: 'warning', confirmButtonText: '确认', cancelButtonText: '取消' });
		return true;
	} catch {
		return false;
	}
}
async function invoke() {
	if (!(await confirm('已核对脱敏预览。发送一次模型请求，可能产生费用；未知响应不会自动重试。'))) return;
	await guarded(async () => {
		await mutation('callAIAssist', suggestion.value.id, '/ai/suggestions/' + suggestion.value.id + '/call', {
			confirmed: true,
			evidence_hash: suggestion.value.prompt_hash,
		});
	});
}
async function control(action: string) {
	if (action === 'apply' && !(await confirm('仅修改当前草稿并增加revision。之后需要重新实时测试、检查和发布。'))) return;
	await guarded(async () => {
		await mutation(action + 'AIAssist', suggestion.value.id, '/ai/suggestions/' + suggestion.value.id + '/' + action, {
			confirmed: true,
			expected_revision: props.revision,
			evidence_hash: suggestion.value.result?.validation?.hash || '',
		});
		if (action === 'apply') {
			notice.value = '已采用到草稿，请重新测试和发布。';
			emit('applied');
		}
	});
}
async function recover() {
	await guarded(async () => {
		if (!pending.value) return;
		const p = pending.value;
		const o = await call<Operation>(
			'/operations/by-key?operation=' +
				encodeURIComponent(p.operation) +
				'&resource_id=' +
				encodeURIComponent(p.resource) +
				'&key=' +
				encodeURIComponent(p.key)
		);
		if (p.operation === 'prepareAIAssist') selectedID.value = o.result?.id || '';
		else selectedID.value = p.resource;
		if (selectedID.value) suggestion.value = await call('/ai/suggestions/' + selectedID.value);
		if (
			(o.status !== 'running' && o.status !== 'queued') ||
			['ready', 'accepted', 'applied', 'unknown', 'failed', 'rejected'].includes(suggestion.value?.status)
		) {
			pending.value = null;
			persist();
			if (suggestion.value?.status === 'applied') emit('applied');
		}
	});
}
async function endWaiting() {
	if (await confirm('清除的只是本地等待记录，不会重发或取消原调用。请先确认原建议状态。')) {
		pending.value = null;
		persist();
	}
}
watch(
	() => props.sampleCollectorId,
	() => {
		sampleID.value = '';
		void loadSamples();
	}
);
onMounted(async () => {
	await guarded(async () => {
		capabilities.value = await call('/ai-capabilities');
	});
	await loadSamples();
	await refresh();
});
</script>

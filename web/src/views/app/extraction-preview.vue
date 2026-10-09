<template>
	<section class="app-card extraction-preview">
		<div class="pane-heading">
			<h2>提取与即时预览</h2>
			<span>固定快照 · 无目标网络 · 不写正式记录</span>
		</div>
		<p class="muted">保存规则或切换快照后自动预览。同一解释器将用于后续试采和正式提取；本阶段不启动正式运行。</p>
		<div class="command-form-grid">
			<label
				>提取步骤<select v-model="stepID" :disabled="busy">
					<option value="">选择步骤</option>
					<option v-for="step in steps" :key="step.step_id" :value="step.step_id">
						{{ step.step_id }} · {{ step.type === 'json_records' ? 'JSON 数组' : '网页记录' }}
					</option>
				</select></label
			><label
				>页面角色<select v-model="stage" :disabled="busy">
					<option value="list">当前页 / 列表</option>
					<option value="detail">详情页</option>
				</select></label
			>
		</div>
		<p v-if="capture">输入 {{ capture.capture_id }} · {{ capture.format }} / {{ capture.source }} · 快照 revision {{ capture.draft_revision }}</p>
		<p v-else class="muted">暂无快照。可在上方获取 HTTP/粘贴样例，或保存当前浏览器页面。</p>
		<p v-if="capture?.source === 'browser' && capture.page_state_id !== session?.page_state_id" class="status-warn">
			当前页面已变化或会话不可用；该固定快照仍能做离线验证，不代表当前页面。
		</p>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !capture || capture.status !== 'succeeded' || !stepID" @click="runPreview">
				{{ busy ? '处理中…' : '预览已保存规则' }}</button
			><button
				v-if="draft.entry_type === 'web'"
				:disabled="busy || blocked || !session?.available || !snapshotSupported || !!pendingKey"
				@click="snapshot"
			>
				保存当前浏览器 HTML 快照</button
			><button v-if="pendingKey" :disabled="busy" @click="resolveSnapshot">查询未确认快照</button
			><button v-if="pendingKey" :disabled="busy" @click="clearPending">结束本地等待</button>
		</div>
		<p v-if="draft.entry_type === 'web' && !snapshotSupported" class="muted">
			浏览器快照能力未确认；可先用离线 HTML。需要支持 snapshot 的浏览器服务。
		</p>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<section v-if="preview">
			<p v-if="stale" class="status-warn">规则、输入或角色已变化，以下结果已过期，请保存后重新预览。</p>
			<strong
				>范围 {{ preview.result.match_count_lower_bound ? '至少 ' : '' }}{{ preview.result.match_count }} 条 · 实际预览
				{{ preview.result.records.length }} 条{{ preview.result.truncated ? '（已截取）' : '' }} · 有效 {{ preview.result.valid_count }} / 无效
				{{ preview.result.invalid_count }}</strong
			>
			<p v-if="!preview.result.records.length" class="status-warn">没有发现记录，不能据此确认方案可用。请检查数组/容器及输入角色。</p>
			<p class="muted">
				{{ preview.result.interpreter_version }} · 规则 revision {{ preview.revision }} · {{ preview.result.stage }} · 哈希 {{ preview.content_hash }}
			</p>
			<p class="muted">
				来源页面：{{ preview.source_url || '离线输入' }}<span v-if="preview.page_state_id"> · 页版本 {{ preview.page_state_id }}</span>
			</p>
			<div class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th>记录</th>
							<th>状态</th>
							<th v-for="name in names" :key="name">{{ name }}</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="row in preview.result.records" :key="row.index">
							<td>{{ row.index + 1 }}</td>
							<td :class="row.valid ? 'status-good' : 'status-warn'">{{ row.valid ? '有效' : '无效' }}</td>
							<td v-for="field in row.fields" :key="field.field_key">
								<button :disabled="stale" @click="focus(field)">{{ typeof field.value === 'string' ? field.value : field.value_json }}</button
								><small>{{ field.match_count }} 命中{{ field.truncated ? ' · 截断' : '' }}</small>
								<details>
									<summary>原值 / 诊断</summary>
									<pre>{{ field.raw_json }}</pre>
									<p :class="field.valid ? 'status-good' : 'app-error'">
										{{ field.errors.length ? field.errors.map(errorLabel).join('；') : '通过' }}
									</p>
									<code>{{ field.pointer ?? field.locator?.expression ?? '容器自身' }}</code>
								</details>
							</td>
						</tr>
					</tbody>
				</table>
			</div>
			<p v-for="warning in preview.result.warnings" :key="warning" class="muted">{{ errorLabel(warning) }}</p>
		</section>
		<SamplePanel
			ref="samplePanel"
			:draft="draft"
			:capture="capture"
			:session="session"
			:step-id="stepID"
			:stage="stage"
			:blocked="blocked || busy"
			:preview="preview"
			:preview-stale="stale"
			@use-sample="useSample"
			@focus-field="(step, key, role) => emit('focus-field', step, key, role)"
		/>
		<OutputPanel
			ref="outputPanel"
			:draft="draft"
			:capture="capture"
			:step-id="stepID"
			:stage="stage"
			:blocked="blocked || busy"
			@draft-saved="(value) => emit('draft-saved', value)"
			@focus-field="(step, key, role) => emit('focus-field', step, key, role)"
		/>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { appApi, type Collector, type BrowserSession, type InputCapture, type ExtractionPreview, type ExtractedField } from './api';
import SamplePanel from './sample-panel.vue';
import OutputPanel from './output-panel.vue';
const outputPanel = ref<InstanceType<typeof OutputPanel>>();
import type { SavedSample } from './api';
const samplePanel = ref<InstanceType<typeof SamplePanel>>();
function useSample(value: SavedSample) {
	if (!value.capture) return;
	stepID.value = value.step_id;
	stage.value = value.stage;
	emit('capture-selected', value.capture);
}
defineExpose({ hasUnsavedChanges: () => !!samplePanel.value?.hasUnsavedChanges() || !!outputPanel.value?.hasUnsavedChanges() });
const props = defineProps<{ draft: Collector; session?: BrowserSession; capture?: InputCapture; blocked: boolean }>();
const emit = defineEmits<{
	(event: 'draft-saved', draft: Collector): void;
	(event: 'capture-selected', capture: InputCapture): void;
	(event: 'focus-field', stepID: string, key: string, role: string): void;
}>();
const busy = ref(false),
	message = ref(''),
	stepID = ref(''),
	stage = ref<'list' | 'detail'>('list'),
	preview = ref<ExtractionPreview>(),
	snapshotSupported = ref(false),
	pendingKey = ref('');
let disposed = false;
let previewTimer: ReturnType<typeof setTimeout> | undefined;
let needsPreview = false;
function schedulePreview() {
	if (previewTimer) clearTimeout(previewTimer);
	previewTimer = undefined;
	if (disposed || busy.value || !needsPreview || props.blocked || props.capture?.status !== 'succeeded' || !stepID.value) return;
	previewTimer = setTimeout(() => {
		previewTimer = undefined;
		void runPreview();
	}, 250);
}
const steps = computed(() =>
	Array.isArray(props.draft.definition.steps) ? props.draft.definition.steps.filter((s: any) => ['record_set', 'json_records'].includes(s?.type)) : []
);
const stale = computed(
	() =>
		!preview.value ||
		props.blocked ||
		preview.value.revision !== props.draft.revision ||
		preview.value.capture_id !== props.capture?.capture_id ||
		preview.value.content_hash !== props.capture?.content_hash ||
		preview.value.result.step_id !== stepID.value ||
		preview.value.result.stage !== stage.value
);
const names = computed(() => preview.value?.result.records[0]?.fields.map((f) => f.name) || []);
const storageKey = () => `scrapio:v22:snapshot-pending:${props.draft.id}`;
function errorLabel(code: string) {
	return (
		(
			{
				REQUIRED_EMPTY: '必填字段为空',
				MULTIPLE_MATCHES: '单值字段匹配多个值',
				INVALID_INTEGER: '无法转换为整数',
				INVALID_NUMBER: '无法转换为数字',
				INVALID_BOOLEAN: '布尔值须为 true/false',
				INVALID_DATE: '日期格式不匹配',
				TYPE_MISMATCH: '值类型不支持转换',
				EXPECTED_TEXT: '清洗/正则需要文本',
				REGEX_NO_MATCH: '正则没有匹配',
				INVALID_LOCATOR: '定位规则无效或越出范围',
				INVALID_URL: '链接地址无效',
				SENSITIVE_ATTRIBUTE: '不允许提取敏感属性',
				VALUE_BUDGET_EXCEEDED: '字段值超出预算',
				MATCH_BUDGET_REACHED: '匹配超出预算',
				RECORD_BUDGET_REACHED: '仅检查预算内记录',
				NO_RECORDS: '未找到记录',
				NO_FIELDS: '尚未配置字段',
				SAVED_HTML_ONLY_NO_NAVIGATION: '固定 HTML 检查，不访问详情或下一页',
				NO_IFRAME_SHADOW_DOM_OR_COMPUTED_VISIBILITY: '不穿透 iframe/Shadow DOM，不保留浏览器计算样式',
			} as Record<string, string>
		)[code] || code
	);
}
async function runPreview() {
	if (busy.value || props.blocked || !props.capture || props.capture.status !== 'succeeded' || !stepID.value) return;
	if (previewTimer) clearTimeout(previewTimer);
	previewTimer = undefined;
	needsPreview = false;
	const revision = props.draft.revision,
		id = props.capture.capture_id,
		step = stepID.value,
		role = stage.value;
	busy.value = true;
	message.value = '';
	try {
		const result = await appApi.previewCollector(props.draft.id, id, revision, step, role);
		if (
			!disposed &&
			revision === props.draft.revision &&
			id === props.capture?.capture_id &&
			step === stepID.value &&
			role === stage.value &&
			!props.blocked
		)
			preview.value = result;
		else message.value = '输入或规则已变化，旧响应已丢弃。';
	} catch (cause: any) {
		message.value = cause?.error?.message || '预览失败，请检查规则和快照';
	} finally {
		busy.value = false;
		schedulePreview();
	}
}
function acceptCapture(value: InputCapture) {
	emit('capture-selected', value);
	if (value.status !== 'succeeded') message.value = `${value.error_code || value.status} · ${value.error_stage}，请核对页面版本和动作回执。`;
	else message.value = '快照已保存，后续预览不依赖浏览器。';
}
async function snapshot() {
	if (busy.value || props.blocked || !props.session?.available || !snapshotSupported.value || pendingKey.value) return;
	busy.value = true;
	message.value = '';
	const key = crypto.randomUUID();
	pendingKey.value = key;
	sessionStorage.setItem(storageKey(), key);
	try {
		const value = await appApi.captureBrowserPage(props.session.session_id, props.draft.revision, props.session.page_state_id, key);
		if (!disposed) {
			acceptCapture(value);
			if (!['running', 'uncertain'].includes(value.status)) {
				pendingKey.value = '';
				sessionStorage.removeItem(storageKey());
			}
		}
	} catch (cause: any) {
		if (cause?.error && cause.error.code !== 'INTERNAL') {
			pendingKey.value = '';
			sessionStorage.removeItem(storageKey());
		}
		message.value = cause?.error?.message || '快照结果未确认，请只查询原请求。';
	} finally {
		busy.value = false;
		schedulePreview();
	}
}
async function resolveSnapshot() {
	if (busy.value || !pendingKey.value) return;
	busy.value = true;
	try {
		const value = await appApi.captureByKey(pendingKey.value);
		if (value.collector_id !== props.draft.id) throw new Error('快照属于其他方案');
		if (!disposed) {
			acceptCapture(value);
			if (!['running', 'uncertain'].includes(value.status)) {
				pendingKey.value = '';
				sessionStorage.removeItem(storageKey());
			}
		}
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '原快照仍未确认';
	} finally {
		busy.value = false;
		schedulePreview();
	}
}
function clearPending() {
	if (!window.confirm('这里只结束本地等待，不删除服务端快照；新取样会使用新请求键。确认？')) return;
	pendingKey.value = '';
	sessionStorage.removeItem(storageKey());
}
function focus(field: ExtractedField) {
	if (stale.value) return;
	emit('focus-field', preview.value!.result.step_id, field.field_key, preview.value!.result.stage);
}
watch(
	steps,
	() => {
		if (!steps.value.some((s: any) => s.step_id === stepID.value)) stepID.value = steps.value[0]?.step_id || '';
	},
	{ immediate: true }
);
watch(
	() => [
		props.draft.id,
		props.draft.revision,
		props.capture?.capture_id,
		props.capture?.content_hash,
		props.capture?.status,
		stepID.value,
		stage.value,
		props.blocked,
	],
	() => {
		needsPreview = true;
		schedulePreview();
	},
	{ immediate: true }
);
onMounted(async () => {
	pendingKey.value = sessionStorage.getItem(storageKey()) || '';
	if (props.draft.entry_type === 'web') {
		try {
			const capabilities = await appApi.capabilities();
			if (!disposed) snapshotSupported.value = !!capabilities.supports_snapshot_capture;
		} catch {
			message.value = '浏览器能力探测失败，可先使用离线快照';
		}
	}
});
onBeforeUnmount(() => {
	disposed = true;
	if (previewTimer) clearTimeout(previewTimer);
});
</script>

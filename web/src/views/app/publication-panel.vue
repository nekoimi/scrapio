<template>
	<section class="app-card publication-panel">
		<div class="pane-heading">
			<h2>检查与发布</h2>
			<button :disabled="busy" @click="reload">刷新试采及版本</button>
		</div>
		<p class="muted">发布会冻结规则、数据表 Schema、试采范围/预算和样例证据。编辑草稿不会改变旧版本；发布本身不会访问目标网站或写正式记录。</p>
		<p class="muted">每个提取角色需通过的正常样例；至少一个缺字段样例需明确断言空值或预期错误。所有保存样例会按当前规则重新检查。</p>
		<div class="command-form-grid">
			<label
				>发布依据 · 最近实时试采<select v-model="trialId" :disabled="busy || !!pendingCheck || !!pendingPublish" @change="acceptLimited = false">
					<option value="">选择一次试采</option>
					<option v-for="item in trials" :key="item.trial_id" :value="item.trial_id">
						{{ item.input.mode === 'live' ? '实时' : '离线' }} · {{ item.status }} · revision {{ item.collector_revision }} ·
						{{ new Date(item.created_at).toLocaleString() }}
					</option>
				</select></label
			>
			<label>版本说明<textarea v-model="note" maxlength="1000" :disabled="busy || !!pendingPublish" rows="2" /></label>
		</div>
		<div v-if="selectedTrial">
			<p>
				试采 {{ selectedTrial.trial_id }} · {{ selectedTrial.summary.stop_reason }} · 页面 {{ selectedTrial.summary.pages || 0 }} / 候选
				{{ selectedTrial.summary.candidates || 0 }}
			</p>
			<p>
				范围 {{ selectedTrial.input.allowed_origins.join('、') || '离线' }} · {{ selectedTrial.input.budget.seconds }} 秒 /
				{{ selectedTrial.input.budget.pages }} 文档页 / {{ selectedTrial.input.budget.records }} 候选 / {{ selectedTrial.input.budget.details }} 详情
			</p>
			<ul>
				<li v-for="warning in selectedTrial.summary.warnings || []" :key="warning" class="status-warn">{{ warning }}</li>
			</ul>
			<label v-if="selectedTrial.status === 'limited'"
				><input
					v-model="acceptLimited"
					type="checkbox"
					:disabled="busy || !!pendingCheck || !!pendingPublish"
				/>我已阅读并接受以上有限覆盖；未知/失败/离线跳过路径仍会阻止发布</label
			>
		</div>
		<p v-if="pendingCheck" class="status-warn">
			发布检查响应未确认。<button :disabled="busy" @click="recoverCheck">查询原检查</button
			><button :disabled="busy" @click="clearPending('check')">结束本地等待</button>
		</p>
		<p v-if="pendingPublish" class="status-warn">
			发布响应未确认。<button :disabled="busy" @click="recoverPublish">查询原发布</button
			><button :disabled="busy" @click="clearPending('publish')">结束本地等待</button>
		</p>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !trialId || !!pendingCheck || !!pendingPublish" @click="runCheck">
				{{ busy ? '处理中…' : '检查当前草稿是否可发布' }}</button
			><button :disabled="busy || blocked || !check?.ready || stale || !!pendingCheck || !!pendingPublish" @click="publish">
				确认发布不可变版本
			</button>
		</div>
		<p v-if="message" role="status" class="app-error">{{ message }}</p>
		<section v-if="check">
			<p :class="check.ready && !stale ? 'status-good' : 'status-warn'">
				{{ stale ? '检查已过期，请重新检查' : check.ready ? '当前检查通过' : '存在发布阻断项' }} · revision {{ check.collector_revision }} · 有效期
				{{ new Date(check.expires_at).toLocaleTimeString() }}
			</p>
			<ul>
				<li v-for="(issue, index) in check.result.issues" :key="index" class="app-error">
					<button :disabled="stale || !issue.step_id" @click="focus(issue)">
						{{ issue.message }} · {{ issue.code }} {{ issue.step_id }} {{ issue.field_key }}</button
					><small v-if="issue.sample_id"> · 样例 {{ issue.sample_id }}</small>
				</li>
			</ul>
			<p v-for="(warning, index) in check.result.warnings" :key="index" class="status-warn">{{ warning.message }}</p>
			<p class="muted">
				已冻结 {{ check.manifest.samples.length }} 个样例 · Schema {{ check.manifest.schema_version }} · {{ check.manifest.schema_hash }}
			</p>
			<details>
				<summary>能力与契约快照</summary>
				<pre>{{ JSON.stringify(check.capabilities, null, 2) }}</pre>
			</details>
		</section>
		<h3>发布版本</h3>
		<p v-if="!versions.length" class="muted">尚无发布版本；试采不会自动成为发布版本。</p>
		<ul>
			<li v-for="version in versions" :key="version.version_id">
				<button :disabled="busy" @click="viewVersion(version)">
					v{{ version.number }} · 草稿 revision {{ version.collector_revision }} · {{ new Date(version.created_at).toLocaleString() }}</button
				><span v-if="version.version_id === draft.published_version_id || version.version_id === justPublished"> · 当前发布</span
				><span> {{ version.note }}</span>
			</li>
		</ul>
		<button v-if="versionCursor" :disabled="busy" @click="loadMoreVersions">加载更多版本</button>
		<section v-if="selectedVersion">
			<h3>v{{ selectedVersion.number }} 发布摘要</h3>
			<p>
				版本 ID {{ selectedVersion.version_id }} · {{ selectedVersion.contract_version }} / {{ selectedVersion.interpreter_version }} · 发布者
				{{ selectedVersion.published_by }}
			</p>
			<p v-if="selectedVersion.collector_revision !== draft.revision" class="status-warn">
				当前草稿与此版本不同。后续运行使用此版本的固定定义，不使用最新草稿。
			</p>
			<p>{{ selectedVersion.note }}</p>
			<details>
				<summary>规则与 Schema 快照</summary>
				<pre>{{
					JSON.stringify(
						{ definition: selectedVersion.definition, schema: selectedVersion.output_schema, runtime_config: selectedVersion.runtime_config },
						null,
						2
					)
				}}</pre>
			</details>
			<button :disabled="busy" @click="loadEvidence">查看发布时样例证据</button>
			<ul v-if="evidence">
				<li v-for="item in evidence.samples" :key="item.sample_id">
					{{ item.name }} · revision {{ item.revision }} · {{ item.step_id }}/{{ item.stage }} · {{ item.comparison.status }} /
					{{ item.comparison.assertion_count }} 断言
				</li>
			</ul>
			<RunPanel v-if="selectedVersion.run_available" ref="runPanel" :version="selectedVersion" />
			<p v-else class="muted">{{ selectedVersion.run_unavailable_reason }}</p>
		</section>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue';
import { appApi, type Collector, type Trial, type PublishCheck, type PublishedVersion, type PublishIssue } from './api';
import RunPanel from './run-panel.vue';
const runPanel = ref<InstanceType<typeof RunPanel>>();
const props = defineProps<{ draft: Collector; blocked: boolean }>();
const emit = defineEmits<{
	(event: 'focus-field', step: string, key: string, stage: string): void;
	(event: 'published', version: PublishedVersion): void;
}>();
const trials = ref<Trial[]>([]),
	trialId = ref(''),
	acceptLimited = ref(false),
	note = ref(''),
	check = ref<PublishCheck>(),
	versions = ref<PublishedVersion[]>([]),
	versionCursor = ref(''),
	selectedVersion = ref<PublishedVersion>(),
	evidence = ref<Awaited<ReturnType<typeof appApi.versionEvidence>>>(),
	busy = ref(false),
	message = ref(''),
	pendingCheck = ref(''),
	pendingPublish = ref(''),
	justPublished = ref(''),
	now = ref(Date.now());
let disposed = false,
	viewEpoch = 0,
	timer: ReturnType<typeof setInterval> | undefined;
const selectedTrial = computed(() => trials.value.find((item) => item.trial_id === trialId.value));
const stale = computed(
	() =>
		!check.value ||
		props.blocked ||
		check.value.collector_revision !== props.draft.revision ||
		check.value.trial_id !== trialId.value ||
		check.value.accept_limited !== acceptLimited.value ||
		Date.parse(check.value.expires_at) <= now.value
);
const storage = (kind: string) => `scrapio:v22:publication:${props.draft.id}:${kind}`;
function error(cause: any) {
	return cause?.error?.message || cause?.message || '请求未确认，请查询原请求；本地配置已保留';
}
function clear(kind: 'check' | 'publish') {
	if (kind === 'check') pendingCheck.value = '';
	else pendingPublish.value = '';
	sessionStorage.removeItem(storage(kind));
}
function clearPending(kind: 'check' | 'publish') {
	if (window.confirm('结束本地等待不会撤销服务器已完成的操作。请先查询原请求或刷新版本，确认？')) clear(kind);
}
async function loadVersions(more = false) {
	const page = await appApi.versions(props.draft.id, more ? versionCursor.value : '');
	if (!disposed) {
		versions.value = more ? [...versions.value, ...page.items] : page.items;
		versionCursor.value = page.next_cursor;
	}
}
async function reload() {
	if (busy.value) return;
	busy.value = true;
	try {
		const page = await appApi.trials(props.draft.id);
		if (!disposed) trials.value = page.items;
		await loadVersions(false);
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function loadMoreVersions() {
	if (busy.value) return;
	busy.value = true;
	try {
		await loadVersions(true);
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function runCheck() {
	if (busy.value || props.blocked || !trialId.value || pendingCheck.value || pendingPublish.value) return;
	busy.value = true;
	message.value = '';
	const key = crypto.randomUUID();
	pendingCheck.value = key;
	sessionStorage.setItem(storage('check'), key);
	try {
		const value = await appApi.createPublishCheck(
			props.draft.id,
			{ expected_revision: props.draft.revision, trial_id: trialId.value, accept_limited: acceptLimited.value },
			key
		);
		if (!disposed) {
			clear('check');
			check.value = value;
			message.value = value.ready ? '检查通过，请确认版本内容后发布。' : '请先修正发布阻断项。';
		}
	} catch (cause: any) {
		if (cause?.error && !['INTERNAL', 'CHECK_TIMEOUT'].includes(cause.error.code)) clear('check');
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function recoverCheck() {
	if (busy.value || !pendingCheck.value) return;
	busy.value = true;
	try {
		const value = await appApi.publishCheckByKey(props.draft.id, pendingCheck.value);
		if (!trials.value.some((item) => item.trial_id === value.trial_id)) {
			const recoveredTrial = await appApi.trial(value.trial_id);
			if (!disposed) trials.value.unshift(recoveredTrial);
		}
		if (!disposed) {
			check.value = value;
			trialId.value = value.trial_id;
			acceptLimited.value = value.accept_limited;
			clear('check');
			message.value = '已恢复原检查；服务器会在发布时再次校验所有依赖。';
		}
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function applyPublished(value: PublishedVersion) {
	viewEpoch++;
	justPublished.value = value.version_id;
	clear('publish');
	selectedVersion.value = value;
	evidence.value = undefined;
	emit('published', value);
	message.value = `已发布 v${value.number}；可在摘要中确认范围后创建正式运行。`;
	try {
		await loadVersions(false);
	} catch (cause) {
		message.value += ` 版本列表刷新失败：${error(cause)}。可稍后刷新。`;
	}
}
async function publish() {
	if (busy.value || stale.value || !check.value?.ready || pendingCheck.value || pendingPublish.value) return;
	if (!window.confirm('确认发布当前检查绑定的规则、Schema、样例和试采范围？发布后版本不可修改，正式运行需另行触发。')) return;
	busy.value = true;
	const key = crypto.randomUUID();
	pendingPublish.value = key;
	sessionStorage.setItem(storage('publish'), key);
	try {
		const value = await appApi.publishVersion(
			props.draft.id,
			{ expected_revision: check.value.collector_revision, check_id: check.value.check_id, note: note.value },
			key
		);
		if (!disposed) await applyPublished(value);
	} catch (cause: any) {
		if (cause?.error && !['INTERNAL', 'CHECK_TIMEOUT'].includes(cause.error.code)) clear('publish');
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function recoverPublish() {
	if (busy.value || !pendingPublish.value) return;
	busy.value = true;
	try {
		const value = await appApi.versionByKey(pendingPublish.value);
		if (!disposed) await applyPublished(value);
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function viewVersion(version: PublishedVersion) {
	if (busy.value) return;
	const epoch = ++viewEpoch;
	evidence.value = undefined;
	try {
		const value = await appApi.version(version.version_id);
		if (!disposed && epoch === viewEpoch) selectedVersion.value = value;
	} catch (cause) {
		message.value = error(cause);
	}
}
async function loadEvidence() {
	if (busy.value || !selectedVersion.value) return;
	const id = selectedVersion.value.version_id,
		epoch = viewEpoch;
	busy.value = true;
	try {
		const value = await appApi.versionEvidence(id);
		if (!disposed && epoch === viewEpoch && selectedVersion.value?.version_id === id) evidence.value = value;
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
function focus(issue: PublishIssue) {
	if (!stale.value && issue.step_id) emit('focus-field', issue.step_id, issue.field_key || '', issue.stage || 'list');
}
onMounted(async () => {
	pendingCheck.value = sessionStorage.getItem(storage('check')) || '';
	pendingPublish.value = sessionStorage.getItem(storage('publish')) || '';
	await reload();
	if (disposed) return;
	timer = setInterval(() => (now.value = Date.now()), 1000);
});
onBeforeUnmount(() => {
	disposed = true;
	viewEpoch++;
	if (timer) clearInterval(timer);
});
defineExpose({ hasUnsavedChanges: () => busy.value || !!pendingCheck.value || !!pendingPublish.value || !!runPanel.value?.hasUnsavedChanges() });
</script>

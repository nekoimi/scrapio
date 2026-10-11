<template>
	<div class="app-page collector-editor-page">
		<div class="eyebrow">SCRAPIO / V2.2 / DRAFT</div>
		<h1>{{ draft?.name || '采集方案' }}</h1>
		<p class="lead editor-entry-summary">入口：{{ draft?.entry_url }} · revision {{ draft?.revision }}</p>
		<RepairContextPanel
			v-if="draft && typeof route.query.repair_id === 'string'"
			:draft="draft"
			:repair-id="route.query.repair_id"
			:blocked="previewBlocked"
			@input="bringRepairInput"
			@focus-field="focusPreviewField"
		/>
		<nav class="collector-stage-nav" aria-label="方案配置阶段">
			<button
				v-for="stage in editorStages"
				:key="stage.id"
				:aria-current="editorStage === stage.id ? 'step' : undefined"
				@click="editorStage = stage.id"
			>
				{{ stage.label }}
			</button>
		</nav>
		<p class="editor-stage-help">
			{{ stageHelp[editorStage] }} <span class="save-state" aria-live="polite">{{ saveState }}</span>
		</p>
		<p v-if="draft?.entry_type === 'web' && editorStage === 'edit' && previewBlocked" class="status-warn">
			请先保存当前规则；入口、样例或方案设置的未保存修改需到对应阶段处理。待确认请求请先查询结果。
		</p>

		<div v-show="editorStage === 'input'">
			<BrowserAuthorization
				v-if="draft?.entry_type === 'web'"
				ref="authorizationPanel"
				:draft="draft"
				:blocked="dirty || saving || !!conflictLatest"
				:session-active="!!browserSession && !browserSession.needs_reopen"
				@draft-saved="applyActionDraft"
			/><HttpEntry
				v-if="draft"
				ref="httpPanel"
				:draft="draft"
				:blocked="dirty || saving || !!conflictLatest"
				@draft-saved="applyActionDraft"
				@capture-saved="inputCapture = $event"
			/>
		</div>
		<div v-show="editorStage === 'edit'">
			<section v-if="draft?.entry_type === 'web'" class="app-card editor-browser">
				<div class="editor-browser-head">
					<div>
						<h2>浏览器会话</h2>
						<p class="muted">浏览/录制执行真实动作；选择记录/字段只检查元素，不点击网站。</p>
					</div>
					<div class="editor-actions">
						<button class="primary-action"
							v-if="!browserSession"
							:disabled="!draft || draft.entry_type !== 'web' || dirty || saving || connecting"
							@click="openBrowserSession"
						>
							{{ connecting ? '连接中…' : '打开浏览器会话' }}</button
						><button
							v-else-if="browserSession.needs_reopen"
							class="secondary-action"
							:disabled="connecting || closing || dirty || saving"
							@click="browserSession.status === 'disconnected' ? resumeBrowserSession() : reopenBrowserSession()"
						>
							{{ connecting ? '处理中…' : browserSession.status === 'disconnected' ? '尝试恢复' : '重新打开' }}</button
						><button v-if="browserSession" class="secondary-action" :disabled="closing" @click="closeBrowserSession">
							{{ closing ? '关闭中…' : '关闭会话' }}
						</button>
					</div>
				</div>
				<details v-if="browserSession" class="editor-advanced">
					<summary>当前页面与连接信息 · {{ browserStatus }}</summary>
					<div v-if="browserSession" class="browser-session-meta">
						<span :data-state="browserSession.status">{{ browserStatus }}</span
						><span>有效期至 {{ new Date(browserSession.expires_at).toLocaleTimeString() }}</span
						><span>revision {{ browserSession.draft_revision }}</span
						><span v-if="draft && browserSession.draft_revision !== draft.revision" class="status-warn"
							>草稿已更新；下次动作将校验当前版本，入口改变时需重连</span
						>
					</div>
					<label v-if="browserSession" for="browser-current-url">当前页面地址</label
					><input v-if="browserSession" id="browser-current-url" class="full-input" :value="browserSession.current_url" readonly />
				</details>
				<p v-if="draft?.entry_type === 'json'" class="muted">JSON 入口不创建浏览器会话。</p>
				<p v-if="browserMessage" class="app-error" role="status">{{ browserMessage }}</p>
				<BrowserActions
					v-if="draft"
					ref="actionsPanel"
					:draft="draft"
					:session="browserSession"
					:dirty="dirty"
					:saving="saving"
					:frame-ready="frameInteractive"
					:frame-refreshing="frameRefreshing"
					:frame-state="frameQueue.frame.value?.state"
					:frame-failed="!!frameError"
					:wheel-enabled="remoteWheel"
					:active="tabVisible && editorStage === 'edit'"
					:refresh-frame="updateBrowserFrame"
					@draft-saved="applyActionDraft"
					@session-state="setActionBrowserSession"
					@frame-invalidated="frameQueue.invalidate(); adaptiveFrames.activity()"
					@workbench-busy="workbenchBusy = $event"
					@highlight="setHighlights"
				>
					<template #display-settings>
						<div class="browser-display-toolbar" role="group" aria-label="画面显示设置">
							<button :aria-pressed="displayScale === 'fit'" @click="displayScale = 'fit'">适应宽度</button>
							<button :aria-pressed="displayScale === 'original'" @click="displayScale = 'original'">原始比例</button>
							<label><input type="checkbox" v-model="remoteWheel" />滚轮控制远端网页</label>
							<label><input type="checkbox" v-model="autoFrames" />持续观察（约每 5 秒更新）</label>
						</div>
						<p class="muted viewport-help">{{ remoteWheel ? '滚轮滚动远端主页面，可能触发懒加载；不支持内部滚动容器。' : '滚轮只移动本地截图，远端页面不滚动。' }} 原始比例按网页 CSS 宽度显示，可用视口滚动条本地查看。
							{{ automaticStatus }}
							{{ frameProtocolStatus }}
						</p>
					</template>
					<template #page
						><div class="browser-frame-status" :data-stale="!frameRefreshing && !frameInteractive" :data-busy="frameRefreshing" :aria-busy="frameRefreshing" role="status"><span v-if="frameRefreshing" class="browser-refresh-dot" aria-hidden="true"></span><span>{{ frameStatus }}</span></div>
						<p v-if="frameError" class="app-error" role="status">{{ frameError }}</p>
						<div class="browser-web-viewport" tabindex="0" aria-label="独立网页视口" @wheel="handleFrameWheel" @mouseenter="pointerInFrame = true; adaptiveFrames.activity()" @mouseleave="pointerInFrame = false; actionsPanel?.leaveFrame()">
						<div v-if="frameObjectUrl" class="browser-frame-stage" :style="frameDisplayStyle" :data-stale="!frameRefreshing && !frameInteractive" :aria-busy="frameRefreshing" @mouseleave="actionsPanel?.leaveFrame()">
							<img
								ref="frameImage"
								class="browser-session-frame"
								:src="frameObjectUrl"
								alt="当前网页截图；选择模式只检查元素，浏览模式准备待确认动作"
								:aria-disabled="!frameInteractive"
								@load="updateFrameSize"
								@error="handleFrameError"
								@click="prepareFrameClick"
								@mousemove="prepareFrameHover"
							/>
							<div class="frame-highlights" aria-hidden="true">
								<div v-for="(box, index) in highlightBoxes" :key="box.kind + index" class="frame-highlight" :data-kind="box.kind" :style="box.style">
									<span>{{ box.kind === 'hover' ? '悬停' : box.kind === 'similar' ? '同类' : '选中' }}</span>
								</div>
							</div>
						</div>
						<div v-else class="editor-page-empty">
							<h3>从网页上选择采集内容</h3>
							<p>打开浏览器会话后，先选一条记录，再选择字段、详情入口和下一页。</p>
							<p>需要登录的网站，请先在“入口与授权”配置凭据。</p>
						</div></div></template
					>
				</BrowserActions>
			</section>
		</div>
		<div v-show="editorStage === 'preview'">
			<ExtractionPreview
				v-if="draft"
				ref="previewPanel"
				:draft="draft"
				:capture="inputCapture"
				:session="browserSession"
				:blocked="previewBlocked"
				@draft-saved="applyActionDraft"
				@capture-selected="acceptPreviewCapture"
				@focus-field="focusPreviewField"
			/>
		</div>
		<div v-show="editorStage === 'trial'">
			<TrialPanel
				v-if="draft"
				ref="trialPanel"
				:draft="draft"
				:capture="inputCapture"
				:blocked="previewBlocked || !!previewPanel?.hasUnsavedChanges()"
				@focus-field="focusPreviewField"
			/>
		</div>
		<div v-show="editorStage === 'publish'">
			<PublicationPanel
				v-if="draft"
				ref="publicationPanel"
				:draft="draft"
				:blocked="previewBlocked || !!previewPanel?.hasUnsavedChanges() || !!trialPanel?.hasUnsavedChanges()"
				@focus-field="focusPreviewField"
				@published="draft.published_version_id = $event.version_id"
			/>
		</div>
		<div v-show="editorStage === 'settings'">
			<section class="app-card">
				<label for="collector-name">方案名称</label><input id="collector-name" v-model="name" class="full-input" />
				<details ref="definitionDetails" class="editor-advanced">
					<summary>高级：编辑定义 JSON</summary>
					<label for="collector-definition">定义 JSON</label
					><textarea id="collector-definition" v-model="definitionText" class="definition-editor" spellcheck="false" />
				</details>
				<div class="editor-actions">
					<button class="primary-action" :disabled="saving" @click="save(false)">{{ saving ? '保存中…' : '保存草稿' }}</button
					><button class="secondary-action" :disabled="!dirty || saving" @click="undoUnsaved">撤销未保存修改</button
					><button class="secondary-action" :disabled="saving || checking || dirty || !draft || !!conflictLatest" @click="validate">
						{{ checking ? '检查中…' : '检查草稿结构' }}</button
					><router-link to="/app/collectors">返回方案列表</router-link><span class="save-state" aria-live="polite">{{ saveState }}</span>
				</div>
				<p class="muted">草稿会自动保存；保存使用 revision 检查，冲突时本地编辑不会被覆盖。结构检查只验证当前定义格式，不访问目标网站。</p>
				<div v-if="draft" class="validation-state" :data-state="draft.validation_status || 'not_validated'">
					<strong>结构检查：{{ validationLabel }}</strong>
					<ul v-if="draft.validation_errors?.length">
						<li v-for="item in draft.validation_errors" :key="item">{{ item }}</li>
					</ul>
				</div>
				<div v-if="conflictLatest" class="conflict-box" role="alert">
					<strong>服务器上的草稿已更新</strong>
					<p>本地编辑仍保留在当前页面。加载服务器版本会用 revision {{ conflictLatest.revision }} 覆盖当前编辑内容。</p>
					<button @click="loadLatest">加载服务器版本</button>
				</div>
			</section>
			<CollectorHealthPanel v-if="draft" :collector-id="draft.id" />
		</div>
		<p v-if="message" class="status-good">{{ message }}</p>
		<p v-if="error" class="app-error" role="alert">{{ error }}</p>
	</div>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import BrowserActions from './browser-actions.vue';
import { createBrowserFrame } from './browser-frame';
import { createAdaptiveRefresh, createDelayedRefreshIndicator, framePoint } from './browser-workbench';
import HttpEntry from './http-entry.vue';
import BrowserAuthorization from './browser-authorization.vue';
import ExtractionPreview from './extraction-preview.vue';
import TrialPanel from './trial-panel.vue';
import PublicationPanel from './publication-panel.vue';
import CollectorHealthPanel from './collector-health.vue';
import RepairContextPanel from './repair-context.vue';
type EditorStage = 'input' | 'edit' | 'preview' | 'trial' | 'publish' | 'settings';
const editorStage = ref<EditorStage>('edit');
const definitionDetails = ref<HTMLDetailsElement>();
const editorStages = computed(() => [
	{ id: 'input' as const, label: '入口与授权' },
	...(draft.value?.entry_type === 'web' ? [{ id: 'edit' as const, label: '1 · 网页与规则' }] : []),
	{ id: 'preview' as const, label: '2 · 数据预览' },
	{ id: 'trial' as const, label: '3 · 全链路试采' },
	{ id: 'publish' as const, label: '4 · 发布与运行' },
	{ id: 'settings' as const, label: '方案设置' },
]);
const stageHelp: Record<EditorStage, string> = {
	input: '配置入口、登录凭据或导入离线样例。',
	edit: '从网页点选记录和字段，确认详情与翻页，再保存并预览。',
	preview: '使用固定快照核对转换后的数据、样例和输出表。',
	trial: '按限定范围试跑完整采集路径，核对结果与失败位置。',
	publish: '核对试采和输出，发布固定版本后开始正式运行。',
	settings: '修改名称、检查定义结构或查看方案运行健康。',
};
const publicationPanel = ref<InstanceType<typeof PublicationPanel>>();
const trialPanel = ref<InstanceType<typeof TrialPanel>>();
const httpPanel = ref<InstanceType<typeof HttpEntry>>();
import { appApi, type BrowserSession, type Collector, type PageHighlight, type InputCapture } from './api';
const actionsPanel = ref<InstanceType<typeof BrowserActions>>();
const inputCapture = ref<InputCapture>();
const previewPanel = ref<InstanceType<typeof ExtractionPreview>>();
const previewBlocked = computed(
	() =>
		dirty.value ||
		saving.value ||
		!!conflictLatest.value ||
		!!(httpPanel.value?.hasUnsavedChanges() || authorizationPanel.value?.hasUnsavedChanges()) ||
		!!actionsPanel.value?.hasUnsavedChanges()
);
function acceptPreviewCapture(value: InputCapture) {
	inputCapture.value = value;
	httpPanel.value?.acceptCapture(value);
}
async function focusPreviewField(stepID: string, key: string, role: string) {
	const steps = Array.isArray(draft.value?.definition.steps) ? draft.value!.definition.steps : [];
	const step = steps.find((s: any) => s?.step_id === stepID);
	if (key && step?.type === 'json_records' && httpPanel.value) {
		editorStage.value = 'input';
		await nextTick();
		httpPanel.value.focusField(stepID, key);
		return;
	}
	if (key && step?.type === 'record_set' && actionsPanel.value) {
		editorStage.value = 'edit';
		await nextTick();
		actionsPanel.value.focusField(stepID, key, role);
		return;
	}
	editorStage.value = 'settings';
	await nextTick();
	if (definitionDetails.value) definitionDetails.value.open = true;
	const el = document.getElementById('collector-definition') as HTMLTextAreaElement | null;
	const stepText = `"step_id": ${JSON.stringify(stepID)}`,
		start = definitionText.value.indexOf(stepText);
	let index = start,
		length = stepText.length;
	if (key && start >= 0) {
		const next = definitionText.value.indexOf('"step_id":', start + stepText.length),
			text = `"field_key": ${JSON.stringify(key)}`,
			at = definitionText.value.indexOf(text, start);
		if (at >= 0 && (next < 0 || at < next)) {
			index = at;
			length = text.length;
		}
	}
	if (el && index >= 0) {
		el.scrollIntoView({ block: 'center' });
		el.focus();
		el.setSelectionRange(index, index + length);
	}
}
async function bringRepairInput(value: InputCapture, step: string, stage: string) {
	if (previewBlocked.value) return;
	acceptPreviewCapture(value);
	editorStage.value = 'preview';
	await nextTick();
	previewPanel.value?.selectInput(step, stage);
}

function confirmLeave() {
	return (
		(!dirty.value &&
			!saving.value &&
			!actionsPanel.value?.hasUnsavedChanges() &&
			!(httpPanel.value?.hasUnsavedChanges() || authorizationPanel.value?.hasUnsavedChanges()) &&
			!previewPanel.value?.hasUnsavedChanges() &&
			!trialPanel.value?.hasUnsavedChanges() &&
			!publicationPanel.value?.hasUnsavedChanges()) ||
		window.confirm('配置尚未保存或请求未确认，离开会丢弃本地修改；服务器已接受的操作仍会继续。确认离开？')
	);
}
onBeforeRouteLeave(confirmLeave);
onBeforeRouteUpdate((to, from) => to.params.id === from.params.id || confirmLeave());
const route = useRoute();
const draft = ref<Collector>();
const name = ref('');
const definitionText = ref('');
const saving = ref(false);
const checking = ref(false);
const dirty = ref(false);
const message = ref('');
const error = ref('');
const conflictLatest = ref<Collector>();
const saveState = ref('');
let saveTimer: ReturnType<typeof setTimeout> | undefined;
let loaded = false;
let suppressChanges = false;
let savedName = '';
let savedDefinition = '';
const validationLabel = computed(
	() => ({ not_validated: '未检查', valid: '通过', invalid: '未通过', stale: '已过期' })[draft.value?.validation_status || 'not_validated']
);
const browserStatus = computed(
	() =>
		({ ready: '已连接', disconnected: '已断开', expired: '已过期', closed: '已关闭', failed: '连接失败', creating: '连接中' })[
			browserSession.value?.status || ''
		] || '未知状态'
);
const changedFieldLabels: Record<string, string> = { name: '名称', entry_url: '入口地址', definition: '采集定义' };
const browserSession = ref<BrowserSession>();
const frameQueue = createBrowserFrame({ load: target => appApi.browserFrame(target.url, target.pageStateID) });
const frameObjectUrl = computed(() => frameQueue.frame.value?.objectURL || '');
const framePageStateID = computed(() => frameQueue.frame.value?.pageStateID || '');
const frameError = frameQueue.error;
const syncingFrame = ref(false);
const frameRefreshing = computed(() => syncingFrame.value || frameQueue.busy.value);
const slowFrameRefresh = ref(false);
const refreshIndicator = createDelayedRefreshIndicator({ visible: value => { slowFrameRefresh.value = value; } });
watch(frameRefreshing, value => refreshIndicator.setUpdating(value), { flush: 'sync' });
let frameSyncRequest = 0;
const browserMessage = ref('');
const connecting = ref(false);
const closing = ref(false);
let heartbeatTimer: ReturnType<typeof setInterval> | undefined;
let lastSessionStorageKey = '';
let eventsController: AbortController | undefined;
let eventsSessionID = '';
let sessionEpoch = 0;
let disposed = false;
const frameImage = ref<HTMLImageElement>();
const authorizationPanel = ref<InstanceType<typeof BrowserAuthorization>>();
const frameSize = ref({ width: 0, height: 0 });
const frameLoaded = ref(false);
const frameInteractive = computed(() => frameLoaded.value && !syncingFrame.value && !frameRefreshing.value && !frameQueue.stale.value && !!browserSession.value?.available && !closing.value);
const frameStatus = computed(() => !browserSession.value?.available ? '浏览器会话不可用；原有配置已保留。'
	: frameRefreshing.value ? (!frameObjectUrl.value ? '正在加载网页画面…' : slowFrameRefresh.value ? '正在更新画面，完成后可点选…' : '')
	: frameQueue.stale.value ? '网页已变化或画面已过期，请更新画面后重新验证；配置已保留。'
	: !frameLoaded.value ? '画面已获取，正在显示…' : '当前画面可点选。');
watch(frameObjectUrl, () => { frameLoaded.value = false; }, { flush: 'sync' });
const displayScale = ref<'fit' | 'original'>('fit');
const remoteWheel = ref(true), autoFrames = ref(false), pointerInFrame = ref(false), workbenchBusy = ref(false);
const frameDemand = ref(false);
const tabVisible = ref(true), refreshMeasurement = ref({ duration: 0, delay: 5000, failures: 0 });
let resumeFrameSync = true;
const frameDisplayStyle = computed(() => displayScale.value === 'original'
	? { width: `${frameQueue.frame.value?.state?.viewport.width || browserSession.value?.viewport?.width || frameSize.value.width}px`, maxWidth: 'none' }
	: { width: '100%', maxWidth: 'none' });
const automaticAllowed = computed(() => tabVisible.value && editorStage.value === 'edit' && !!browserSession.value?.available
	&& !pointerInFrame.value && !workbenchBusy.value && !dirty.value && !saving.value && !closing.value && !connecting.value && !syncingFrame.value && !frameQueue.busy.value);
const adaptiveFrames = createAdaptiveRefresh({
	allowed: () => automaticAllowed.value,
	continuous: () => autoFrames.value,
	needed: () => frameDemand.value,
	capture: async () => {
		frameDemand.value = false;
		if (resumeFrameSync) {
			resumeFrameSync = false;
			await updateBrowserFrame();
		} else await frameQueue.refresh();
		return !frameQueue.stale.value && !frameError.value;
	},
	report: (duration, delay, failures) => { refreshMeasurement.value = { duration, delay, failures }; },
});
const frameProtocolStatus = computed(() => frameQueue.frame.value?.state
	? `状态扩展已启用 · 帧 ${frameQueue.frame.value.state.frame_sequence} · ${frameQueue.frame.value.state.image?.width}×${frameQueue.frame.value.state.image?.height} 像素 · DPR ${frameQueue.frame.value.state.pixel_ratio}`
	: browserSession.value?.state ? '等待带元数据的截图。' : '兼容模式：使用严格页面令牌；浏览器服务未提供画面元数据。');
const automaticStatus = computed(() => !browserSession.value?.available ? '连接会话后按需更新。'
	: !tabVisible.value || editorStage.value !== 'edit' ? '画面更新已暂停。'
	: pointerInFrame.value || workbenchBusy.value || dirty.value || saving.value ? '点选、核对样例或编辑配置期间暂缓更新；手动更新仍可使用。'
	: refreshMeasurement.value.failures ? (autoFrames.value ? `更新失败，已退避至约 ${Math.round(refreshMeasurement.value.delay / 1000)} 秒。` : '更新失败，请手动更新画面重试。')
	: frameDemand.value ? '网页有变化，等待空闲后更新。'
	: autoFrames.value ? `持续观察中，约每 5 秒更新；最近获取耗时 ${Math.round(refreshMeasurement.value.duration)}ms。` : '编辑优先：仅在网页变化、动作完成或手动请求时更新。');
watch([automaticAllowed, frameDemand, autoFrames], () => adaptiveFrames.reconcile(), { flush: 'sync' });
watch(() => frameQueue.stale.value, stale => {
	if (!stale) {
		frameDemand.value = false;
		adaptiveFrames.captured();
		if (refreshMeasurement.value.failures) refreshMeasurement.value = { ...refreshMeasurement.value, failures: 0 };
	}
}, { flush: 'sync' });
watch(() => [tabVisible.value, editorStage.value], () => { resumeFrameSync = true; frameDemand.value = true; pointerInFrame.value = false; adaptiveFrames.reconcile(); });
function handleVisibility() { tabVisible.value = document.visibilityState !== 'hidden'; }
onMounted(() => { handleVisibility(); document.addEventListener('visibilitychange', handleVisibility); adaptiveFrames.reconcile(); });
function handleFrameWheel(event: WheelEvent) {
	if (!remoteWheel.value || !tabVisible.value || editorStage.value !== 'edit') return;
	if (actionsPanel.value?.frameWheel(event, frameQueue.frame.value?.state?.viewport.height || browserSession.value?.viewport?.height || 800)) {
		event.preventDefault(); adaptiveFrames.activity();
	}
}
const pageHighlights = ref<Partial<Record<PageHighlight['kind'], PageHighlight>>>({});
const highlightBoxes = computed(() =>
	Object.values(pageHighlights.value).flatMap((group) => {
		if (!frameInteractive.value || !group || group.page_state_id !== framePageStateID.value || group.page_state_id !== browserSession.value?.page_state_id)
			return [];
		const { width, height } = frameSize.value;
		if (!width || !height) return [];
		return group.elements.flatMap((element) => {
			const b = element.bounds,
				x = Math.max(0, b.x),
				y = Math.max(0, b.y),
				right = Math.min(width, b.x + b.width),
				bottom = Math.min(height, b.y + b.height);
			if (right <= x || bottom <= y) return [];
			return [
				{
					kind: group.kind,
					style: {
						left: `${(x / width) * 100}%`,
						top: `${(y / height) * 100}%`,
						width: `${((right - x) / width) * 100}%`,
						height: `${((bottom - y) / height) * 100}%`,
					},
				},
			];
		});
	})
);
function updateFrameSize() {
	frameLoaded.value = !!frameImage.value?.naturalWidth && frameImage.value.currentSrc === frameObjectUrl.value
		&& frameImage.value.naturalWidth === frameQueue.frame.value?.width && frameImage.value.naturalHeight === frameQueue.frame.value?.height;
	frameSize.value = { width: frameImage.value?.naturalWidth || 0, height: frameImage.value?.naturalHeight || 0 };
}
function handleFrameError() {
	frameLoaded.value = false;
	frameQueue.invalidate();
	frameError.value = '画面显示失败，请更新画面后再操作。';
}
function setHighlights(group: PageHighlight) {
	pageHighlights.value = { ...pageHighlights.value, [group.kind]: group };
}
function prepareFrameHover(event: MouseEvent) {
	const img = event.currentTarget as HTMLImageElement;
	if (!frameInteractive.value) return;
	const point = framePoint(event.clientX, event.clientY, img.getBoundingClientRect(), img.naturalWidth, img.naturalHeight);
	if (point) actionsPanel.value?.frameHover(point.x, point.y, framePageStateID.value);
}
function applyActionDraft(latest: Collector) {
	if (dirty.value || saving.value) {
		conflictLatest.value = latest;
		saveState.value = '服务器新增动作；本地修改已保留';
		return;
	}
	suppressChanges = true;
	draft.value = latest;
	name.value = latest.name;
	definitionText.value = formatDefinition(latest.definition);
	savedName = name.value;
	savedDefinition = definitionText.value;
	saveState.value = '已保存';
	queueMicrotask(() => {
		suppressChanges = false;
	});
}
function prepareFrameClick(event: MouseEvent) {
	const img = event.currentTarget as HTMLImageElement;
	if (!frameInteractive.value) return;
	const point = framePoint(event.clientX, event.clientY, img.getBoundingClientRect(), img.naturalWidth, img.naturalHeight);
	if (point) actionsPanel.value?.frameClick(point.x, point.y, framePageStateID.value);
}
function formatDefinition(value: Collector['definition']) {
	return JSON.stringify(value, null, 2);
}
onMounted(async () => {
	try {
		draft.value = await appApi.collector(String(route.params.id));
		editorStage.value = draft.value.entry_type === 'json' ? 'input' : 'edit';
		name.value = draft.value.name;
		definitionText.value = formatDefinition(draft.value.definition);
		savedName = name.value;
		savedDefinition = definitionText.value;
		loaded = true;
		saveState.value = '已保存';
		lastSessionStorageKey = `scrapio:v22:session:${draft.value.id}`;
		const sessionId = sessionStorage.getItem(lastSessionStorageKey);
		if (sessionId && draft.value.entry_type === 'web') await restoreBrowserSession(sessionId);
	} catch {
		error.value = '无法读取草稿。';
	}
});
watch([name, definitionText], () => {
	if (!loaded || suppressChanges) return;
	dirty.value = true;
	message.value = '';
	saveState.value = '有未保存修改';
	if (conflictLatest.value) return;
	if (saveTimer) clearTimeout(saveTimer);
	saveTimer = setTimeout(() => save(true), 900);
});
async function save(silent = false) {
	if (!draft.value || saving.value || !dirty.value || conflictLatest.value) return;
	let definition: Record<string, any>;
	try {
		definition = JSON.parse(definitionText.value);
		if (!definition || Array.isArray(definition) || typeof definition !== 'object') throw new Error('object required');
	} catch {
		error.value = '定义必须是有效的 JSON 对象；修改暂留在本地。';
		saveState.value = '等待修正';
		return;
	}
	saving.value = true;
	error.value = '';
	const sentName = name.value;
	const sentDefinition = definitionText.value;
	saveState.value = '保存中…';
	try {
		draft.value = await appApi.updateCollector(draft.value.id, { name: sentName, expected_revision: draft.value.revision, definition });
		savedName = sentName;
		savedDefinition = sentDefinition;
		dirty.value = name.value !== sentName || definitionText.value !== sentDefinition;
		saveState.value = dirty.value ? '还有修改待保存' : '已保存';
		if (!dirty.value) {
			const summary = draft.value.save_summary;
			const fields = (summary?.changed_fields || []).map((field) => changedFieldLabels[field] || field);
			message.value = summary
				? `已保存 revision ${summary.revision}${fields.length ? `，更新：${fields.join('、')}` : '，内容未变化'}`
				: `已保存 revision ${draft.value.revision}`;
		}
	} catch (cause: any) {
		const latest = cause?.error?.latest as Collector | undefined;
		if (latest) {
			conflictLatest.value = latest;
			saveState.value = '存在版本冲突';
			error.value = '服务器版本已变化，本地修改仍保留。';
		} else {
			error.value = '草稿保存失败，本地修改仍保留。';
			saveState.value = '保存失败';
		}
	} finally {
		saving.value = false;
		if (dirty.value && !conflictLatest.value && !error.value) {
			if (saveTimer) clearTimeout(saveTimer);
			saveTimer = setTimeout(() => save(true), 900);
		} else if (!silent && dirty.value && !conflictLatest.value && error.value) {
			saveState.value = '保存失败';
		}
	}
}
async function validate() {
	if (!draft.value || dirty.value || checking.value || saving.value || conflictLatest.value) return;
	checking.value = true;
	error.value = '';
	try {
		const response = await appApi.validateCollector(draft.value.id);
		draft.value = response.collector;
		message.value = response.validation.valid ? '当前草稿结构检查通过；尚未访问目标网站。' : '草稿结构检查未通过，请按错误提示修正。';
	} catch {
		error.value = '结构检查失败，请稍后重试。';
	} finally {
		checking.value = false;
	}
}
function clearFrame() {
	frameQueue.setTarget(undefined);
	frameLoaded.value = false;
	pageHighlights.value = {};
}
function clearSession() {
	sessionEpoch++;
	frameSyncRequest++;
	syncingFrame.value = false;
	stopHeartbeat();
	stopSessionEvents();
	clearFrame();
	browserSession.value = undefined;
	if (lastSessionStorageKey) sessionStorage.removeItem(lastSessionStorageKey);
}
function isCurrent(session: BrowserSession, epoch: number) {
	return !disposed && epoch === sessionEpoch && browserSession.value?.session_id === session.session_id;
}
function ended(cause: any) {
	return ['SESSION_GONE', 'NOT_FOUND', 'UNAUTHENTICATED'].includes(cause?.error?.code);
}
async function openBrowserSession() {
	if (!draft.value || dirty.value || saving.value || connecting.value || draft.value.entry_type !== 'web') return;
	connecting.value = true;
	browserMessage.value = '';
	const epoch = sessionEpoch;
	const keyStorage = `${lastSessionStorageKey}:create:${draft.value.revision}`;
	const key = sessionStorage.getItem(keyStorage) || crypto.randomUUID();
	sessionStorage.setItem(keyStorage, key);
	try {
		const session = await appApi.createBrowserSession(draft.value.id, draft.value.revision, key);
		sessionStorage.removeItem(keyStorage);
		if (!disposed && epoch === sessionEpoch) await setBrowserSession(session);
	} catch (cause: any) {
		if (epoch !== sessionEpoch || disposed) return;
		if (cause?.error && !cause.error.retryable) sessionStorage.removeItem(keyStorage);
		browserMessage.value = cause?.error?.message || '浏览器服务不可用；重试会沿用本次请求。';
	} finally {
		if (!disposed) connecting.value = false;
	}
}
async function restoreBrowserSession(id: string) {
	connecting.value = true;
	const epoch = sessionEpoch;
	try {
		const session = await appApi.resumeBrowserSession(id);
		if (!disposed && epoch === sessionEpoch) await setBrowserSession(session);
	} catch (cause: any) {
		if (disposed || epoch !== sessionEpoch) return;
		if (ended(cause)) clearSession();
		browserMessage.value = cause?.error?.message || '原会话暂时无法恢复，请重试或重新打开。';
	} finally {
		if (!disposed) connecting.value = false;
	}
}
async function resumeBrowserSession() {
	if (browserSession.value) await restoreBrowserSession(browserSession.value.session_id);
}
async function reopenBrowserSession() {
	if (await closeBrowserSession()) await openBrowserSession();
}
async function setActionBrowserSession(session: BrowserSession) {
	await setBrowserSession(session, false);
}
async function setBrowserSession(session: BrowserSession, capture = true) {
	if (disposed) return;
	const prior = browserSession.value;
	if (prior?.session_id !== session.session_id) {
		sessionEpoch++;
		frameSyncRequest++;
		syncingFrame.value = false;
		stopHeartbeat();
		stopSessionEvents();
		pageHighlights.value = {};
		frameDemand.value = false;
	}
	browserSession.value = session;
	frameQueue.setTarget({ sessionID: session.session_id, pageStateID: session.page_state_id, url: session.frame_url, available: session.available, state: session.state });
	if (lastSessionStorageKey) sessionStorage.setItem(lastSessionStorageKey, session.session_id);
	if (!['closed', 'expired', 'failed'].includes(session.status)) startSessionEvents(session.session_id);
	else stopSessionEvents();
	if (session.available) {
		browserMessage.value = '';
		startHeartbeat();
		const changed = prior?.session_id !== session.session_id || !prior?.available || prior.page_state_id !== session.page_state_id
			|| prior.state?.document_id !== session.state?.document_id || prior.state?.frame_state_id !== session.state?.frame_state_id
			|| prior.state?.validation_state_id !== session.state?.validation_state_id;
		if (capture && frameQueue.stale.value && !frameError.value && (changed || (!frameObjectUrl.value && !frameQueue.busy.value))) {
			frameDemand.value = true;
			// First/reconnected frames load immediately; SSE changes share the idle scheduler.
			if ((!frameObjectUrl.value || !prior?.available) && tabVisible.value && editorStage.value === 'edit' && !workbenchBusy.value && !pointerInFrame.value) {
				resumeFrameSync = false;
				await frameQueue.refresh();
			}
		}
	} else {
		stopHeartbeat();
		browserMessage.value =
			session.status === 'disconnected'
				? '浏览器暂时断开；可尝试恢复，原页面丢失后需要重新打开。'
				: session.needs_reopen
					? '会话已结束，请重新打开入口页面。'
					: '';
	}
}
async function updateBrowserFrame(state?: BrowserSession) {
	const prior = browserSession.value;
	if (!prior?.available || disposed || closing.value) return;
	const epoch = sessionEpoch;
	const request = ++frameSyncRequest;
	const priorState = prior.page_state_id;
	frameDemand.value = false;
	resumeFrameSync = false;
	syncingFrame.value = true;
	if (!state) frameQueue.invalidate();
	try {
		let session = state || await appApi.browserSession(prior.session_id, true);
		if (!isCurrent(prior, epoch) || session.session_id !== prior.session_id) return;
		// SSE may have advanced while the state request was in flight. Never roll it back.
		if (state || browserSession.value?.page_state_id !== priorState) session = browserSession.value!;
		await setBrowserSession(session, false);
		await frameQueue.refresh();
	} catch (cause: any) {
		if (isCurrent(prior, epoch)) frameError.value = cause?.error?.message || '页面状态同步失败；上次截图已停用，请重试更新画面。';
	} finally {
		if (request === frameSyncRequest && epoch === sessionEpoch && !disposed) syncingFrame.value = false;
	}
}
function startHeartbeat() {
	if (heartbeatTimer || !browserSession.value?.available) return;
	heartbeatTimer = setInterval(
		async () => {
			const prior = browserSession.value;
			const epoch = sessionEpoch;
			if (!prior?.available || closing.value) return;
			try {
				const session = await appApi.heartbeatBrowserSession(prior.session_id);
				if (isCurrent(prior, epoch)) await setBrowserSession(session);
			} catch (cause: any) {
				if (!isCurrent(prior, epoch)) return;
				stopHeartbeat();
				browserMessage.value = cause?.error?.message || '浏览器会话已断开；可尝试恢复或重新打开。';
				browserSession.value = { ...prior, status: ended(cause) ? 'expired' : 'disconnected', available: false, needs_reopen: true };
				frameQueue.setTarget({ sessionID: prior.session_id, pageStateID: prior.page_state_id, url: prior.frame_url, available: false });
				if (ended(cause)) stopSessionEvents();
			}
		},
		(browserSession.value.heartbeat_interval_seconds || 20) * 1000
	);
}
function stopHeartbeat() {
	if (heartbeatTimer) clearInterval(heartbeatTimer);
	heartbeatTimer = undefined;
}
function stopSessionEvents() {
	eventsController?.abort();
	eventsController = undefined;
	eventsSessionID = '';
}
function startSessionEvents(id: string) {
	if (eventsSessionID === id && eventsController) return;
	stopSessionEvents();
	const controller = new AbortController();
	eventsController = controller;
	eventsSessionID = id;
	const epoch = sessionEpoch;
	const consume = async () => {
		let attempts = 0;
		while (!controller.signal.aborted && !disposed && epoch === sessionEpoch) {
			try {
				await appApi.streamBrowserSession(id, controller.signal, (session) => {
					if (controller.signal.aborted || disposed || epoch !== sessionEpoch || browserSession.value?.session_id !== id) return;
					attempts = 0;
					void setBrowserSession(session);
				});
				if (!controller.signal.aborted) throw new Error('事件流已结束');
			} catch (cause: any) {
				if (controller.signal.aborted || disposed || epoch !== sessionEpoch) return;
				if (cause?.terminal || ++attempts >= 5) {
					stopHeartbeat();
					stopSessionEvents();
					if (browserSession.value) {
						browserSession.value = { ...browserSession.value, status: 'disconnected', available: false, needs_reopen: true };
						frameQueue.setTarget({ sessionID: id, pageStateID: browserSession.value.page_state_id, url: browserSession.value.frame_url, available: false });
					}
					browserMessage.value = '状态连接已停止，请检查登录与服务状态后尝试恢复。';
					return;
				}
				browserMessage.value = '浏览器状态连接中断，正在重连。';
				await new Promise((resolve) => setTimeout(resolve, Math.min(1000 * 2 ** attempts, 10000)));
			}
		}
	};
	void consume();
}
async function closeBrowserSession(): Promise<boolean> {
	const session = browserSession.value;
	if (!session) return true;
	if (closing.value) return false;
	closing.value = true;
	sessionEpoch++;
	frameSyncRequest++;
	syncingFrame.value = false;
	stopHeartbeat();
	stopSessionEvents();
	frameQueue.invalidate();
	try {
		await appApi.closeBrowserSession(session.session_id);
		if (!disposed) {
			clearSession();
			browserMessage.value = '';
		}
		return true;
	} catch (cause: any) {
		if (!disposed) {
			browserSession.value = { ...session, status: 'disconnected', available: false, needs_reopen: true };
			frameQueue.setTarget({ sessionID: session.session_id, pageStateID: session.page_state_id, url: session.frame_url, available: false });
			browserMessage.value = cause?.error?.message || '关闭未确认，请重试关闭；原会话信息已保留。';
		}
		return false;
	} finally {
		if (!disposed) closing.value = false;
	}
}
function undoUnsaved() {
	suppressChanges = true;
	name.value = savedName;
	definitionText.value = savedDefinition;
	dirty.value = false;
	if (saveTimer) clearTimeout(saveTimer);
	error.value = '';
	message.value = '';
	saveState.value = '已撤销未保存修改';
	queueMicrotask(() => {
		suppressChanges = false;
	});
}
function loadLatest() {
	if (!conflictLatest.value) return;
	suppressChanges = true;
	draft.value = conflictLatest.value;
	name.value = draft.value.name;
	definitionText.value = formatDefinition(draft.value.definition);
	savedName = name.value;
	savedDefinition = definitionText.value;
	dirty.value = false;
	conflictLatest.value = undefined;
	error.value = '';
	saveState.value = '已加载服务器版本';
	queueMicrotask(() => {
		suppressChanges = false;
	});
}
onBeforeUnmount(() => {
	disposed = true;
	sessionEpoch++;
	if (saveTimer) clearTimeout(saveTimer);
	stopHeartbeat();
	stopSessionEvents();
	adaptiveFrames.dispose();
	refreshIndicator.dispose();
	document.removeEventListener('visibilitychange', handleVisibility);
	frameQueue.dispose();
});
</script>

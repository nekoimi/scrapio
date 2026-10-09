<template>
  <div class="app-page"><div class="eyebrow">SCRAPIO / V2.2 / DRAFT</div><h1>{{ draft?.name || '采集方案' }}</h1><p class="lead">入口：{{ draft?.entry_url }} · revision {{ draft?.revision }}</p><section class="app-card"><label for="collector-name">方案名称</label><input id="collector-name" v-model="name" class="full-input" /><label for="collector-definition">定义 JSON</label><textarea id="collector-definition" v-model="definitionText" class="definition-editor" spellcheck="false" /><div class="editor-actions"><button :disabled="saving" @click="save(false)">{{ saving ? '保存中…' : '保存草稿' }}</button><button class="secondary-action" :disabled="!dirty || saving" @click="undoUnsaved">撤销未保存修改</button><button class="secondary-action" :disabled="saving || checking || dirty || !draft || !!conflictLatest" @click="validate">{{ checking ? '检查中…' : '检查草稿结构' }}</button><router-link to="/app/collectors">返回方案列表</router-link><span class="save-state" aria-live="polite">{{ saveState }}</span></div><p class="muted">草稿会自动保存；保存使用 revision 检查，冲突时本地编辑不会被覆盖。结构检查只验证当前定义格式，不访问目标网站。</p><div v-if="draft" class="validation-state" :data-state="draft.validation_status || 'not_validated'"><strong>结构检查：{{ validationLabel }}</strong><ul v-if="draft.validation_errors?.length"><li v-for="item in draft.validation_errors" :key="item">{{ item }}</li></ul></div><div v-if="conflictLatest" class="conflict-box" role="alert"><strong>服务器上的草稿已更新</strong><p>本地编辑仍保留在当前页面。加载服务器版本会用 revision {{ conflictLatest.revision }} 覆盖当前编辑内容。</p><button @click="loadLatest">加载服务器版本</button></div></section><section class="app-card editor-browser"><div class="editor-browser-head"><div><h2>浏览器会话</h2><p class="muted">浏览/录制执行真实动作；选择记录/字段只检查元素，不点击网站。</p></div><div class="editor-actions"><button v-if="!browserSession" :disabled="!draft || draft.entry_type !== 'web' || dirty || saving || connecting" @click="openBrowserSession">{{ connecting ? '连接中…' : '打开浏览器会话' }}</button><button v-else-if="browserSession.needs_reopen" class="secondary-action" :disabled="connecting || closing || dirty || saving" @click="browserSession.status === 'disconnected' ? resumeBrowserSession() : reopenBrowserSession()">{{ connecting ? '处理中…' : browserSession.status === 'disconnected' ? '尝试恢复' : '重新打开' }}</button><button v-if="browserSession" class="secondary-action" :disabled="closing" @click="closeBrowserSession">{{ closing ? '关闭中…' : '关闭会话' }}</button></div></div><div v-if="browserSession" class="browser-session-meta"><span :data-state="browserSession.status">{{ browserStatus }}</span><span>有效期至 {{ new Date(browserSession.expires_at).toLocaleTimeString() }}</span><span>revision {{ browserSession.draft_revision }}</span><span v-if="draft && browserSession.draft_revision !== draft.revision" class="status-warn">草稿已更新；下次动作将校验当前版本，入口改变时需重连</span></div><label v-if="browserSession" for="browser-current-url">当前页面地址</label><input v-if="browserSession" id="browser-current-url" class="full-input" :value="browserSession.current_url" readonly /><p v-if="draft?.entry_type === 'json'" class="muted">JSON 入口不创建浏览器会话。</p><div v-if="frameObjectUrl" class="browser-frame-stage" @mouseleave="actionsPanel?.leaveFrame()"><img ref="frameImage" class="browser-session-frame" :src="frameObjectUrl" alt="当前网页截图；选择模式只检查元素，浏览模式准备待确认动作" @load="updateFrameSize" @click="prepareFrameClick" @mousemove="prepareFrameHover" /><div class="frame-highlights" aria-hidden="true"><div v-for="(box, index) in highlightBoxes" :key="box.kind + index" class="frame-highlight" :data-kind="box.kind" :style="box.style"><span>{{ box.kind === 'hover' ? '悬停' : box.kind === 'similar' ? '同类' : '选中' }}</span></div></div></div><p v-if="browserMessage" class="app-error" role="status">{{ browserMessage }}</p><BrowserActions v-if="draft" ref="actionsPanel" :draft="draft" :session="browserSession" :dirty="dirty" :saving="saving" @draft-saved="applyActionDraft" @session-state="setBrowserSession" @highlight="setHighlights" /></section><p v-if="message" class="status-good">{{ message }}</p><p v-if="error" class="app-error" role="alert">{{ error }}</p></div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import BrowserActions from './browser-actions.vue';
import { appApi, type BrowserSession, type Collector, type PageHighlight } from './api';
const actionsPanel = ref<InstanceType<typeof BrowserActions>>();
const route = useRoute(); const draft = ref<Collector>(); const name = ref(''); const definitionText = ref(''); const saving = ref(false); const checking = ref(false); const dirty = ref(false); const message = ref(''); const error = ref(''); const conflictLatest = ref<Collector>(); const saveState = ref('');
let saveTimer: ReturnType<typeof setTimeout> | undefined; let loaded = false; let suppressChanges = false; let savedName = ''; let savedDefinition = '';
const validationLabel = computed(() => ({ not_validated: '未检查', valid: '通过', invalid: '未通过', stale: '已过期' }[draft.value?.validation_status || 'not_validated']));
const browserStatus = computed(() => ({ ready: '已连接', disconnected: '已断开', expired: '已过期', closed: '已关闭', failed: '连接失败', creating: '连接中' }[browserSession.value?.status || ''] || '未知状态'));
const changedFieldLabels: Record<string, string> = { name: '名称', entry_url: '入口地址', definition: '采集定义' };
const browserSession = ref<BrowserSession>(); const frameObjectUrl = ref(''); const framePageStateID = ref(''); const browserMessage = ref(''); const connecting = ref(false); const closing = ref(false); let heartbeatTimer: ReturnType<typeof setInterval> | undefined; let lastSessionStorageKey = ''; let eventsController: AbortController | undefined; let eventsSessionID = ''; let sessionEpoch = 0; let disposed = false;
const frameImage = ref<HTMLImageElement>();
const frameSize = ref({width: 0, height: 0});
const frameLoaded = ref(false);
const pageHighlights = ref<Partial<Record<PageHighlight['kind'], PageHighlight>>>({});
const highlightBoxes = computed(() => Object.values(pageHighlights.value).flatMap(group => {
  if (!frameLoaded.value || !group || group.page_state_id !== framePageStateID.value || group.page_state_id !== browserSession.value?.page_state_id) return [];
  const {width,height} = frameSize.value;
  if (!width || !height) return [];
  return group.elements.flatMap(element => {
    const b=element.bounds, x=Math.max(0,b.x), y=Math.max(0,b.y), right=Math.min(width,b.x+b.width), bottom=Math.min(height,b.y+b.height);
    if (right<=x || bottom<=y) return [];
    return [{kind:group.kind, style:{left:`${x/width*100}%`,top:`${y/height*100}%`,width:`${(right-x)/width*100}%`,height:`${(bottom-y)/height*100}%`}}];
  });
}));
function updateFrameSize() { frameLoaded.value=!!frameImage.value?.naturalWidth && frameImage.value.currentSrc===frameObjectUrl.value; frameSize.value={width:frameImage.value?.naturalWidth || 0,height:frameImage.value?.naturalHeight || 0}; }
function setHighlights(group:PageHighlight) { pageHighlights.value={...pageHighlights.value,[group.kind]:group}; }
function prepareFrameHover(event:MouseEvent) {
  const img=event.currentTarget as HTMLImageElement, box=img.getBoundingClientRect();
  if(!frameLoaded.value || !img.naturalWidth || !browserSession.value?.available)return;
  actionsPanel.value?.frameHover((event.clientX-box.left-img.clientLeft)/img.clientWidth*img.naturalWidth,(event.clientY-box.top-img.clientTop)/img.clientHeight*img.naturalHeight,framePageStateID.value);
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
  queueMicrotask(() => { suppressChanges = false; });
}
function prepareFrameClick(event: MouseEvent) {
  const img = event.currentTarget as HTMLImageElement;
  if (!frameLoaded.value || !img.naturalWidth || !browserSession.value?.available) return;
  const box = img.getBoundingClientRect();
  const x = (event.clientX - box.left - img.clientLeft) / img.clientWidth * img.naturalWidth;
  const y = (event.clientY - box.top - img.clientTop) / img.clientHeight * img.naturalHeight;
  actionsPanel.value?.frameClick(x, y, framePageStateID.value);
}
function formatDefinition(value: Collector['definition']) { return JSON.stringify(value, null, 2); }
onMounted(async () => { try { draft.value = await appApi.collector(String(route.params.id)); name.value = draft.value.name; definitionText.value = formatDefinition(draft.value.definition); savedName = name.value; savedDefinition = definitionText.value; loaded = true; saveState.value = '已保存'; lastSessionStorageKey = `scrapio:v22:session:${draft.value.id}`; const sessionId = sessionStorage.getItem(lastSessionStorageKey); if (sessionId) await restoreBrowserSession(sessionId); } catch { error.value = '无法读取草稿。'; } });
watch([name, definitionText], () => { if (!loaded || suppressChanges) return; dirty.value = true; message.value = ''; saveState.value = '有未保存修改'; if (conflictLatest.value) return; if (saveTimer) clearTimeout(saveTimer); saveTimer = setTimeout(() => save(true), 900); });
async function save(silent = false) { if (!draft.value || saving.value || !dirty.value || conflictLatest.value) return; let definition: Record<string, any>; try { definition = JSON.parse(definitionText.value); if (!definition || Array.isArray(definition) || typeof definition !== 'object') throw new Error('object required'); } catch { error.value = '定义必须是有效的 JSON 对象；修改暂留在本地。'; saveState.value = '等待修正'; return; } saving.value = true; error.value = ''; const sentName = name.value; const sentDefinition = definitionText.value; saveState.value = '保存中…'; try { draft.value = await appApi.updateCollector(draft.value.id, { name: sentName, expected_revision: draft.value.revision, definition }); savedName = sentName; savedDefinition = sentDefinition; dirty.value = name.value !== sentName || definitionText.value !== sentDefinition; saveState.value = dirty.value ? '还有修改待保存' : '已保存'; if (!dirty.value) { const summary = draft.value.save_summary; const fields = (summary?.changed_fields || []).map(field => changedFieldLabels[field] || field); message.value = summary ? `已保存 revision ${summary.revision}${fields.length ? `，更新：${fields.join('、')}` : '，内容未变化'}` : `已保存 revision ${draft.value.revision}`; } } catch (cause: any) { const latest = cause?.error?.latest as Collector | undefined; if (latest) { conflictLatest.value = latest; saveState.value = '存在版本冲突'; error.value = '服务器版本已变化，本地修改仍保留。'; } else { error.value = '草稿保存失败，本地修改仍保留。'; saveState.value = '保存失败'; } } finally { saving.value = false; if (dirty.value && !conflictLatest.value && !error.value) { if (saveTimer) clearTimeout(saveTimer); saveTimer = setTimeout(() => save(true), 900); } else if (!silent && dirty.value && !conflictLatest.value && error.value) { saveState.value = '保存失败'; } } }
async function validate() { if (!draft.value || dirty.value || checking.value || saving.value || conflictLatest.value) return; checking.value = true; error.value = ''; try { const response = await appApi.validateCollector(draft.value.id); draft.value = response.collector; message.value = response.validation.valid ? '当前草稿结构检查通过；尚未访问目标网站。' : '草稿结构检查未通过，请按错误提示修正。'; } catch { error.value = '结构检查失败，请稍后重试。'; } finally { checking.value = false; } }
function clearFrame() {
  if (frameObjectUrl.value) URL.revokeObjectURL(frameObjectUrl.value);
  frameObjectUrl.value = '';
  framePageStateID.value = '';
  frameLoaded.value = false;
  pageHighlights.value = {};
}
function clearSession() {
  sessionEpoch++;
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
  } finally { if (!disposed) connecting.value = false; }
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
  } finally { if (!disposed) connecting.value = false; }
}
async function resumeBrowserSession() {
  if (browserSession.value) await restoreBrowserSession(browserSession.value.session_id);
}
async function reopenBrowserSession() {
  if (await closeBrowserSession()) await openBrowserSession();
}
async function setBrowserSession(session: BrowserSession) {
  if (disposed) return;
  const prior = browserSession.value;
  const epoch = sessionEpoch;
  browserSession.value = session;
  if (lastSessionStorageKey) sessionStorage.setItem(lastSessionStorageKey, session.session_id);
  if (session.available) {
    browserMessage.value = '';
    startHeartbeat();
    if (!frameObjectUrl.value || prior?.page_state_id !== session.page_state_id) await refreshFrame(session);
  } else {
    stopHeartbeat();
    clearFrame();
    browserMessage.value = session.status === 'disconnected'
      ? '浏览器暂时断开；可尝试恢复，原页面丢失后需要重新打开。'
      : session.needs_reopen ? '会话已结束，请重新打开入口页面。' : '';
  }
  if (!isCurrent(session, epoch) || browserSession.value?.page_state_id !== session.page_state_id || browserSession.value.status !== session.status) return;
  if (!['closed', 'expired', 'failed'].includes(session.status)) startSessionEvents(session.session_id);
  else stopSessionEvents();
}
async function refreshFrame(session: BrowserSession) {
  const epoch = sessionEpoch;
  try {
    const blob = await appApi.browserFrame(session.frame_url, session.page_state_id);
    if (!isCurrent(session, epoch) || browserSession.value?.page_state_id !== session.page_state_id || !browserSession.value.available) return;
    clearFrame();
    frameObjectUrl.value = URL.createObjectURL(blob);
    framePageStateID.value = session.page_state_id;
  } catch {
    if (isCurrent(session, epoch)) browserMessage.value = '页面画面已变化或暂时不可用，请恢复会话状态。';
  }
}
function startHeartbeat() {
  if (heartbeatTimer || !browserSession.value?.available) return;
  heartbeatTimer = setInterval(async () => {
    const prior = browserSession.value;
    const epoch = sessionEpoch;
    if (!prior?.available || closing.value) return;
    try {
      const session = await appApi.heartbeatBrowserSession(prior.session_id);
      if (isCurrent(prior, epoch)) await setBrowserSession(session);
    } catch (cause: any) {
      if (!isCurrent(prior, epoch)) return;
      stopHeartbeat();
      clearFrame();
      browserMessage.value = cause?.error?.message || '浏览器会话已断开；可尝试恢复或重新打开。';
      browserSession.value = { ...prior, status: ended(cause) ? 'expired' : 'disconnected', available: false, needs_reopen: true };
      if (ended(cause)) stopSessionEvents();
    }
  }, (browserSession.value.heartbeat_interval_seconds || 20) * 1000);
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
        await appApi.streamBrowserSession(id, controller.signal, session => {
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
          clearFrame();
          if (browserSession.value) browserSession.value = { ...browserSession.value, status: 'disconnected', available: false, needs_reopen: true };
          browserMessage.value = '状态连接已停止，请检查登录与服务状态后尝试恢复。';
          return;
        }
        browserMessage.value = '浏览器状态连接中断，正在重连。';
        await new Promise(resolve => setTimeout(resolve, Math.min(1000 * 2 ** attempts, 10000)));
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
  stopHeartbeat();
  stopSessionEvents();
  clearFrame();
  try {
    await appApi.closeBrowserSession(session.session_id);
    if (!disposed) { clearSession(); browserMessage.value = ''; }
    return true;
  } catch (cause: any) {
    if (!disposed) {
      browserSession.value = { ...session, status: 'disconnected', available: false, needs_reopen: true };
      browserMessage.value = cause?.error?.message || '关闭未确认，请重试关闭；原会话信息已保留。';
    }
    return false;
  } finally { if (!disposed) closing.value = false; }
}
function undoUnsaved() { suppressChanges = true; name.value = savedName; definitionText.value = savedDefinition; dirty.value = false; if (saveTimer) clearTimeout(saveTimer); error.value = ''; message.value = ''; saveState.value = '已撤销未保存修改'; queueMicrotask(() => { suppressChanges = false; }); }
function loadLatest() { if (!conflictLatest.value) return; suppressChanges = true; draft.value = conflictLatest.value; name.value = draft.value.name; definitionText.value = formatDefinition(draft.value.definition); savedName = name.value; savedDefinition = definitionText.value; dirty.value = false; conflictLatest.value = undefined; error.value = ''; saveState.value = '已加载服务器版本'; queueMicrotask(() => { suppressChanges = false; }); }
onBeforeUnmount(() => { disposed = true; sessionEpoch++; if (saveTimer) clearTimeout(saveTimer); stopHeartbeat(); stopSessionEvents(); if (frameObjectUrl.value) URL.revokeObjectURL(frameObjectUrl.value); });
</script>

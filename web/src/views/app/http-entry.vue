<template>
	<section class="app-card http-entry-panel">
		<div class="pane-heading">
			<h2>HTTP / 离线输入</h2>
			<span>{{ draft.entry_type === 'json' ? 'JSON 入口 · 无需浏览器' : '页面快照取样' }}</span>
		</div>
		<p class="muted">请求使用已保存的入口地址与配置。粘贴内容不会访问网站；获取快照和检查规则不会写正式业务记录。</p>
		<fieldset :disabled="busy || blocked">
			<label>入口地址<input v-model="entryURL" type="url" class="full-input" /></label>
			<div class="command-form-grid">
				<label
					>请求方法<select v-model="httpMethod">
						<option value="GET">GET</option>
						<option value="POST">POST</option>
					</select></label
				><label>超时（毫秒）<input v-model.number="timeoutMS" type="number" min="100" max="15000" /></label>
			</div>
			<label>查询参数（JSON 对象）<textarea v-model="queryText" rows="3" /></label
			><label>普通请求头（JSON 对象）<textarea v-model="headersText" rows="3" /></label
			><label v-if="httpMethod === 'POST'">JSON 请求体<textarea v-model="bodyText" rows="4" /></label
			><label>凭据引用（可选）<input v-model="credentialRef" placeholder="${secret:example_api}" /></label>
			<p class="muted">Authorization、Cookie、API key 和密码须使用服务端配置的引用；不支持在参数/请求体中填写秘密。完整凭据管理在 D04 接入。</p>
			<button :disabled="!requestDirty || advanced" @click="saveRequest">保存入口请求</button>
		</fieldset>
		<button :disabled="busy || blocked" @click="reloadDraft">重新加载已保存草稿</button>
		<p v-if="baseRevision !== draft.revision" class="status-warn">草稿版本已变化，请保留或放弃本地修改后重新加载。</p>
		<p v-if="requestDirty" class="status-warn">请求配置未保存；先保存再发起取样。</p>
		<p v-if="advanced" class="status-warn">请求或记录规则包含高级配置，请使用 JSON 编辑；表单不会覆盖。</p>
		<div class="command-form-grid">
			<label
				>取样方式<select v-model="source" :disabled="busy">
					<option value="http">访问 HTTP 接口</option>
					<option value="offline">粘贴离线样例</option>
				</select></label
			><label
				>内容格式<select v-model="format" :disabled="busy">
					<option value="json">JSON</option>
					<option value="html">HTML</option>
				</select></label
			>
		</div>
		<label v-if="source === 'offline'"
			>离线内容（最多 1 MiB）<textarea v-model="offline" rows="8" :disabled="busy" placeholder="粘贴真实 JSON 或 HTML；已知秘密字段会脱敏" />
		</label>
		<div class="editor-actions">
			<button :disabled="!canCapture" @click="createCapture">
				{{ busy ? '处理中…' : source === 'http' ? '确认访问并获取快照' : '保存离线快照' }}</button
			><button v-if="unresolved" :disabled="busy || blocked" @click="resolveCapture">查询原请求（沿用请求键）</button
			><button v-if="capture" :disabled="busy" @click="loadCapture(capture.capture_id)">重新读取快照状态</button
			><button
				v-if="unresolved || capture?.status === 'running' || capture?.status === 'uncertain'"
				:disabled="busy || capture?.status === 'running'"
				@click="abandonPending"
			>
				结束本地等待，允许新取样
			</button>
		</div>
		<label>读取已有快照 ID<input v-model="captureID" placeholder="capture_id" /></label
		><button :disabled="busy || !captureID.trim()" @click="loadCapture(captureID.trim())">打开快照</button>
		<section v-if="capture" class="capture-result">
			<strong>{{ capture.status }} · {{ capture.source }} · {{ capture.byte_count }} bytes</strong>
			<p>{{ capture.final_url }} · HTTP {{ capture.status_code || '—' }} · {{ capture.content_type }}</p>
			<p v-if="capture.error_code" class="app-error">{{ capture.error_code }} · {{ capture.error_stage }}。失败或未确认的 POST 不会自动重发。</p>
			<p class="muted">快照 revision {{ capture.draft_revision }} · {{ capture.content_hash }} · 已做基础脱敏</p>
			<p v-if="capture.draft_revision !== draft.revision" class="status-warn">
				这是较早 revision 的固定输入，可用当前规则检查；不表示当前 HTTP 请求已验证。
			</p>
			<details v-if="capture.content">
				<summary>已保存内容（大整数与原始值以此为准）</summary>
				<pre>{{ capture.content }}</pre>
			</details>
		</section>
		<template v-if="tree !== undefined"
			><h3>JSON 响应树</h3>
			<p class="muted">先选择记录数组，再选择其某个元素里的字段；字段路径相对于每条记录。树分层展开，支持手填 JSON Pointer。</p>
			<div class="json-tree">
				<JsonTreeNode :key="capture!.capture_id" :value="tree" path="" :depth="0" :disabled="busy || blocked || advanced" @select="selectPointer" />
			</div>
			<fieldset :disabled="busy || blocked || advanced">
				<label>记录数组 JSON Pointer<input v-model="plan.array_pointer" placeholder="根数组留空，或 /data/items" /></label
				><label>最多检查记录<input v-model.number="plan.max_records" type="number" min="1" max="20" /></label>
				<div v-for="(field, index) in plan.fields" :key="index" class="json-field-row">
					<input v-model="field.name" placeholder="字段名" aria-label="字段名称" /><input
						v-model="field.pointer"
						placeholder="相对指针，如 /title"
						aria-label="字段 JSON Pointer"
					/><button @click="plan.fields.splice(index, 1)">删除</button>
				</div>
				<button @click="plan.fields.push({ name: `field_${plan.fields.length + 1}`, pointer: '' })">添加字段</button
				><button :disabled="!planDirty && !!stepID" @click="savePlan">保存 JSON 记录规则</button>
			</fieldset>
			<p v-if="planDirty" class="status-warn">JSON 规则未保存。</p>
			<button :disabled="busy || blocked || planDirty || !stepID || advanced" @click="checkJSON">检查已保存快照（不访问网站）</button></template
		>
		<section v-if="check" class="record-preview-results">
			<strong>{{ check.match_count }} 条数组记录{{ check.truncated ? ' · 有界截取' : '' }}</strong>
			<p v-if="check.revision !== draft.revision || planDirty || check.capture_id !== capture?.capture_id" class="status-warn">
				规则或输入已变化，以下结果已过期。
			</p>
			<div class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th>记录</th>
							<th v-for="field in plan.fields" :key="field.name">{{ field.name }}</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="row in check.records" :key="row.index">
							<td>{{ row.index + 1 }}</td>
							<td v-for="field in row.fields" :key="field.name">{{ field.found ? field.raw_value : '未命中' }}</td>
						</tr>
					</tbody>
				</table>
			</div>
			<p class="muted">仅展示快照中的基础原值与命中情况；转换/必填/多值和正式执行在阶段 B 接入。</p>
		</section>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, nextTick, ref, watch } from 'vue';
import JsonTreeNode from './json-tree-node.vue';
import { appApi, type Collector, type HTTPEntryRequest, type InputCapture, type JSONRecordPlan, type JSONCaptureCheck } from './api';
const props = defineProps<{ draft: Collector; blocked: boolean }>();
const emit = defineEmits<{ (event: 'draft-saved', draft: Collector): void }>();
const busy = ref(false),
	message = ref(''),
	entryURL = ref(''),
	httpMethod = ref<'GET' | 'POST'>('GET'),
	queryText = ref('{}'),
	headersText = ref('{}'),
	bodyText = ref('{}'),
	credentialRef = ref(''),
	timeoutMS = ref(10000);
const source = ref<'http' | 'offline'>('http'),
	format = ref<'json' | 'html'>(props.draft.entry_type === 'json' ? 'json' : 'html'),
	offline = ref(''),
	capture = ref<InputCapture>(),
	captureID = ref(''),
	tree = ref<unknown>();
const plan = ref<JSONRecordPlan>({ array_pointer: '', max_records: 10, fields: [] }),
	stepID = ref(''),
	check = ref<JSONCaptureCheck>();
const savedRequest = ref(''),
	savedPlan = ref(''),
	baseRevision = ref(props.draft.revision),
	advanced = ref(false);
let disposed = false;
const unresolved = ref<{ key: string; input?: Parameters<typeof appApi.createCapture>[0] }>();
function requestState() {
	return JSON.stringify([entryURL.value, httpMethod.value, queryText.value, headersText.value, bodyText.value, credentialRef.value, timeoutMS.value]);
}
const requestDirty = computed(() => requestState() !== savedRequest.value),
	planDirty = computed(() => JSON.stringify(plan.value) !== savedPlan.value);
const canCapture = computed(
	() =>
		!busy.value &&
		!props.blocked &&
		!unresolved.value &&
		capture.value?.status !== 'running' &&
		capture.value?.status !== 'uncertain' &&
		(source.value === 'offline' ? !!offline.value : !requestDirty.value) &&
		baseRevision.value === props.draft.revision
);
function key() {
	return `scrapio:v22:capture:${props.draft.id}`;
}
function loadDraft() {
	const definition = props.draft.definition,
		config = definition.http_request ?? {},
		steps = Array.isArray(definition.steps) ? definition.steps : [],
		jsonSteps = steps.filter((s: any) => s?.type === 'json_records'),
		step = jsonSteps[0];
	entryURL.value = props.draft.entry_url;
	httpMethod.value = config.method || 'GET';
	queryText.value = JSON.stringify(config.query || {}, null, 2);
	headersText.value = JSON.stringify(config.headers || {}, null, 2);
	bodyText.value = JSON.stringify(config.body ?? {}, null, 2);
	credentialRef.value = config.credential_ref || '';
	timeoutMS.value = config.timeout_ms || 10000;
	const rawPlan = step?.config;
	advanced.value =
		typeof config !== 'object' ||
		Array.isArray(config) ||
		Object.keys(config).some((k) => !['method', 'query', 'headers', 'body', 'credential_ref', 'timeout_ms'].includes(k)) ||
		!Array.isArray(definition.steps) ||
		jsonSteps.length > 1 ||
		(!!step &&
			(!rawPlan ||
				!Array.isArray(rawPlan.fields) ||
				Object.keys(rawPlan).some((k) => !['array_pointer', 'max_records', 'fields'].includes(k)) ||
				rawPlan.fields.some(
					(f: any) =>
						!f || Object.keys(f).some((k) => !['name', 'pointer'].includes(k)) || typeof f.name !== 'string' || typeof f.pointer !== 'string'
				)));
	plan.value = rawPlan && Array.isArray(rawPlan.fields) ? JSON.parse(JSON.stringify(rawPlan)) : { array_pointer: '', max_records: 10, fields: [] };
	stepID.value = step?.step_id || '';
	savedRequest.value = requestState();
	savedPlan.value = JSON.stringify(plan.value);
	baseRevision.value = props.draft.revision;
	check.value = undefined;
}
async function reloadDraft() {
	if (busy.value || props.blocked) return;
	if (hasUnsavedChanges() && !window.confirm('重新加载会放弃本地未保存的请求和 JSON 规则，确认？')) return;
	busy.value = true;
	try {
		const latest = await appApi.collector(props.draft.id);
		if (!disposed) {
			emit('draft-saved', latest);
			await nextTick();
			loadDraft();
			message.value = '已加载服务器草稿。';
		}
	} catch (cause: any) {
		message.value = cause?.error?.message || '读取草稿失败';
	} finally {
		busy.value = false;
	}
}
function abandonPending() {
	if (busy.value || capture.value?.status === 'running') return;
	if (!window.confirm('原请求可能已在目标网站执行。这里只结束本地等待，不撤销原请求；新取样会使用新请求键。请先在目标网站核实 POST 结果。继续？'))
		return;
	unresolved.value = undefined;
	capture.value = undefined;
	tree.value = undefined;
	check.value = undefined;
	sessionStorage.removeItem(key() + ':pending');
	sessionStorage.removeItem(key());
	message.value = '已结束本地等待。原快照仍可用 ID 查询，新取样须再次确认。';
}
function stringMap(text: string) {
	const value = JSON.parse(text);
	if (!value || Array.isArray(value) || typeof value !== 'object' || Object.values(value).some((v) => typeof v !== 'string'))
		throw new Error('查询参数/请求头必须是字符串值 JSON 对象');
	return value;
}
function config(): HTTPEntryRequest {
	return {
		method: httpMethod.value,
		query: stringMap(queryText.value),
		headers: stringMap(headersText.value),
		timeout_ms: timeoutMS.value,
		...(httpMethod.value === 'POST' ? { body: JSON.parse(bodyText.value) } : {}),
		...(credentialRef.value.trim() ? { credential_ref: credentialRef.value.trim() } : {}),
	};
}
async function update(definition: Record<string, any>) {
	const latest = await appApi.updateCollector(props.draft.id, { name: props.draft.name, expected_revision: baseRevision.value, definition });
	if (disposed) return;
	baseRevision.value = latest.revision;
	emit('draft-saved', latest);
}
async function saveRequest() {
	if (busy.value || props.blocked || advanced.value) return;
	busy.value = true;
	message.value = '';
	try {
		await update({ ...props.draft.definition, entry_url: entryURL.value, http_request: config() });
		savedRequest.value = requestState();
		message.value = '请求配置已保存；实际取样仍需确认。';
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '保存失败，本地修改已保留';
	} finally {
		busy.value = false;
	}
}
function applyCapture(value: InputCapture) {
	if (value.collector_id !== props.draft.id) throw new Error('快照属于其他方案');
	capture.value = value;
	captureID.value = value.capture_id;
	sessionStorage.setItem(key(), value.capture_id);
	check.value = undefined;
	tree.value = value.status === 'succeeded' && value.format === 'json' ? JSON.parse(value.content) : undefined;
}
async function createCapture() {
	if (!canCapture.value) return;
	if (
		source.value === 'http' &&
		!window.confirm(`将以已保存的 ${httpMethod.value} 访问 ${props.draft.entry_url}。POST 可能产生外部副作用；失败不会自动重发。确认？`)
	)
		return;
	if (!canCapture.value) return;
	busy.value = true;
	message.value = '';
	const previous = {
		key: unresolved.value?.key || crypto.randomUUID(),
		input: {
			collector_id: props.draft.id,
			expected_revision: props.draft.revision,
			source: source.value,
			format: format.value,
			content: source.value === 'offline' ? offline.value : undefined,
			confirmed: true,
		},
	};
	sessionStorage.setItem(key() + ':pending', previous.key);
	try {
		const value = await appApi.createCapture(previous.input, previous.key);
		if (!disposed) {
			applyCapture(value);
			sessionStorage.removeItem(key() + ':pending');
		}
	} catch (cause: any) {
		if (!cause?.error || cause.error.code === 'INTERNAL') unresolved.value = previous;
		else sessionStorage.removeItem(key() + ':pending');
		message.value = cause?.error?.message || '请求未确认，沿用原请求键查询；不要重复发起 POST。';
	} finally {
		busy.value = false;
	}
}
async function resolveCapture() {
	if (!unresolved.value || busy.value) return;
	busy.value = true;
	try {
		const value = await appApi.captureByKey(unresolved.value.key);
		if (!disposed) {
			applyCapture(value);
			unresolved.value = undefined;
			sessionStorage.removeItem(key() + ':pending');
		}
	} catch (cause: any) {
		message.value = cause?.error?.message || '原请求仍未确认';
	} finally {
		busy.value = false;
	}
}
async function loadCapture(id: string) {
	if (busy.value) return;
	busy.value = true;
	message.value = '';
	try {
		const value = await appApi.capture(id);
		if (!disposed) applyCapture(value);
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '快照读取失败';
	} finally {
		busy.value = false;
	}
}
function selectPointer(pointer: string, array: boolean) {
	if (array) {
		if (plan.value.fields.length && !window.confirm('切换记录数组会清空已有相对字段，确认？')) return;
		plan.value.array_pointer = pointer;
		plan.value.fields = [];
		message.value = '已选择数组，继续点选其任一元素内的字段。';
		return;
	}
	const prefix = plan.value.array_pointer + '/';
	if (!pointer.startsWith(prefix)) {
		message.value = '请在已选数组的某个元素里选择字段';
		return;
	}
	const parts = pointer.slice(prefix.length).split('/');
	if (!/^\d+$/.test(parts[0])) {
		message.value = '字段必须位于数组元素内';
		return;
	}
	const name = window.prompt('字段名称', `field_${plan.value.fields.length + 1}`);
	if (!name) return;
	plan.value.fields.push({ name, pointer: parts.length === 1 ? '' : '/' + parts.slice(1).join('/') });
}
async function savePlan() {
	if (busy.value || props.blocked || advanced.value || !Array.isArray(props.draft.definition.steps)) return;
	busy.value = true;
	message.value = '';
	try {
		const id = stepID.value || `json-${crypto.randomUUID()}`,
			previous = props.draft.definition.steps.find((s: any) => s.step_id === id);
		const next = { ...previous, step_id: id, type: 'json_records', config: JSON.parse(JSON.stringify(plan.value)) };
		await update({
			...props.draft.definition,
			steps: previous ? props.draft.definition.steps.map((s: any) => (s.step_id === id ? next : s)) : [...props.draft.definition.steps, next],
		});
		stepID.value = id;
		savedPlan.value = JSON.stringify(plan.value);
		message.value = 'JSON 数组和字段规则已保存。';
	} catch (cause: any) {
		message.value = cause?.error?.message || '规则保存失败，本地修改已保留';
	} finally {
		busy.value = false;
	}
}
async function checkJSON() {
	if (!capture.value || busy.value || props.blocked || planDirty.value || !stepID.value) return;
	busy.value = true;
	message.value = '';
	const revision = props.draft.revision,
		id = capture.value.capture_id;
	try {
		const result = await appApi.checkCaptureJSON(id, revision, stepID.value);
		if (!disposed && revision === props.draft.revision && id === capture.value?.capture_id) check.value = result;
	} catch (cause: any) {
		message.value = cause?.error?.message || 'JSON 规则未命中，请检查数组路径与字段';
	} finally {
		busy.value = false;
	}
}
watch(
	() => props.draft.revision,
	() => {
		if (!busy.value && !requestDirty.value && !planDirty.value) loadDraft();
	}
);
onMounted(async () => {
	loadDraft();
	const id = sessionStorage.getItem(key());
	const pending = sessionStorage.getItem(key() + ':pending');
	if (pending) {
		unresolved.value = { key: pending };
		message.value = '有未确认的原请求，请先查询；不会自动重新执行。';
	}
	if (id) await loadCapture(id);
});
onBeforeUnmount(() => {
	disposed = true;
});
function hasUnsavedChanges() {
	return requestDirty.value || planDirty.value;
}
defineExpose({ hasUnsavedChanges });
</script>

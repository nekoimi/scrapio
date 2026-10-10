<template>
	<section class="sample-panel">
		<div class="pane-heading">
			<h3>样例与检查证据</h3>
			<button :disabled="busy" @click="loadList(false)">刷新样例</button>
		</div>
		<p class="muted">保存固定输入作为正常或缺字段样例；检查只验证离线提取，不验证导航。每个方案最多 100 个样例，每个样例保留最近 20 次证据。</p>
		<p><a href="#sample-regression">检查全部保存样例、比较版本与复制历史草稿</a></p>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<p v-if="pendingKey" class="status-warn">
			上次样例保存结果未确认。先查询原请求，再继续编辑。<button :disabled="busy" @click="recover">查询原请求</button
			><button :disabled="busy" @click="endWaiting">结束本地等待</button>
		</p>
		<div class="command-form-grid">
			<label
				>样例<select
					:value="selected?.sample_id || ''"
					:disabled="busy || !!pendingKey"
					@change="selectSample(($event.target as HTMLSelectElement).value)"
				>
					<option value="">新建：使用当前快照</option>
					<option v-for="item in items" :key="item.sample_id" :value="item.sample_id">
						{{ item.name }} · {{ item.kind === 'normal' ? '正常' : '缺字段' }} / {{ item.stage }} {{ item.protected ? '· 受保护' : '' }}
					</option>
				</select></label
			>
			<button v-if="nextCursor" :disabled="busy" @click="loadList(true)">加载更多样例</button>
			<label>样例名称<input v-model="name" maxlength="160" :disabled="busy || !!pendingKey" placeholder="例如：列表正常页 / 缺少评分页" /></label>
			<label
				>样例类型<select v-model="kind" :disabled="busy || !!selected || !!pendingKey">
					<option value="normal">正常输入</option>
					<option value="missing_field">字段缺失输入</option>
				</select></label
			>
			<label><input v-model="protectedSample" type="checkbox" :disabled="busy || !!pendingKey" />保留保护（删除前需解除）</label>
		</div>
		<p v-if="selected" class="muted">
			输入 {{ selected.capture_id }} · {{ selected.stage }} / {{ selected.step_id }} · 保存规则 revision {{ selected.saved_revision }} · 样例 revision
			{{ selected.revision }} · 快照随样例保留
		</p>
		<p v-else class="muted">
			当前输入 {{ capture?.capture_id || '未选择' }} · {{ stage }} / {{ stepId || '未选择步骤' }}；角色与输入保存后不可替换。
		</p>
		<label :for="`sample-expected-${draft.id}`">预期结果（JSON；{} 表示尚未设置）</label>
		<textarea
			:id="`sample-expected-${draft.id}`"
			v-model="expectedText"
			class="definition-editor"
			:disabled="busy || !!pendingKey"
			spellcheck="false"
		/>
		<p class="muted">
			可配置 record_count、valid_count、invalid_count，以及 fields 中的 record_index（从 0
			起）、field_key、value、errors。值与错误码精确比较；显式预期的字段错误可作为负例通过，其他字段错误仍判失败。截断检查不判通过。
		</p>
		<div class="editor-actions">
			<button
				:disabled="busy || !!pendingKey || !preview || previewStale || (selected && selected.capture_id !== preview.capture_id)"
				@click="fillExpected"
			>
				从预览填入首条预期（请确认）</button
			><button v-if="selected" :disabled="busy || !!pendingKey" @click="reloadSelected">重读样例（放弃本地修改）</button>
		</div>
		<p v-if="!selected?.sample_id" class="muted">先保存输入快照，再保存样例；保存不会重新采集 HTML/JSON。</p>
		<template v-if="!selected && capture?.source === 'browser'">
			<label
				><input v-model="includeScreenshot" type="checkbox" :disabled="busy || !!pendingKey || !canScreenshot" />附带当前页截图（默认不保存）</label
			>
			<p v-if="!canScreenshot" class="status-warn">当前会话或页版本与快照不同，不能附带截图。</p>
			<template v-if="includeScreenshot">
				<button :disabled="busy || imageLoading || !!pendingKey" @click="loadFrame">读取同版本画面并设置遮挡</button>
				<p class="muted">在图片上拖动框选敏感区域；可整图遮挡。服务端仅保存重新编码后的遮挡 PNG，不保存原图。截图不能保证自动识别所有敏感信息。</p>
				<div
					v-if="frameURL"
					class="sample-mask-stage"
					@pointerdown="startMask"
					@pointermove="moveMask"
					@pointerup="finishMask"
					@pointercancel="cancelMask"
				>
					<img :src="frameURL" alt="待确认的样例截图，拖动以遮挡敏感区域" draggable="false" />
					<div v-for="(mask, index) in shownMasks" :key="index" class="sample-image-mask" :style="maskStyle(mask)" />
				</div>
				<div class="editor-actions">
					<button
						:disabled="busy || !!pendingKey"
						@click="
							masks = [{ x: 0, y: 0, width: 1, height: 1 }];
							reviewed = false;
						"
					>
						遮挡整张图片</button
					><button
						:disabled="busy || !!pendingKey"
						@click="
							masks = [];
							reviewed = false;
						"
					>
						清除遮挡
					</button>
				</div>
				<label><input v-model="reviewed" type="checkbox" :disabled="busy || !!pendingKey || !frameURL" />已确认敏感区域处理，允许保存截图</label>
			</template>
		</template>
		<div class="editor-actions">
			<button
				v-if="!selected"
				:disabled="
					busy ||
					blocked ||
					!!pendingKey ||
					capture?.status !== 'succeeded' ||
					!stepId ||
					(includeScreenshot && (!reviewed || !frameURL || !canScreenshot))
				"
				@click="create"
			>
				保存为样例
			</button>
			<button v-else :disabled="busy || blocked || !!pendingKey || !dirty" @click="save">保存样例修改</button>
			<button v-if="selected" :disabled="busy || blocked || dirty || !!pendingKey" @click="check">验证并保存检查证据</button>
			<button v-if="selected" :disabled="busy || dirty || !!pendingKey" @click="useInput">带入预览</button>
			<button v-if="selected" :disabled="busy || dirty || !!pendingKey || selected.protected" @click="remove">删除样例及检查证据</button>
		</div>
		<template v-if="selected">
			<details>
				<summary>动作证据 · {{ selected.actions.length }} 条{{ selected.actions_truncated ? '（只保留最近 50 条）' : '' }}</summary>
				<p class="muted">仅保存取样前已完成的动作回执摘要，不保存输入值，也不重放动作。</p>
				<ul>
					<li v-for="action in selected.actions" :key="action.command_id">
						{{ action.type }} · {{ action.status }} · {{ action.before_page_state_id }} → {{ action.after_page_state_id }} {{ action.error_code }}
					</li>
				</ul>
				<p v-if="!selected.actions.length">无浏览器动作证据（HTTP/离线样例或直接打开页面）。</p>
			</details>
			<details v-if="selected.screenshot_url">
				<summary>保存的截图与策略</summary>
				<p>{{ selected.screenshot_policy }} · 哈希 {{ selected.screenshot_hash }}</p>
				<button :disabled="busy || imageLoading" @click="loadSavedImage">查看已遮挡图片</button
				><img v-if="savedImageURL" class="sample-saved-image" :src="savedImageURL" alt="已确认并遮挡的样例截图" />
			</details>
			<div class="command-form-grid">
				<label
					>检查历史<select :value="evidence?.check_id || ''" :disabled="busy" @change="loadCheck(($event.target as HTMLSelectElement).value)">
						<option value="">选择证据</option>
						<option v-for="item in checks" :key="item.check_id" :value="item.check_id">
							{{ statusLabel(item.status) }} · {{ new Date(item.created_at).toLocaleString() }} · 规则 {{ item.collector_revision }} / 样例
							{{ item.sample_revision }}
						</option>
					</select></label
				>
			</div>
		</template>
		<section v-if="evidence">
			<h4>检查：{{ statusLabel(evidence.status) }}</h4>
			<p v-if="evidenceStale" class="status-warn">这是历史或过期证据，规则/样例已变化，需重新检查。</p>
			<p class="muted">
				{{ evidence.interpreter_version }} · 规则 revision {{ evidence.collector_revision }} · 样例 revision {{ evidence.sample_revision }} · 输入哈希
				{{ evidence.content_hash }}
			</p>
			<p v-if="evidence.status === 'unconfigured'" class="status-warn">尚未设置断言；本次只保存提取诊断，没有判定样例通过。</p>
			<p v-if="evidence.error_code" class="app-error">
				{{ evidence.error_code }} · {{ evidence.error_message || '步骤或提取规则无法执行，请检查保存定义。' }}
			</p>
			<ul>
				<li v-for="(diff, index) in evidence.comparison?.differences || []" :key="index">
					<button :disabled="evidenceStale || !diff.field_key" @click="focus(diff.field_key)">
						{{ differenceLabel(diff.code) }}{{ diff.record_index !== undefined ? ` · 第 ${diff.record_index + 1} 条` : ''
						}}{{ diff.field_key ? ` · ${diff.field_key}` : '' }}
					</button>
					<p>预期 {{ diff.expected_json || '—' }} / 实际 {{ diff.actual_json || '—' }}</p>
				</li>
			</ul>
			<div v-if="evidence.result" class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th>记录</th>
							<th>字段</th>
							<th>原值</th>
							<th>输出</th>
							<th>诊断</th>
						</tr>
					</thead>
					<tbody>
						<template v-for="row in evidence.result.records" :key="row.index"
							><tr v-for="field in row.fields" :key="field.field_key">
								<td>{{ row.index + 1 }}</td>
								<td>
									<button :disabled="evidenceStale" @click="focus(field.field_key)">{{ field.name }}</button>
								</td>
								<td>
									<pre>{{ field.raw_json }}</pre>
								</td>
								<td>
									<pre>{{ field.value_json }}</pre>
								</td>
								<td :class="field.valid ? 'status-good' : 'app-error'">{{ field.errors.join('；') || '通过' }}</td>
							</tr></template
						>
					</tbody>
				</table>
			</div>
			<details>
				<summary>本次预期与完整元数据</summary>
				<pre>{{ evidence.expected_json }}</pre>
				<pre>{{ evidenceMetadata }}</pre>
				<pre>{{ evidence.definition_json }}</pre>
			</details>
		</section>
	</section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import {
	appApi,
	type Collector,
	type InputCapture,
	type BrowserSession,
	type ExtractionPreview,
	type SavedSample,
	type SampleCheck,
	type SampleMask,
} from './api';
const props = defineProps<{
	draft: Collector;
	capture?: InputCapture;
	session?: BrowserSession;
	stepId: string;
	stage: 'list' | 'detail';
	blocked: boolean;
	preview?: ExtractionPreview;
	previewStale: boolean;
}>();
const emit = defineEmits<{
	(event: 'use-sample', sample: SavedSample): void;
	(event: 'focus-field', stepId: string, key: string, role: string): void;
}>();
const items = ref<SavedSample[]>([]),
	nextCursor = ref(''),
	selected = ref<SavedSample>(),
	checks = ref<SampleCheck[]>([]),
	evidence = ref<SampleCheck>(),
	busy = ref(false),
	message = ref('');
const name = ref(''),
	kind = ref<'normal' | 'missing_field'>('normal'),
	protectedSample = ref(false),
	expectedText = ref('{}'),
	includeScreenshot = ref(false),
	reviewed = ref(false),
	masks = ref<SampleMask[]>([]),
	frameURL = ref(''),
	savedImageURL = ref(''),
	imageLoading = ref(false),
	pendingKey = ref('');
let disposed = false,
	epoch = 0,
	imageEpoch = 0;
let baseline = '';
const localState = () => JSON.stringify([name.value, expectedText.value, protectedSample.value]);
const dirty = computed(() =>
	selected.value ? localState() !== baseline : !!name.value || expectedText.value.trim() !== '{}' || includeScreenshot.value
);
const evidenceMetadata = computed(() =>
	JSON.stringify(
		{
			check_id: evidence.value?.check_id,
			definition_hash: evidence.value?.definition_hash,
			expected_hash: evidence.value?.expected_hash,
			content_hash: evidence.value?.content_hash,
		},
		null,
		2
	)
);
const evidenceStale = computed(
	() =>
		!evidence.value ||
		props.blocked ||
		dirty.value ||
		evidence.value.collector_revision !== props.draft.revision ||
		evidence.value.sample_revision !== selected.value?.revision ||
		evidence.value.expected_hash !== selected.value?.expected_hash
);
const canScreenshot = computed(
	() =>
		props.capture?.source === 'browser' &&
		props.capture.session_id === props.session?.session_id &&
		props.capture.page_state_id === props.session?.page_state_id &&
		props.session?.available
);
const storageKey = () => `scrapio:v22:sample-pending:${props.draft.id}`;
const temporaryMask = ref<SampleMask>();
const shownMasks = computed(() => [...masks.value, ...(temporaryMask.value ? [temporaryMask.value] : [])]);
let start: { x: number; y: number } | undefined;
function maskStyle(m: SampleMask) {
	return { left: `${m.x * 100}%`, top: `${m.y * 100}%`, width: `${m.width * 100}%`, height: `${m.height * 100}%` };
}
function point(e: PointerEvent) {
	const b = (e.currentTarget as HTMLElement).getBoundingClientRect();
	return { x: Math.max(0, Math.min(1, (e.clientX - b.left) / b.width)), y: Math.max(0, Math.min(1, (e.clientY - b.top) / b.height)) };
}
function startMask(e: PointerEvent) {
	if (busy.value || pendingKey.value || masks.value.length >= 50) return;
	start = point(e);
	temporaryMask.value = undefined;
	(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	reviewed.value = false;
}
function moveMask(e: PointerEvent) {
	if (!start) return;
	const end = point(e);
	temporaryMask.value = {
		x: Math.min(start.x, end.x),
		y: Math.min(start.y, end.y),
		width: Math.abs(end.x - start.x),
		height: Math.abs(end.y - start.y),
	};
}
function finishMask(e: PointerEvent) {
	moveMask(e);
	if (temporaryMask.value && temporaryMask.value.width > 0.001 && temporaryMask.value.height > 0.001) masks.value.push(temporaryMask.value);
	cancelMask();
}
function cancelMask() {
	start = undefined;
	temporaryMask.value = undefined;
}
function resetImages() {
	imageEpoch++;
	for (const r of [frameURL, savedImageURL]) {
		if (r.value) URL.revokeObjectURL(r.value);
		r.value = '';
	}
	masks.value = [];
	reviewed.value = false;
	imageLoading.value = false;
}
function errorMessage(cause: any) {
	return cause?.error?.message || cause?.message || '请求失败，本地编辑已保留';
}
function statusLabel(status: string) {
	return { passed: '通过', failed: '失败', unconfigured: '未判定' }[status] || status;
}
function differenceLabel(code: string) {
	return (
		(
			{
				VALUE_MISMATCH: '输出值不符',
				ERRORS_MISMATCH: '错误码不符',
				UNEXPECTED_FIELD_ERROR: '未预期的字段错误',
				FIELD_NOT_FOUND: '字段已不存在',
				RECORD_NOT_PREVIEWED: '记录未覆盖',
				RECORD_COUNT_MISMATCH: '记录数不符',
				VALID_COUNT_MISMATCH: '有效数不符',
				INVALID_COUNT_MISMATCH: '无效数不符',
				COUNT_INCOMPLETE: '范围计数不完整',
				PREVIEW_INCOMPLETE: '预览覆盖被截取',
				NO_FIELDS: '没有字段',
				EXTRACTION_FAILED: '提取失败',
			} as Record<string, string>
		)[code] || code
	);
}
function confirmDiscard() {
	return !dirty.value || window.confirm('样例编辑尚未保存，继续会丢弃这些修改。确认？');
}
function setSelected(row?: SavedSample) {
	selected.value = row;
	name.value = row?.name || '';
	kind.value = row?.kind || 'normal';
	protectedSample.value = !!row?.protected;
	expectedText.value = row?.expected_json || '{}';
	includeScreenshot.value = false;
	baseline = localState();
	checks.value = [];
	evidence.value = undefined;
	resetImages();
}
async function loadList(more = false) {
	if (busy.value) return;
	busy.value = true;
	try {
		const page = await appApi.samples(props.draft.id, more ? nextCursor.value : '');
		if (!disposed) {
			items.value = more ? [...items.value, ...page.items] : page.items;
			nextCursor.value = page.next_cursor;
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function selectSample(id: string) {
	if (busy.value || !confirmDiscard()) return;
	const token = ++epoch;
	busy.value = true;
	try {
		if (!id) {
			setSelected();
			return;
		}
		const row = await appApi.sample(id);
		const history = await appApi.sampleChecks(id);
		if (!disposed && token === epoch) {
			setSelected(row);
			checks.value = history.items;
			if (history.items[0]) evidence.value = await appApi.sampleCheck(id, history.items[0].check_id);
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function reloadSelected() {
	if (selected.value) await selectSample(selected.value.sample_id);
}
function validateExpected() {
	const value = JSON.parse(expectedText.value);
	if (!value || Array.isArray(value) || typeof value !== 'object') throw Error('预期结果需要 JSON 对象');
	if (new TextEncoder().encode(expectedText.value).length > 65536) throw Error('预期结果超出 64 KiB');
}
function fillExpected() {
	if (!props.preview || props.previewStale) return;
	const result = props.preview.result;
	const row = result.records[0];
	const fields = (row?.fields || []).map(
		(f) => `{"record_index":0,"field_key":${JSON.stringify(f.field_key)},"value":${f.value_json},"errors":${JSON.stringify(f.errors)}}`
	);
	expectedText.value = `{\n${result.truncated ? '' : `  "record_count":${result.match_count},\n`}  "valid_count":${result.valid_count},\n  "invalid_count":${result.invalid_count},\n  "fields":[${fields.join(',\n')}]\n}`;
	message.value = '已填入首条记录及计数的当前结果，请核对它是否就是期望；尚未保存。';
}
async function loadFrame() {
	if (!canScreenshot.value || !props.session || !props.capture) return;
	imageLoading.value = true;
	const token = ++imageEpoch;
	const page = props.capture.page_state_id;
	try {
		const blob = await appApi.browserFrame(props.session.frame_url, page);
		if (!disposed && token === imageEpoch && props.capture?.page_state_id === page) {
			if (frameURL.value) URL.revokeObjectURL(frameURL.value);
			frameURL.value = URL.createObjectURL(blob);
			reviewed.value = false;
			masks.value = [];
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		if (token === imageEpoch) imageLoading.value = false;
	}
}
async function loadSavedImage() {
	if (!selected.value?.screenshot_url) return;
	const token = ++imageEpoch;
	imageLoading.value = true;
	try {
		const blob = await appApi.sampleScreenshot(selected.value.screenshot_url);
		if (!disposed && token === imageEpoch) {
			if (savedImageURL.value) URL.revokeObjectURL(savedImageURL.value);
			savedImageURL.value = URL.createObjectURL(blob);
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		if (token === imageEpoch) imageLoading.value = false;
	}
}
async function create() {
	if (
		busy.value ||
		props.blocked ||
		pendingKey.value ||
		!props.capture ||
		!props.stepId ||
		(includeScreenshot.value && (!reviewed.value || !frameURL.value || !canScreenshot.value))
	)
		return;
	busy.value = true;
	message.value = '';
	try {
		validateExpected();
		const key = crypto.randomUUID();
		pendingKey.value = key;
		sessionStorage.setItem(storageKey(), key);
		const row = await appApi.createSample(
			props.draft.id,
			{
				name: name.value,
				capture_id: props.capture.capture_id,
				expected_revision: props.draft.revision,
				step_id: props.stepId,
				stage: props.stage,
				kind: kind.value,
				expected_json: expectedText.value,
				protected: protectedSample.value,
				include_screenshot: includeScreenshot.value,
				screenshot_reviewed: includeScreenshot.value && reviewed.value,
				masks: includeScreenshot.value ? masks.value : [],
			},
			key
		);
		if (!disposed) {
			clearPending();
			setSelected(row);
			items.value = [row, ...items.value.filter((i) => i.sample_id !== row.sample_id)];
			message.value = '样例已保存，输入快照随样例保留。';
		}
	} catch (cause: any) {
		if (cause?.error && cause.error.code !== 'INTERNAL') clearPending();
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function clearPending() {
	pendingKey.value = '';
	sessionStorage.removeItem(storageKey());
}
async function recover() {
	if (busy.value || !pendingKey.value) return;
	busy.value = true;
	try {
		const row = await appApi.sampleByKey(pendingKey.value);
		if (row.collector_id !== props.draft.id) throw Error('原请求属于其他方案');
		if (!disposed) {
			clearPending();
			setSelected(row);
			items.value = [row, ...items.value.filter((i) => i.sample_id !== row.sample_id)];
			message.value = '已恢复原请求的样例，不重新截图或取样。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function endWaiting() {
	if (window.confirm('只结束本地等待，不撤销服务端保存。请先检查样例列表避免重复创建，确认？')) clearPending();
}
async function save() {
	if (!selected.value || busy.value || props.blocked) return;
	busy.value = true;
	try {
		validateExpected();
		const row = await appApi.updateSample(selected.value.sample_id, {
			expected_revision: selected.value.revision,
			name: name.value,
			expected_json: expectedText.value,
			protected: protectedSample.value,
		});
		if (!disposed) {
			const oldChecks = checks.value;
			const oldEvidence = evidence.value;
			setSelected({ ...row, capture: selected.value?.capture });
			checks.value = oldChecks;
			evidence.value = oldEvidence;
			items.value = items.value.map((i) => (i.sample_id === row.sample_id ? row : i));
			message.value = '样例已保存；历史证据保留，需重新验证。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function check() {
	if (!selected.value || busy.value || props.blocked || dirty.value) return;
	busy.value = true;
	const id = selected.value.sample_id;
	try {
		const row = await appApi.checkSample(id, props.draft.revision, selected.value.revision, crypto.randomUUID());
		if (!disposed && selected.value?.sample_id === id) {
			evidence.value = row;
			checks.value = (await appApi.sampleChecks(id)).items;
			message.value = '检查证据已保存；页面动作未重放，正式数据未写入。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function loadCheck(id: string) {
	if (!selected.value || !id || busy.value) return;
	busy.value = true;
	try {
		const row = await appApi.sampleCheck(selected.value.sample_id, id);
		if (!disposed) evidence.value = row;
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function useInput() {
	if (!selected.value || busy.value || dirty.value) return;
	busy.value = true;
	try {
		const row = await appApi.sample(selected.value.sample_id);
		if (!disposed) {
			emit('use-sample', row);
			message.value = '样例固定输入已带入预览，不重新访问网站。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function remove() {
	if (
		!selected.value ||
		busy.value ||
		dirty.value ||
		selected.value.protected ||
		!window.confirm('删除样例会一并删除该样例的检查证据，原输入快照日志仍保留。确认删除？')
	)
		return;
	busy.value = true;
	try {
		const id = selected.value.sample_id;
		await appApi.deleteSample(id, selected.value.revision);
		if (!disposed) {
			items.value = items.value.filter((i) => i.sample_id !== id);
			setSelected();
			message.value = '样例及检查证据已删除，原快照未删除。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function focus(key?: string) {
	if (key && selected.value && !evidenceStale.value) emit('focus-field', selected.value.step_id, key, selected.value.stage);
}
watch(
	() => props.capture?.capture_id,
	() => {
		if (!selected.value) {
			includeScreenshot.value = false;
			resetImages();
		}
	}
);
watch(
	() => [props.session?.page_state_id, props.session?.session_id],
	() => {
		resetImages();
	}
);
watch(canScreenshot, (valid) => {
	if (!valid) {
		includeScreenshot.value = false;
		resetImages();
	}
});
onMounted(() => {
	pendingKey.value = sessionStorage.getItem(storageKey()) || '';
	void loadList(false);
});
onBeforeUnmount(() => {
	disposed = true;
	epoch++;
	resetImages();
});
defineExpose({ hasUnsavedChanges: () => dirty.value || !!pendingKey.value || busy.value });
</script>

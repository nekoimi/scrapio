<template>
	<section id="repair" class="app-card">
		<div class="pane-heading">
			<h2>修复方案</h2>
			<button :disabled="busy || !!pending" @click="load">刷新修复上下文</button>
		</div>
		<p>带入这次失败的原版本、步骤和保存输入。这里只准备离线修复，不重跑网站动作，不发布或写入正式记录。</p>
		<p v-if="busy" role="status">正在处理…</p>
		<p v-if="message" class="app-error" role="alert">{{ message }}</p>
		<template v-if="context && !result">
			<p>
				原运行 v{{ context.version_number }} · {{ context.failed_step_id || '尚无具体失败步骤' }} / {{ context.failed_stage }} · {{ context.reason }}
			</p>
			<p v-if="documentId" class="muted">已选择页面证据；使用这张页面，不自动改成其他输入。</p>
			<p v-if="context.document_id">
				带入输入：{{ context.step_id }} / {{ context.stage }} · 来源文档：<router-link :to="`/app/pages/${context.document_id}`"
					>查看页面与字段证据</router-link
				>
			</p>
			<p :class="context.evidence_status === 'available' ? 'status-good' : 'status-warn'">{{ evidenceText(context.evidence_status) }}</p>
			<p v-if="context.field_keys.length">需核对字段：{{ context.field_keys.join('、') }}</p>
			<p class="muted">
				当前草稿 revision {{ context.target_revision }} ·
				{{ context.pending_draft ? '有未发布草稿，继续修复会保留它' : '当前无未发布修改' }}。来源方案当前发布：{{
					context.current_published_version_number ? `v${context.current_published_version_number}` : '尚未发布'
				}}；本次失败仍使用原运行版本。
			</p>
			<fieldset :disabled="busy || !!pending">
				<legend>修复方式</legend>
				<label
					><input v-model="mode" type="radio" value="continue" :disabled="context.archived" />继续当前草稿：保留全部现有配置，只关联失败输入</label
				>
				<label><input v-model="mode" type="radio" value="fork" />从失败版本另建草稿：复制原规则，重新确认输出</label>
			</fieldset>
			<p v-if="mode === 'continue' && !context.step_compatible" class="status-warn">
				当前草稿不再有匹配的提取步骤/角色，输入可能无法直接预览；可另建原版本草稿。不会恢复或覆盖当前规则。
			</p>
			<p v-if="context.archived" class="status-warn">来源方案已归档，只能另建修复草稿；原调度保持原样。</p>
			<label><input v-model="confirmed" type="checkbox" :disabled="busy || !!pending" />确认修复方式和保存输入，原运行及现有草稿不会被覆盖</label>
			<button :disabled="busy || !!pending || !confirmed || (mode === 'continue' && context.archived)" @click="start">准备修复草稿</button>
		</template>
		<div v-if="pending" class="editor-actions">
			<button :disabled="busy" @click="recover">查询原修复请求</button><button :disabled="busy" @click="discard">结束本地等待</button>
		</div>
		<div v-if="result">
			<p class="status-good">修复上下文已保存；{{ result.mode === 'fork' ? '已另建草稿，需要重新确认输出' : '当前草稿配置保持原样' }}。</p>
			<router-link :to="{ path: `/app/collectors/${result.target_collector_id}`, query: { repair_id: result.repair_id } }">进入修复草稿</router-link>
		</div>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { appApi, type FormalRun, type RepairContext } from './api';
import { readError } from './data-read';
import { evidenceText } from './repair-read';
const props = defineProps<{ run: FormalRun; documentId?: string }>();
const context = ref<RepairContext>(),
	result = ref<RepairContext>(),
	mode = ref<'continue' | 'fork'>('continue'),
	confirmed = ref(false),
	busy = ref(false),
	message = ref(''),
	pending = ref('');
let epoch = 0;
const storage = (id = props.run.run_id) => `scrapio:repair:${id}`;
function clear(id = props.run.run_id) {
	pending.value = '';
	sessionStorage.removeItem(storage(id));
}
async function load() {
	const token = ++epoch;
	busy.value = true;
	message.value = '';
	context.value = undefined;
	confirmed.value = false;
	try {
		const h = await appApi.prepareRepair(props.run.run_id, props.documentId || '');
		if (token === epoch) {
			context.value = h;
			if (h.archived) mode.value = 'fork';
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function start() {
	if (!context.value || busy.value || pending.value || !confirmed.value) return;
	const h = context.value,
		token = epoch,
		id = props.run.run_id,
		key = crypto.randomUUID();
	busy.value = true;
	message.value = '';
	try {
		sessionStorage.setItem(storage(id), key);
		pending.value = key;
		const value = await appApi.createRepair(
			h.source_collector_id,
			{ run_id: id, document_id: h.document_id, mode: mode.value, expected_revision: h.target_revision, confirmed: true },
			key
		);
		if (token !== epoch) return;
		result.value = value;
		clear(id);
	} catch (e: any) {
		if (token !== epoch) return;
		message.value = readError(e) + '；结果未知时先查询原请求。';
		if (e?.error && ['INVALID_ARGUMENT', 'NOT_FOUND', 'REPAIR_CONFLICT', 'REPAIR_CAPACITY'].includes(e.error.code)) clear(id);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function recover() {
	if (busy.value || !pending.value) return;
	const token = epoch,
		id = props.run.run_id;
	busy.value = true;
	message.value = '';
	try {
		const value = await appApi.repairByKey(pending.value);
		if (token !== epoch) return;
		if (value.run_id !== id) throw Error('修复上下文不属于当前运行');
		result.value = value;
		clear(id);
	} catch (e) {
		if (token === epoch) message.value = readError(e) + '；未查到时保留请求键，稍后再查。';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function discard() {
	if (window.confirm('结束等待不会删除服务端修复草稿。请先查询原请求，确认结束本地等待？')) clear();
}
function unload(e: BeforeUnloadEvent) {
	if (busy.value || pending.value) {
		e.preventDefault();
		e.returnValue = '';
	}
}
onBeforeRouteLeave(
	() => (!busy.value && !pending.value) || window.confirm('存在未确认的修复请求，离开不会撤销服务端草稿；返回可查询原请求。确认离开？')
);
watch(
	() => [props.run.run_id, props.documentId],
	() => {
		epoch++;
		busy.value = false;
		result.value = undefined;
		mode.value = 'continue';
		pending.value = sessionStorage.getItem(storage()) || '';
		void load();
	},
	{ immediate: true }
);
watch(mode, () => {
	confirmed.value = false;
});
onMounted(() => window.addEventListener('beforeunload', unload));
onBeforeUnmount(() => {
	epoch++;
	window.removeEventListener('beforeunload', unload);
});
</script>

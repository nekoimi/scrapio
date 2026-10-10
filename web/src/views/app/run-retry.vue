<template>
	<section class="app-card">
		<h2>按原版本重新运行</h2>
		<p v-if="run.retry_of">
			本次来源于 <router-link :to="`/app/runs/${run.retry_of}`">原运行 {{ run.retry_of.slice(0, 8) }}</router-link> · {{ run.retry_scope }}
		</p>
		<template v-if="run.controls?.can_retry">
			<p>从原入口完整重新采集，固定发布 v{{ run.version_number }}。复用下列来源和预算，不使用当前草稿或上次文档。</p>
			<pre>{{ JSON.stringify(run.input, null, 2) }}</pre>
			<p class="status-warn">
				会新建运行和独立浏览器会话，并重复入口请求、点击、输入及分页等动作。成功后按原输出策略写正式数据及新的来源观察；原运行保留。这不是只重试失败页面，也不会续跑旧
				attempt。
			</p>
			<label><input v-model="confirmed" type="checkbox" :disabled="busy || !!pending" />确认原版本、原范围/预算、重复网站动作及正式数据写入</label>
			<button :disabled="busy || !confirmed || !!pending" @click="start">完整重跑 v{{ run.version_number }}</button>
			<p class="muted">提交时及执行前仍校验能力、版本和目标 Schema，不兼容时明确阻断；修复规则需要另行发布。</p>
		</template>
		<p v-else class="muted">原运行结束后才能重新运行。当前不支持仅失败页面重试或自动恢复未知动作。</p>
		<div v-if="pending" class="editor-actions">
			<button :disabled="busy" @click="recover">查询原重试请求</button><button :disabled="busy" @click="discard">结束本地等待</button>
		</div>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<router-link v-if="result" :to="`/app/runs/${result.run_id}`"
			>查看新运行 {{ result.run_id.slice(0, 8) }} · {{ runStatus(result.status) }}</router-link
		>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount, onMounted } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { appApi, type FormalRun } from './api';
import { readError, runStatus } from './data-read';
const props = defineProps<{ run: FormalRun }>();
const confirmed = ref(false),
	busy = ref(false),
	pending = ref(''),
	message = ref(''),
	result = ref<FormalRun>();
let epoch = 0;
const storage = (id = props.run.run_id) => `scrapio:run-retry:${id}`;
function clear(id = props.run.run_id) {
	pending.value = '';
	sessionStorage.removeItem(storage(id));
}
async function start() {
	if (busy.value || pending.value || !confirmed.value || !props.run.controls?.can_retry) return;
	const token = epoch,
		id = props.run.run_id,
		key = crypto.randomUUID();
	busy.value = true;
	message.value = '';
	pending.value = key;
	try {
		sessionStorage.setItem(storage(id), key);
		const r = await appApi.retryRun(id, key);
		if (token !== epoch) return;
		result.value = r;
		clear(id);
		confirmed.value = false;
	} catch (e: any) {
		if (token !== epoch) return;
		message.value = readError(e) + '；请求结果未知时先查询原请求。';
		if (e?.error && ['INVALID_ARGUMENT', 'NOT_FOUND', 'RUN_CONFLICT', 'RUN_CAPACITY'].includes(e.error.code)) clear(id);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function recover() {
	if (busy.value || !pending.value) return;
	const token = epoch,
		id = props.run.run_id;
	busy.value = true;
	try {
		const r = await appApi.runByKey(pending.value);
		if (token !== epoch) return;
		if (r.retry_of !== id || r.retry_scope !== 'full_run') throw Error('请求不属于当前重试');
		result.value = r;
		clear(id);
		message.value = '已恢复原重试，没有创建新运行。';
	} catch (e) {
		if (token === epoch) message.value = readError(e) + '；未查到时保留请求键，稍后再查。';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function discard() {
	if (window.confirm('结束本地等待不会取消服务器运行。请先查询原请求并检查运行列表，确认结束？')) clear();
}
function unload(e: BeforeUnloadEvent) {
	if (busy.value || pending.value) {
		e.preventDefault();
		e.returnValue = '';
	}
}
onBeforeRouteLeave(() => (!busy.value && !pending.value) || window.confirm('有未确认的重试请求；离开后可返回查询，离开不会撤销运行。'));
onMounted(() => window.addEventListener('beforeunload', unload));
watch(
	() => props.run.run_id,
	() => {
		epoch++;
		busy.value = false;
		confirmed.value = false;
		result.value = undefined;
		pending.value = sessionStorage.getItem(storage()) || '';
		message.value = pending.value ? '有未确认的重试请求，请先查询。' : '';
	},
	{ immediate: true }
);
onBeforeUnmount(() => {
	epoch++;
	window.removeEventListener('beforeunload', unload);
});
</script>

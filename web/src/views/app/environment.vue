<template>
	<div class="app-page">
		<h1>浏览器连接</h1>
		<p class="lead">网页点选与动作录制需要浏览器编辑服务；JSON 入口、已有数据和运行记录可独立使用。</p>
		<section class="app-card">
			<div class="pane-heading">
				<h2>编辑服务状态</h2>
				<button :disabled="loading" @click="load">{{ loading ? '检查中…' : '重新检查' }}</button>
			</div>
			<p v-if="loading" role="status">正在检查连接…</p>
			<p v-if="error" class="app-error" role="alert">无法确认当前连接：{{ error }}</p>
			<template v-else-if="capabilities"
				><p :class="capabilities.editor_service_connected ? 'status-good' : 'status-warn'">
					{{ capabilities.editor_service_connected ? '浏览器编辑服务已连接' : '浏览器编辑服务未连接' }}
				</p>
				<p class="muted">{{ capabilities.reason }}</p>
				<p v-if="!capabilities.editor_service_connected">请检查部署中的浏览器服务和连接配置，修复后重新检查。</p></template
			>
		</section>
	</div>
</template>
<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { appApi, type Capabilities } from './api';
import { readError } from './data-read';
const capabilities = ref<Capabilities>(),
	loading = ref(false),
	error = ref('');
let epoch = 0;
async function load() {
	const token = ++epoch;
	loading.value = true;
	error.value = '';
	capabilities.value = undefined;
	try {
		const value = await appApi.capabilities();
		if (token === epoch) capabilities.value = value;
	} catch (cause) {
		if (token === epoch) error.value = readError(cause);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
onMounted(() => void load());
onBeforeUnmount(() => epoch++);
</script>

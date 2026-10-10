<template>
	<section class="app-card">
		<div class="editor-actions">
			<strong>运行计划</strong><router-link :to="`/app/collectors/${collectorId}/schedule`">调度与 API</router-link
			><button :disabled="loading" @click="load">刷新</button>
		</div>
		<p v-if="loading" role="status">正在读取计划…</p>
		<p v-else-if="message" class="app-error">{{ message }}</p>
		<p v-else-if="value">
			{{ value.enabled ? '已启用' : '已暂停' }} · 固定版本 {{ value.input.version_id.slice(0, 8) }} · {{ value.timezone }} · 下次运行：{{
				value.next_at ? new Date(value.next_at).toLocaleString() : '无'
			}}（按当前设备时区显示）
		</p>
		<p v-else class="muted">尚未配置定时计划。发布方案后可设置定时或创建专用 API 凭据。</p>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type CollectorSchedule } from './api';
import { readError } from './data-read';
const props = defineProps<{ collectorId: string }>();
const value = ref<CollectorSchedule | null>(null),
	loading = ref(false),
	message = ref('');
let epoch = 0;
async function load() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	try {
		const result = await appApi.schedule(props.collectorId);
		if (token === epoch) value.value = result;
	} catch (cause) {
		if (token === epoch) message.value = readError(cause);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
watch(() => props.collectorId, load, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>

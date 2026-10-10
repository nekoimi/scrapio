<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>方案运行健康</h2>
			<button :disabled="loading" @click="load">刷新健康</button>
		</div>
		<p v-if="loading" role="status">正在读取实际运行结果…</p>
		<p v-if="message" class="app-error">{{ message }}；不能据此判断方案没有异常。</p>
		<HealthCard v-if="health" :health="health" detailed />
	</section>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type CollectorHealth } from './api';
import HealthCard from './health-card.vue';
import { readError } from './data-read';
const props = defineProps<{ collectorId: string }>(),
	health = ref<CollectorHealth>(),
	loading = ref(false),
	message = ref('');
let epoch = 0;
async function load() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	health.value = undefined;
	try {
		const h = await appApi.collectorHealth(props.collectorId);
		if (token === epoch) health.value = h;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
watch(() => props.collectorId, load, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>

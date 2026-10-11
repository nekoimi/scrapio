<template>
	<form class="collector-starter" @submit.prevent="start">
		<fieldset :disabled="creating">
			<label :for="inputId">目标网址</label>
			<div class="url-row">
				<input :id="inputId" v-model="entryUrl" type="url" required pattern="https?://.+" placeholder="https://example.com/list" />
				<select v-model="entryType" aria-label="入口类型">
					<option value="web">网页</option>
					<option value="json">JSON 接口</option>
				</select>
				<button type="submit" :disabled="!entryUrl.trim()">{{ creating ? '创建中…' : '开始采集' }}</button>
			</div>
		</fieldset>
		<p class="starter-help">创建方案后进入编辑器配置内容；JSON 接口可直接提取数据。</p>
		<p v-if="error" class="app-error" role="alert">{{ error }}</p>
	</form>
</template>
<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue';
import { useRouter } from 'vue-router';
import { appApi } from './api';
import { readError } from './data-read';
defineProps<{ inputId: string }>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const router = useRouter();
const entryUrl = ref(''),
	entryType = ref<'web' | 'json'>('web'),
	creating = ref(false),
	error = ref('');
let disposed = false;
async function start() {
	if (!entryUrl.value.trim() || creating.value) return;
	creating.value = true;
	emit('busy', true);
	error.value = '';
	try {
		const collector = await appApi.createCollector({ entry_url: entryUrl.value.trim(), entry_type: entryType.value });
		if (!disposed) await router.push(`/app/collectors/${collector.id}`);
	} catch (cause) {
		if (!disposed) error.value = readError(cause);
	} finally {
		creating.value = false;
		if (!disposed) emit('busy', false);
	}
}
onBeforeUnmount(() => (disposed = true));
</script>
<style scoped>
.collector-starter fieldset {
	min-width: 0;
	margin: 0;
	padding: 0;
	border: 0;
}
.collector-starter label {
	display: block;
	margin-bottom: 8px;
	font-size: 13px;
	font-weight: 600;
}
.starter-help {
	margin: 12px 0 0;
	font-size: 13px;
	color: var(--el-text-color-secondary, #909399);
	line-height: 1.6;
}
.collector-starter .app-error {
	margin-top: 12px;
}
</style>

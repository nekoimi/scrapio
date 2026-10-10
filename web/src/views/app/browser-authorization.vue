<template>
	<section class="app-card">
		<h2>浏览器授权与隐私</h2>
		<CredentialPicker v-model="selected" kind="browser_cookie" :target="draft.entry_url" :disabled="blocked || busy || sessionActive" />
		<p class="muted">
			Cookie
			在独立上下文中注入，不录制密码或验证码。保存授权引用后，手动打开浏览器并检查是否进入已登录页面；现有会话须先关闭。跨目标资源可能被阻止。
		</p>
		<p v-if="selected !== original" class="status-warn">授权引用未保存；取样和正式运行使用已保存/已发布的引用。</p>
		<button :disabled="blocked || busy || sessionActive || selected === original" @click="save">保存授权引用</button>
		<p v-if="error" class="app-error" role="status">{{ error }}</p>
	</section>
</template>
<script setup lang="ts">
import { ref, watch, computed } from 'vue';
import { appApi, type Collector } from './api';
import CredentialPicker from './credential-picker.vue';
const props = defineProps<{ draft: Collector; blocked: boolean; sessionActive: boolean }>();
const emit = defineEmits<{ (e: 'draft-saved', v: Collector): void }>();
const selected = ref(''),
	busy = ref(false),
	error = ref('');
const original = computed(() => props.draft.definition.browser_auth?.credential_ref || '');
watch(original, (v) => (selected.value = v), { immediate: true });
async function save() {
	busy.value = true;
	error.value = '';
	const id = props.draft.id;
	const definition = { ...props.draft.definition };
	if (selected.value) definition.browser_auth = { credential_ref: selected.value };
	else delete definition.browser_auth;
	try {
		const v = await appApi.updateCollector(id, { name: props.draft.name, expected_revision: props.draft.revision, definition });
		if (id === props.draft.id) emit('draft-saved', v);
	} catch (e: any) {
		error.value =
			(e?.response?.data?.error?.message || e?.error?.message || e?.message || '保存未确认') +
			'；本地选择保留，请读取草稿核对，勿重复提交。';
	} finally {
		busy.value = false;
	}
}
defineExpose({ hasUnsavedChanges: () => selected.value !== original.value || busy.value });
</script>

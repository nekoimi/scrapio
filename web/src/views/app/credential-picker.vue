<template>
	<div>
		<label
			>已管理的凭据<select
				:value="modelValue"
				:disabled="disabled || busy"
				@change="$emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
			>
				<option value="">不使用凭据</option>
				<option v-if="modelValue && !eligible.some((c) => c.credential_ref === modelValue)" :value="modelValue">
					当前引用 {{ modelValue }}（未匹配、失效或部署引用）
				</option>
				<option v-for="c in eligible" :key="c.credential_id" :value="c.credential_ref">{{ c.name }} · revision {{ c.revision }}</option>
			</select></label
		>
		<div class="editor-actions">
			<button :disabled="busy" @click="load">刷新凭据</button
			><button :disabled="busy || !modelValue.startsWith('${credential:')" @click="check">检查作用域与运行环境</button
			><router-link to="/app/settings">管理凭据</router-link>
		</div>
		<p v-if="message" class="muted" role="status">{{ message }}</p>
		<p class="muted">检查不访问目标、不证明登录成功；保存引用后打开浏览器或实时取样核对授权。</p>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { appApi, type Credential } from './api';
const props = defineProps<{ modelValue: string; kind: 'http_header' | 'browser_cookie'; target: string; disabled?: boolean }>();
defineEmits<{ (e: 'update:modelValue', v: string): void }>();
const items = ref<Credential[]>([]),
	busy = ref(false),
	message = ref('');
let epoch = 0;
const origin = computed(() => {
	try {
		return new URL(props.target).origin;
	} catch {
		return '';
	}
});
const eligible = computed(() => items.value.filter((c) => c.kind === props.kind && c.origin === origin.value && c.status === 'available'));
async function load() {
	const token = ++epoch;
	busy.value = true;
	message.value = '';
	try {
		const v = await appApi.credentials();
		if (token === epoch) items.value = v.items;
	} catch (e: any) {
		if (token === epoch) message.value = e?.response?.data?.error?.message || e?.error?.message || e?.message || '凭据列表不可用';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function check() {
	const match = /^\$\{credential:([0-9a-f-]{36})\}$/.exec(props.modelValue);
	if (!match || busy.value) return;
	const token = ++epoch;
	busy.value = true;
	try {
		const v = await appApi.checkCredential(match[1], props.target, props.kind);
		if (token === epoch)
			message.value = v.runtime_ready
				? `revision ${v.revision} 的作用域与运行环境可用，网站授权尚未验证`
				: '浏览器授权协议不可用，请同步升级 scrapio-browser';
	} catch (e: any) {
		if (token === epoch) message.value = e?.response?.data?.error?.message || e?.error?.message || e?.message || '检查失败';
	} finally {
		if (token === epoch) busy.value = false;
	}
}
watch(
	() => [props.kind, props.target],
	() => void load(),
	{ immediate: true },
);
onBeforeUnmount(() => epoch++);
</script>

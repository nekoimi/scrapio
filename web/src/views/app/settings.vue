<template>
	<div>
		<nav class="app-page editor-actions settings-tabs" aria-label="设置分类" style="padding-bottom: 0">
			<router-link
				v-for="t in tabs"
				:key="t.key"
				:to="{ path: '/app/settings', query: { tab: t.key } }"
				:aria-current="tab === t.key ? 'page' : undefined"
				>{{ t.name }}</router-link
			>
		</nav>
		<Credentials v-if="tab === 'credentials'" />
		<Retention v-else-if="tab === 'retention'" />
		<Operations v-else-if="tab === 'operations'" />
		<section v-else class="app-page">
			<div class="eyebrow">SCRAPIO / SETTINGS / IDENTITY</div>
			<h1>身份与成员</h1>
			<p class="lead">当前数据、凭据、容量策略和操作记录按登录账号隔离。</p>
			<p v-if="error" class="app-error" role="alert">{{ error }}</p>
			<p v-if="loading" role="status">读取中…</p>
			<article v-if="identity" class="app-card">
				<h2>{{ identity.username }}</h2>
				<p>账号 ID：{{ identity.id }} · 当前工作区成员</p>
				<p class="muted">本版本采用账号独立工作区。成员仅展示当前身份；共享工作区、邀请和权限分配尚未提供。</p>
			</article>
			<button :disabled="loading" @click="load">重新读取身份</button>
		</section>
	</div>
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue';
import { useRoute } from 'vue-router';
import Credentials from './credentials.vue';
import Retention from './retention.vue';
import Operations from './operations.vue';
import { appApi, type Identity } from './api';
import { readError } from './data-read';
const route = useRoute(),
	tabs = [
		{ key: 'credentials', name: '凭据与授权' },
		{ key: 'retention', name: '容量与保留' },
		{ key: 'members', name: '身份与成员' },
		{ key: 'operations', name: '操作记录' },
	];
const tab = computed(() => (tabs.some((t) => t.key === String(route.query.tab)) ? String(route.query.tab) : 'credentials'));
const identity = ref<Identity>(),
	loading = ref(false),
	error = ref('');
let epoch = 0;
async function load() {
	const token = ++epoch;
	loading.value = true;
	error.value = '';
	try {
		const r = await appApi.me();
		if (token === epoch) identity.value = r;
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
onMounted(() => void load());
onBeforeUnmount(() => epoch++);
</script>

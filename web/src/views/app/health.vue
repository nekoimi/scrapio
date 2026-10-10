<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / COLLECTORS</div>
		<h1>方案运行健康</h1>
		<p class="lead">依据最近完成的运行和调度决定定位问题，查看数据或继续配置。</p>
		<div class="editor-actions">
			<button :disabled="loading" @click="setFilter(false)">全部方案</button
			><button :disabled="loading" @click="setFilter(true)">需要处理或核对</button><button :disabled="loading" @click="reload">刷新</button
			><router-link to="/app/collectors">方案列表</router-link>
		</div>
		<p>当前范围：{{ attention ? '需要处理或核对' : '全部未归档方案' }}。每次刷新重新读取，尚未配置质量基线，不提供推测趋势。</p>
		<p v-if="loading" role="status">正在读取…</p>
		<p v-if="message" class="app-error">{{ message }}</p>
		<section class="app-card" v-if="items.length"><HealthCard v-for="h in items" :key="h.collector_id" :health="h" detailed /></section>
		<section class="app-card" v-if="!loading && !message && !items.length">
			<h2>{{ attention ? '当前没有需处理或核对的运行结果' : '还没有未归档方案' }}</h2>
			<p>健康列表不包含试采或旧数据；需要更多配置时回到方案，排查原文与字段时打开来源运行。</p>
			<router-link to="/app/home">返回首页</router-link>
		</section>
		<button v-if="cursor" :disabled="loading" @click="load(true)">更多方案</button>
	</div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { appApi, type CollectorHealth } from './api';
import HealthCard from './health-card.vue';
import { readError } from './data-read';
const route = useRoute(),
	router = useRouter(),
	items = ref<CollectorHealth[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref(''),
	attention = ref(false);
let epoch = 0;
async function load(more = false) {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	message.value = '';
	try {
		const p = await appApi.healthList(attention.value, more ? cursor.value : '');
		if (token !== epoch) return;
		items.value = more ? [...items.value, ...p.items] : p.items;
		cursor.value = p.next_cursor;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
function reload() {
	epoch++;
	loading.value = false;
	items.value = [];
	cursor.value = '';
	void load();
}
function setFilter(value: boolean) {
	if (value === attention.value) {
		reload();
		return;
	}
	void router.replace({ query: { attention: value ? '1' : '0' } });
}
watch(
	() => route.query.attention,
	() => {
		attention.value = route.query.attention === '1';
		reload();
	},
	{ immediate: true }
);
onBeforeUnmount(() => epoch++);
</script>

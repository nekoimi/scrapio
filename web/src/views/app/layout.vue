<template>
	<div class="scrapio-app">
		<aside class="app-side">
			<router-link class="app-brand" to="/app/home"
				><span class="brand-mark">S</span><span>scrapio<small>采集工作台</small></span></router-link
			>
			<div class="nav-caption">工作区</div>
			<nav aria-label="主导航">
				<router-link v-for="item in nav" :key="item.to" :to="item.to">{{ item.label }}</router-link>
			</nav>
			<div class="side-bottom"><router-link to="/workspace/projects">打开旧版管理界面 ↗</router-link></div>
		</aside>
		<div class="app-main">
			<header class="app-top">
				<strong>{{ title }}</strong
				><span
					>{{ identity?.username || (identityLoading ? '身份读取中…' : '身份暂不可读') }}
					<button v-if="identityError" @click="loadIdentity">重试身份</button></span
				>
			</header>
			<main><router-view /></main>
		</div>
	</div>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type Identity } from './api';
import { NextLoading } from '/@/utils/loading';
const route = useRoute();
const identity = ref<Identity>();
const identityLoading = ref(false),
	identityError = ref(false);
let epoch = 0;
async function loadIdentity() {
	const token = ++epoch;
	identityLoading.value = true;
	identityError.value = false;
	identity.value = undefined;
	try {
		const value = await appApi.me();
		if (token === epoch) identity.value = value;
	} catch {
		if (token === epoch) identityError.value = true;
	} finally {
		if (token === epoch) identityLoading.value = false;
	}
}
onBeforeUnmount(() => epoch++);
const nav = [
	{ to: '/app/home', label: '首页' },
	{ to: '/app/collectors', label: '采集方案' },
	{ to: '/app/data', label: '数据' },
	{ to: '/app/runs', label: '运行' },
	{ to: '/app/issues', label: '异常待办' },
	{ to: '/app/settings', label: '设置' },
];
const title = computed(() => String(route.meta.title || nav.find((item) => route.path.startsWith(item.to))?.label || '工作区'));
onMounted(() => {
	NextLoading.done();
	void loadIdentity();
});
</script>
<style src="./style.css"></style>

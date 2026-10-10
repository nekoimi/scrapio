<template>
  <div class="scrapio-app">
    <aside class="app-side">
      <router-link class="app-brand" to="/app/home"><span class="brand-mark">S</span><span>scrapio<small>采集工作台</small></span></router-link>
      <div class="nav-caption">工作区</div>
      <nav aria-label="主导航">
        <router-link v-for="item in nav" :key="item.to" :to="item.to">{{ item.label }}</router-link>
      </nav>
      <div class="side-bottom"><router-link to="/workspace/projects">打开旧版管理界面 ↗</router-link></div>
    </aside>
    <div class="app-main">
      <header class="app-top"><strong>{{ title }}</strong><span>{{ identity?.username || '当前用户' }}</span></header>
      <main><router-view /></main>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type Identity } from './api';
import { NextLoading } from '/@/utils/loading';
const route = useRoute();
const identity = ref<Identity>();
const nav = [
  { to: '/app/home', label: '首页' },
  { to: '/app/collectors', label: '采集方案' },
  { to: '/app/data', label: '数据' },
  { to: '/app/runs', label: '运行' },
  { to: '/app/settings', label: '设置' },
];
const title = computed(() => String(route.meta.title || nav.find(item => route.path.startsWith(item.to))?.label || '工作区'));
onMounted(async () => {
  NextLoading.done();
  try { identity.value = await appApi.me(); } catch { /* 请求层处理认证；首页显示服务错误。 */ }
});
</script>
<style src="./style.css"></style>

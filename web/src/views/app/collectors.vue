<template>
  <div class="app-page"><div class="eyebrow">SCRAPIO / V2.2</div><h1>采集方案</h1><p class="lead">只展示新产品创建的方案，不读取旧 workflows 或历史记录。</p><section class="app-card" v-if="loading">正在读取方案…</section><section class="app-card" v-else-if="!items.length"><h2>还没有方案</h2><p class="muted">从首页输入一个网址开始。</p><router-link to="/app/home">开始采集</router-link></section><section class="app-card collector-list" v-else><div v-for="item in items" :key="item.id" class="collector-row"><div><strong>{{ item.name }}</strong><span>{{ item.entry_url }}</span></div><div><small>{{ item.entry_type }} · 草稿 revision {{ item.revision }}</small><router-link :to="`/app/collectors/${item.id}`">继续编辑</router-link></div></div></section><p v-if="error" class="app-error" role="alert">{{ error }}</p></div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { appApi, type Collector } from './api';
const items = ref<Collector[]>([]); const loading = ref(true); const error = ref('');
onMounted(async () => { try { items.value = (await appApi.collectors()).items; } catch { error.value = '无法读取方案列表。'; } finally { loading.value = false; } });
</script>

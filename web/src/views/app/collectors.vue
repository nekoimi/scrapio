<template>
  <div class="app-page"><div class="eyebrow">SCRAPIO / V2.2</div><h1>采集方案</h1><p class="lead">只展示新产品创建的方案，不读取旧 workflows 或历史记录。</p><div class="editor-actions"><router-link to="/app/collectors/health">方案运行健康</router-link><router-link to="/app/collectors/health?attention=1">需要处理或核对</router-link></div><section class="app-card" v-if="loading">正在读取方案…</section><section class="app-card" v-else-if="!error && !items.length"><h2>还没有方案</h2><p class="muted">从首页输入一个网址开始。</p><router-link to="/app/home">开始采集</router-link></section><section class="app-card collector-list" v-else><div v-for="item in items" :key="item.id" class="collector-row"><div><strong>{{ item.name }}</strong><span>{{ item.entry_url }}</span></div><div><small>{{ item.entry_type }} · {{ item.status === 'draft' ? '草稿' : item.status }} revision {{ item.revision }} · {{ item.published_version_id ? "已有发布版本" : "尚未发布" }}</small><router-link :to="`/app/collectors/${item.id}`">继续编辑</router-link><button class="text-action" :disabled="copyingId === item.id" @click="copy(item)">{{ copyingId === item.id ? '复制中…' : '复制为草稿' }}</button></div></div><button v-if="hasMore" class="text-action" :disabled="loadingMore" @click="loadMore">{{ loadingMore ? '加载中…' : '加载更多方案' }}</button></section><p v-if="error" class="app-error" role="alert">{{ error }}</p></div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { appApi, type Collector } from './api';
const items = ref<Collector[]>([]); const loading = ref(true); const loadingMore = ref(false); const hasMore = ref(false); const nextCursor = ref(''); const error = ref('');
const copyingId = ref(0);
async function load() { try { const page = await appApi.collectors(); items.value = page.items; hasMore.value = page.has_more; nextCursor.value = page.next_cursor || ''; } catch { error.value = '无法读取方案列表。'; } finally { loading.value = false; } }
async function loadMore() { if (!hasMore.value || loadingMore.value) return; loadingMore.value = true; error.value = ''; try { const page = await appApi.collectors(nextCursor.value); items.value.push(...page.items); hasMore.value = page.has_more; nextCursor.value = page.next_cursor || ''; } catch { error.value = '无法加载更多方案。'; } finally { loadingMore.value = false; } }
async function copy(item: Collector) { copyingId.value = item.id; error.value = ''; try { const draft = await appApi.copyCollector(item.id); items.value.unshift(draft); } catch { error.value = '复制失败，请稍后重试。'; } finally { copyingId.value = 0; } }
onMounted(load);
</script>

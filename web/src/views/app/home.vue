<template>
  <div class="app-page">
    <div class="eyebrow">SCRAPIO / V2.2</div>
    <h1>把网页变成你的数据</h1>
    <p class="lead">从目标网址开始，逐步配置页面操作、字段和运行方式。</p>
    <section class="app-card starter">
      <h2>开始一个采集方案</h2>
      <label for="entry-url">目标网址</label>
      <div class="url-row"><input id="entry-url" type="url" placeholder="https://example.com/list" disabled /><button disabled>开始采集</button></div>
      <p class="muted">新方案保存和可视化浏览器尚未接入，当前不能创建草稿。</p>
    </section>
    <div class="app-grid">
      <section class="app-card"><h2>浏览器编辑能力</h2><p :class="capabilities?.browser_ready ? 'status-good' : 'status-wait'">{{ capabilities?.browser_ready ? '已就绪' : '尚未就绪' }}</p><p class="muted">{{ capabilities?.reason || (loading ? '检查能力中…' : '能力状态暂不可用') }}</p></section>
      <section class="app-card"><h2>工作区</h2><p class="status-wait">{{ home?.status === 'pending' ? '正在建设' : '暂不可用' }}</p><p class="muted">{{ home?.reason || '新产品数据尚未接入。' }}</p></section>
    </div>
    <p v-if="error" class="app-error" role="alert">{{ error }} <button type="button" @click="reload">重试</button></p>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { appApi, type Capabilities, type HomeState } from './api';
const capabilities = ref<Capabilities>();
const home = ref<HomeState>();
const loading = ref(true);
const error = ref('');
async function reload() {
  loading.value = true;
  error.value = '';
  try { [capabilities.value, home.value] = await Promise.all([appApi.capabilities(), appApi.home()]); }
  catch { error.value = '无法读取新工作区状态，请检查服务连接。'; }
  finally { loading.value = false; }
}
onMounted(reload);
</script>

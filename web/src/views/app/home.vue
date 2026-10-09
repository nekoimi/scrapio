<template>
  <div class="app-page">
    <div class="eyebrow">SCRAPIO / V2.2</div>
    <h1>把网页变成你的数据</h1>
    <p class="lead">从目标网址开始，逐步配置页面操作、字段和运行方式。</p>
    <section class="app-card starter">
      <h2>开始一个采集方案</h2>
      <label for="entry-url">目标网址</label>
      <div class="url-row"><input id="entry-url" v-model="entryUrl" type="url" placeholder="https://example.com/list" @keyup.enter="start" /><select v-model="entryType" aria-label="入口类型"><option value="web">网页</option><option value="json">JSON 接口</option></select><button :disabled="creating || !entryUrl" @click="start">{{ creating ? '创建中…' : '开始采集' }}</button></div>
      <p class="muted">当前先创建可恢复的草稿；可视化浏览器和正式试采将在后续阶段接入。</p>
    </section>
    <div class="app-grid">
      <section class="app-card"><h2>浏览器编辑能力</h2><p :class="capabilities?.editor_service_connected ? 'status-good' : 'status-wait'">{{ capabilities?.editor_service_connected ? '编辑服务已连接' : '编辑服务未连接' }}</p><p class="muted">{{ capabilities?.reason || (loading ? '检查能力中…' : '能力状态暂不可用') }}</p></section>
      <section class="app-card"><h2>工作区</h2><p class="status-wait">{{ home?.status === 'pending' ? '正在建设' : '暂不可用' }}</p><p class="muted">{{ home?.reason || '新产品数据尚未接入。' }}</p></section>
    </div>
    <p v-if="error" class="app-error" role="alert">{{ error }} <button type="button" @click="reload">重试</button></p>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { appApi, type Capabilities, type HomeState } from './api';
const router = useRouter();
const entryUrl = ref('');
const entryType = ref<'web' | 'json'>('web');
const creating = ref(false);
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
async function start() {
  if (!entryUrl.value || creating.value) return;
  creating.value = true;
  try {
    const collector = await appApi.createCollector({ entry_url: entryUrl.value, entry_type: entryType.value });
    await router.push(`/app/collectors/${collector.id}`);
  } catch { error.value = '草稿创建失败，请检查网址或服务连接。'; }
  finally { creating.value = false; }
}
</script>

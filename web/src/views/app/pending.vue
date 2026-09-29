<template>
  <div class="app-page"><div class="eyebrow">SCRAPIO / V2.2</div><h1>{{ section }}</h1><section class="app-card"><h2>功能建设中</h2><p class="muted">{{ section }}尚未接入新产品数据域。完成后会在这里提供对应的管理能力。</p><router-link to="/app/home">返回首页</router-link></section></div>
</template>
<script setup lang="ts">defineProps<{ section: string }>();</script>

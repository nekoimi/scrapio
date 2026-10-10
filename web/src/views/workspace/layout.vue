<template>
 <div class="v21-shell">
  <aside class="v21-sidebar" :class="{open:menuOpen}">
   <div class="v21-brand"><span class="v21-logo">S</span><span><strong>scrapio</strong><small>Data collection · v2.1</small></span></div>
   <label class="v21-project-picker">当前项目<select v-model.number="selected" @change="chooseProject"><option v-if="!projects.length" :value="0">暂无项目</option><option v-for="project in projects" :key="project.id" :value="project.id">{{project.name}}</option></select></label>
   <div class="v21-nav-label">工作区</div>
   <nav aria-label="主要导航"><router-link v-for="entry in nav" :key="entry.path" :to="entry.path" @click="menuOpen=false"><span>{{entry.icon}}</span>{{entry.label}}</router-link></nav>
   <div class="v21-side-foot">控制平面 · PostgreSQL<br><small>浏览器服务按需连接</small></div>
  </aside>
  <div class="v21-main"><header class="v21-topbar"><button class="v21-mobile-menu" :aria-expanded="menuOpen" aria-label="打开导航" @click="menuOpen=!menuOpen">☰</button><span>scrapio / <strong>{{pageTitle}}</strong></span><router-link to="/settings/runtime">设置</router-link></header><main class="v21-content" tabindex="0" aria-label="工作区内容"><router-view /></main></div>
  <button v-if="menuOpen" class="v21-scrim" aria-label="关闭导航" @click="menuOpen=false" />
 </div>
</template>
<script setup lang="ts">
import { computed,onMounted,ref,watch } from 'vue';
import { useRoute } from 'vue-router';
import { api, type Project } from './api';
import { projectId,projectRefresh,selectProject } from './state';
import { NextLoading } from '/@/utils/loading';
import Watermark from '/@/utils/watermark';
import { useThemeConfig } from '/@/stores/themeConfig';
const route=useRoute(); const menuOpen=ref(false); const projects=ref<Project[]>([]); const selected=ref(projectId.value);
const theme=useThemeConfig();
const nav=[{path:'/workspace/projects',label:'项目',icon:'▦'},{path:'/workspace/data',label:'数据',icon:'▤'},{path:'/workspace/collectors',label:'采集器',icon:'⌘'},{path:'/workspace/runs',label:'运行',icon:'▷'}];
const pageTitle=computed(()=>nav.find(item=>item.path===route.path)?.label||'工作区');
function chooseProject(){selectProject(selected.value);}
watch(projectId,id=>{selected.value=id;});
watch(projectRefresh,async()=>{try{projects.value=await api.projects();}catch{ /* Project page shows errors. */ }});
onMounted(async()=>{theme.themeConfig.isWartermark=false;NextLoading.done(600);Watermark.del();try{projects.value=await api.projects(); if(!projects.value.some(p=>p.id===selected.value)){selected.value=projects.value[0]?.id||0; chooseProject();}}catch{ /* Pages show their own fetch errors. */ }});
</script>
<style src="./style.css"></style>

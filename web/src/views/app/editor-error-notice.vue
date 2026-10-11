<template>
	<section class="editor-error-notice" role="alert">
		<strong>{{ problem.title }}</strong>
		<p>{{ problem.reason }}</p>
		<p class="editor-error-recovery">{{ problem.recovery }}</p>
		<details v-if="problem.code || problem.requestID || problem.commandID || problem.stage || problem.path">
			<summary>诊断信息</summary>
			<dl>
				<template v-if="problem.stage"><dt>失败阶段</dt><dd>{{ problem.stage }}</dd></template>
				<template v-if="problem.path"><dt>配置位置</dt><dd>{{ problem.path }}</dd></template>
				<template v-if="problem.code"><dt>错误码</dt><dd>{{ problem.code }}</dd></template>
				<template v-if="problem.requestID"><dt>请求编号</dt><dd>{{ problem.requestID }}</dd></template>
				<template v-if="problem.commandID"><dt>动作编号</dt><dd>{{ problem.commandID }}</dd></template>
			</dl>
		</details>
	</section>
</template>
<script setup lang="ts">
import type { EditorProblem } from './editor-feedback';
defineProps<{ problem: EditorProblem }>();
</script>

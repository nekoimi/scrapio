<template>
	<details :id="`v22-field-${role || 'list'}-${field.field_key || field.name}`" class="field-options">
		<summary>类型、清洗与校验</summary>
		<div class="command-form-grid">
			<label
				>输出类型<select v-model="field.type" @change="if (!['date', 'datetime'].includes(field.type || '')) field.date_format = undefined;">
					<option :value="undefined">保持原类型</option>
					<option value="string">文本</option>
					<option value="integer">整数（int64）</option>
					<option value="number">数字</option>
					<option value="boolean">布尔</option>
					<option value="date">日期</option>
					<option value="datetime">日期时间</option>
					<option value="json">JSON 原值</option>
				</select></label
			>
			<label
				>空白处理<select v-model="field.clean">
					<option :value="undefined">保留原文</option>
					<option value="trim">去首尾空白</option>
					<option value="whitespace">合并空白</option>
				</select></label
			>
			<label><input v-model="field.required" type="checkbox" />必填</label
			><label><input v-model="field.multiple" type="checkbox" />多值（逐项转换）</label>
			<label>正则提取（可选，RE2）<input v-model="field.regex" maxlength="512" placeholder="第一个捕获组；不匹配会报错" /></label>
			<label v-if="field.type === 'date' || field.type === 'datetime'"
				>日期格式（Go layout）<input
					v-model="field.date_format"
					maxlength="64"
					:placeholder="field.type === 'date' ? '2006-01-02' : '2006-01-02T15:04:05Z07:00'"
			/></label>
		</div>
		<p class="muted">
			处理顺序：原值 → 空白/正则 → 类型 → 必填。单值匹配多个元素会报错；多值逐项校验。日期时间没有时区时按 UTC 解析。保存后在固定快照预览查看结果。
		</p>
	</details>
</template>
<script setup lang="ts">
import type { FieldOptions } from './api';
defineProps<{ field: FieldOptions & { name: string }; role?: string }>();
</script>

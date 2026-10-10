<template>
	<p v-if="diff.truncated" class="status-warn">差异超过 200 项，当前列表不完整；发布前需缩小变更并重新比较。</p>
	<p v-if="!diff.items.length" class="muted">在本次覆盖范围内没有差异。</p>
	<div v-else class="regression-diff-scroll">
		<table class="regression-diff">
			<thead>
				<tr>
					<th>路径 · JSON Pointer</th>
					<th>基准</th>
					<th>目标</th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="item in diff.items" :key="item.path">
					<td>{{ item.path || '/' }}</td>
					<td :title="item.before_hash">
						<pre>{{ item.before_present ? item.before_json : '（不存在）' }}</pre>
					</td>
					<td :title="item.after_hash">
						<pre>{{ item.after_present ? item.after_json : '（不存在）' }}</pre>
					</td>
				</tr>
			</tbody>
		</table>
	</div>
	<p class="muted">长值仅显示片段；悬停可查看完整值的哈希。不存在与 null 分开比较。</p>
</template>
<script setup lang="ts">
import type { RegressionDiff } from './api';
defineProps<{ diff: RegressionDiff }>();
</script>
<style scoped>
.regression-diff-scroll {
	overflow: auto;
	max-height: 420px;
}
.regression-diff {
	width: 100%;
	border-collapse: collapse;
	font-size: 13px;
}
.regression-diff th,
.regression-diff td {
	padding: 10px;
	border-bottom: 1px solid #dbe2ed;
	text-align: left;
	vertical-align: top;
}
.regression-diff td {
	max-width: 320px;
	overflow-wrap: anywhere;
}
.regression-diff pre {
	white-space: pre-wrap;
	margin: 0;
	font-size: 12px;
}
</style>

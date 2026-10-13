<template>
	<section v-if="resource" class="co-page">
		<div class="co-heading">
			<div>
				<router-link to="/app/co/collectors">← 独立采集器</router-link>
				<h1>{{ resource.name }}</h1>
				<div class="co-tag-row">
					<el-tag>revision {{ resource.revision }}</el-tag
					><el-tag v-if="dirty" type="warning">未保存</el-tag
					><el-tag v-if="versions.length" type="success">已发布 {{ versions.length }} 个版本</el-tag
					><el-tag v-if="resource.archived" type="info">已归档</el-tag>
				</div>
			</div>
			<div>
				<el-button :disabled="busy" @click="reload">重新加载</el-button
				><el-button type="primary" :loading="saving" :disabled="resource.archived || busy" @click="save">保存草稿</el-button>
			</div>
		</div>
		<el-alert v-if="problem" type="error" :title="problem" :closable="false" show-icon />
		<el-alert v-if="unconfirmedCommand" type="warning" title="网页动作结果未知，已暂停新动作" :closable="false"
			><el-button @click="queryEditorReceipt">查询原回执</el-button>
			<pre v-if="editorReceipt" class="co-json">{{ JSON.stringify(editorReceipt, null, 2) }}</pre>
		</el-alert>
		<el-alert v-if="unresolvedRequest" type="warning" title="请求结果尚未确认，先查询原请求" :closable="false"
			><el-button :loading="busy" @click="recoverRequest">查询原请求</el-button
			><el-button @click="clearUnknownRequest">确认后清除本地等待</el-button></el-alert
		>
		<el-alert v-if="pending" type="info" :title="'操作 ' + pending.id + ' · ' + pending.status" :closable="false"
			><p>响应丢失时查询原操作；不会自动重新发起访问或动作。</p>
			<el-button @click="queryPending" :loading="busy">查询原操作</el-button></el-alert
		>
		<el-alert
			v-if="!capabilities.browser_ready"
			type="info"
			title="浏览器服务未就绪：可以保存规则及使用HTTP采集，浏览器实时测试需升级/连接独立context服务。"
			:closable="false"
		/>
		<el-tabs v-model="tab">
			<el-tab-pane label="① 输入样例" name="input"
				><div class="co-columns">
					<el-card
						><template #header>输入契约</template>
						<div v-for="(f, i) in definition.input_fields" :key="i" class="co-field-row">
							<div class="co-fields">
								<el-input v-model="f.key" placeholder="稳定字段键，例如movie_id" /><el-input
									v-model="f.label"
									placeholder="显示名（可选）"
								/><el-select v-model="f.type"><el-option v-for="t in types" :key="t" :value="t" :label="t" /></el-select>
							</div>
							<el-checkbox v-model="f.required">必填</el-checkbox><el-checkbox v-model="f.nullable">可为null</el-checkbox
							><el-checkbox v-model="f.multiple">多值数组</el-checkbox><el-checkbox v-model="f.empty_allowed">允许空值</el-checkbox
							><el-button link type="danger" @click="definition.input_fields.splice(i, 1)">删除</el-button
							><el-input :model-value="defaultText(f)" @change="setDefault(f, $event)" placeholder="默认值JSON（仅缺失时使用；留空表示无默认值）" />
						</div>
						<el-button
							:disabled="definition.input_fields.length >= 30"
							@click="definition.input_fields.push(newField('input_' + definition.input_fields.length))"
							>添加输入字段</el-button
						></el-card
					><el-card
						><template #header>结构化样例输入</template>
						<p class="co-muted">输入不需要来源于列表。直接填写详情/评论地址及业务标识。必填与类型在访问前检查；凭据请在获取设置中使用引用。</p>
						<el-form label-position="top"
							><el-form-item v-for="f in definition.input_fields" :key="f.key" :label="(f.label || f.key) + ' · ' + f.type + (f.multiple ? '[]' : '')"
								><el-input
									v-if="!f.multiple && ['string', 'url', 'date', 'datetime'].includes(f.type)"
									:model-value="String(sampleInput[f.key] ?? '')"
									@update:model-value="sampleInput[f.key] = $event"
									:placeholder="f.type === 'url' ? 'https://…' : ''"
								/><el-select v-else-if="f.type === 'boolean' && !f.multiple" v-model="sampleInput[f.key]" clearable
									><el-option :value="true" label="true" /><el-option :value="false" label="false" /></el-select
								><el-input
									v-else
									:model-value="JSON.stringify(sampleInput[f.key] ?? null)"
									@change="setInputJSON(f.key, $event)"
									placeholder="JSON数字、对象或数组"
								/>
								<div>
									<el-button link @click="delete sampleInput[f.key]">设为缺失</el-button
									><el-button v-if="f.nullable" link @click="sampleInput[f.key] = null">显式null</el-button
									><el-tag size="small" type="info">{{ Object.hasOwn(sampleInput, f.key) ? JSON.stringify(sampleInput[f.key]) : '缺失' }}</el-tag>
								</div></el-form-item
							></el-form
						><el-button @click="inferOrigin">由样例URL填入允许来源</el-button></el-card
					>
				</div></el-tab-pane
			>
			<el-tab-pane label="② 获取与操作" name="fetch"
				><el-card
					><el-form label-position="top"
						><div class="co-fields">
							<el-form-item label="获取方式"
								><el-select v-model="definition.fetch.kind" @change="changeFetch"
									><el-option value="http_html" label="HTTP HTML" /><el-option value="http_json" label="HTTP JSON" /><el-option
										value="browser"
										label="浏览器" /></el-select></el-form-item
							><el-form-item label="URL绑定方式"
								><el-select v-model="definition.fetch.url.kind" @change="changeURLBinding"
									><el-option value="field" label="输入字段" /><el-option value="constant" label="固定地址" /><el-option
										value="template"
										label="{{input.字段}} 模板" /></el-select></el-form-item
							><el-form-item v-if="definition.fetch.kind !== 'browser'" label="请求方法"
								><el-select v-model="definition.fetch.method"><el-option value="GET" /><el-option value="POST" /></el-select
							></el-form-item>
						</div>
						<el-form-item label="目标地址"
							><el-select v-if="definition.fetch.url.kind === 'field'" v-model="definition.fetch.url.path"
								><el-option v-for="f in definition.input_fields" :key="f.key" :value="'input.' + f.key" :label="'input.' + f.key" /></el-select
							><el-input v-else v-model="definition.fetch.url.value" placeholder="https://example.com/subject/{{input.movie_id}}/" /></el-form-item
						><el-form-item label="允许的来源（每行scheme://host[:port]，列表页/详情域名按实际范围添加）"
							><el-input
								type="textarea"
								:rows="3"
								:model-value="definition.fetch.allowed_origins.join('\n')"
								@update:model-value="
									definition.fetch.allowed_origins = String($event)
										.split('\n')
										.map((v) => v.trim())
										.filter(Boolean)
								" /></el-form-item
						><el-form-item label="凭据引用（可选，不填写明文）"
							><el-input v-model="definition.fetch.credential_ref" placeholder="${credential:UUID} 或 ${secret:name}" /></el-form-item
						><template v-if="definition.fetch.kind !== 'browser'"
							><el-form-item label="查询参数绑定JSON（field / constant / template）"
								><el-input
									type="textarea"
									:rows="4"
									:model-value="JSON.stringify(definition.fetch.query || {}, null, 2)"
									@change="setQuery" /></el-form-item
							><el-form-item v-if="definition.fetch.method === 'POST'" label="请求体绑定JSON"
								><el-input
									type="textarea"
									:rows="4"
									:model-value="JSON.stringify(definition.fetch.body || { kind: 'constant', value: {} }, null, 2)"
									@change="setBody" /></el-form-item></template></el-form
				></el-card>
				<el-card v-if="definition.fetch.kind === 'browser'"
					><template #header>打开入口后的动作</template>
					<p class="co-muted">navigate、click、input、wait、scroll。点击/输入查原回执，未知结果停止等待确认。测试与编辑各使用独立浏览器会话。</p>
					<div v-for="(a, i) in definition.actions" :key="i" class="co-field-row">
						<div class="co-fields">
							<el-input v-model="a.id" placeholder="动作ID" /><el-select
								v-model="a.kind"
								@change="a.replay_class = ['click', 'input'].includes(a.kind) ? 'receipt_required' : 'read_only'"
								><el-option v-for="kind in ['navigate', 'click', 'input', 'wait', 'scroll']" :key="kind" :value="kind" /></el-select
							><el-input-number v-model="a.timeout_ms" :min="100" :max="30000" />
						</div>
						<el-input v-model="a.locator!.expression" placeholder="CSS定位器（动作不需要定位时留空）" /><el-input
							:model-value="JSON.stringify(a.value)"
							@change="setActionValue(a, $event)"
							placeholder='{"kind":"field","path":"input.search"}'
						/><el-button :disabled="!browserSession || busy || !!pending" @click="executeAction(a)">在编辑会话执行</el-button
						><el-button link type="danger" @click="definition.actions.splice(i, 1)">删除动作</el-button>
					</div>
					<el-button @click="addAction">添加动作</el-button></el-card
				></el-tab-pane
			>
			<el-tab-pane label="③ 当前页记录与字段" name="rules"
				><el-card
					><template #header>当前页提取规则</template>
					<div class="co-fields">
						<el-select v-model="definition.records.strategy"><el-option v-for="s in locatorKinds" :key="s" :value="s" /></el-select
						><el-input
							v-model="definition.records.expression"
							placeholder="当前页记录容器；详情可用main，列表可用.item，JSON用$.items[*]"
						/><el-button v-if="definition.fetch.kind === 'browser'" :disabled="busy || resource.archived" @click="openBrowser"
							>打开独立网页</el-button
						>
					</div>
					<p class="co-muted">字段相对每条记录定位。XPath使用 .//；单值命中多项会明确标为无效。链接是普通字段，可以交给其他采集器。</p></el-card
				>
				<template v-if="definition.fetch.kind === 'browser'"
					><div class="co-browser-toolbar">
						<el-radio-group v-model="pickTarget"
							><el-radio-button value="record">选记录</el-radio-button><el-radio-button value="field">选字段</el-radio-button
							><el-radio-button value="next">选下一页</el-radio-button></el-radio-group
						><el-button :disabled="!browserSession || busy" @click="refreshFrame">更新画面</el-button
						><el-button :disabled="!browserSession || busy" @click="scroll(-600)">向上滚动</el-button
						><el-button :disabled="!browserSession || busy" @click="scroll(600)">向下滚动</el-button
						><el-button :disabled="!browserSession || busy" @click="closeBrowser">关闭网页</el-button>
					</div>
					<p v-if="frameQueue.error.value" role="alert">{{ frameQueue.error.value }}</p>
					<div v-if="browserSession" class="co-browser-grid">
						<div>
							<p class="co-muted">{{ browserSession.current_url }} · 点选画面冻结，完成选择后更新；滚轮操作远端网页。</p>
							<div class="co-browser-view" @wheel.prevent="onWheel">
								<img v-if="frameQueue.frame.value" ref="image" :src="frameQueue.frame.value.objectURL" alt="独立浏览器当前页" @click="pick" />
								<div v-for="(box, i) in highlightBoxes" :key="i" class="co-selection-box" :style="box" aria-hidden="true"></div>
							</div>
						</div>
						<BrowserSelection
							ref="selection"
							independent
							:inspection-api="selectionApi"
							:session="browserSession"
							:mode="pickTarget === 'field' ? 'field' : 'record'"
							:target="pickTarget"
							:enabled="!frameQueue.stale.value"
							:blocked="busy || frameQueue.busy.value"
							:revision="resource.revision"
							:frame-state="frameQueue.frame.value?.state"
							:record-scope="recordScope"
							@rule="applyRule"
							@highlight="highlights = $event"
							@action-locator="prepareClick"
						/></div
				></template>
				<div v-for="(f, i) in definition.output.fields" :key="i" class="co-field-row">
					<div class="co-fields">
						<el-input v-model="f.key" @change="syncMappingKey(i)" placeholder="输出字段键" /><el-input
							v-model="f.label"
							placeholder="显示名"
						/><el-select v-model="f.type"><el-option v-for="t in types" :key="t" :value="t" /></el-select>
					</div>
					<el-checkbox v-model="f.required">必填</el-checkbox><el-checkbox v-model="f.nullable">可为null</el-checkbox
					><el-checkbox v-model="f.multiple">多值</el-checkbox><el-checkbox v-model="f.empty_allowed">允许空值</el-checkbox
					><el-button link type="danger" @click="removeField(i)">删除字段</el-button>
					<div class="co-fields">
						<el-select v-model="definition.output.mapping[i].source"
							><el-option value="element" label="当前记录提取" /><el-option value="input" label="输入透传" /><el-option
								value="constant"
								label="常量" /></el-select
						><el-select v-if="definition.output.mapping[i].source === 'input'" v-model="definition.output.mapping[i].input_key"
							><el-option v-for="input in definition.input_fields" :key="input.key" :value="input.key" /></el-select
						><el-input
							v-else-if="definition.output.mapping[i].source === 'constant'"
							:model-value="JSON.stringify(definition.output.mapping[i].value)"
							@change="setConstant(i, $event)"
							placeholder="JSON常量"
						/><el-input v-else v-model="definition.output.mapping[i].locator!.expression" placeholder="相对CSS / .//XPath / $.JSON字段" /><el-select
							v-if="definition.output.mapping[i].source === 'element'"
							v-model="definition.output.mapping[i].locator!.strategy"
							><el-option v-for="s in locatorKinds" :key="s" :value="s"
						/></el-select>
					</div>
					<div v-if="definition.output.mapping[i].source === 'element'" class="co-fields">
						<el-select v-model="definition.output.mapping[i].extract"
							><el-option value="text" label="文本" /><el-option value="link" label="链接地址" /><el-option
								value="image"
								label="图片地址" /><el-option value="attribute" label="属性" /></el-select
						><el-select v-model="definition.output.mapping[i].clean"
							><el-option value="none" label="不清洗" /><el-option value="trim" label="去除首尾空白" /><el-option
								value="whitespace"
								label="合并空白" /></el-select
						><el-input
							v-if="definition.output.mapping[i].extract === 'attribute'"
							v-model="definition.output.mapping[i].attribute"
							placeholder="属性名"
						/>
					</div>
				</div>
				<el-button :disabled="definition.output.fields.length >= 30" @click="addField()">添加输出字段</el-button></el-tab-pane
			>
			<el-tab-pane label="④ 分页与输出" name="output"
				><el-card
					><template #header>可选分页</template
					><el-form label-position="top"
						><div class="co-fields">
							<el-form-item label="分页方式"
								><el-select v-model="definition.pagination.kind" @change="changePagination"
									><el-option v-for="p in pageKinds" :key="p" :value="p" :label="pageLabels[p]" /></el-select></el-form-item
							><el-form-item label="结束依据"
								><el-select v-model="definition.pagination.end_condition"
									><el-option value="single_page" label="明确单页（只用于不分页）" /><el-option
										value="next_missing"
										label="下一页入口/游标消失" /><el-option value="empty_verified" label="显式空态已出现" /></el-select></el-form-item
							><el-form-item label="恢复方式"
								><el-select v-model="definition.pagination.resume"
									><el-option value="url_or_cursor" label="URL/游标检查点" /><el-option value="restart_item" label="从头重采此输入" /></el-select
							></el-form-item>
						</div>
						<el-form-item v-if="definition.pagination.kind !== 'none'" label="下一页链接/按钮/游标定位"
							><div class="co-fields">
								<el-select v-model="definition.pagination.next!.strategy"><el-option v-for="s in locatorKinds" :key="s" :value="s" /></el-select
								><el-input v-model="definition.pagination.next!.expression" placeholder="a.next 或 $.next_cursor" /></div></el-form-item
						><el-form-item v-if="definition.pagination.kind === 'cursor'" label="游标查询参数"
							><el-input v-model="definition.pagination.cursor_parameter" placeholder="cursor" /></el-form-item
						><el-form-item
							v-if="definition.pagination.end_condition === 'empty_verified'"
							label="空态标识（HTML元素 / JSON true；仅缺少记录不能算成功）"
							><div class="co-fields">
								<el-select
									:model-value="definition.pagination.empty_marker?.strategy || defaultStrategy"
									@update:model-value="setEmptyStrategy($event)"
									><el-option v-for="s in locatorKinds" :key="s" :value="s" /></el-select
								><el-input
									:model-value="definition.pagination.empty_marker?.expression || ''"
									@update:model-value="setEmptyExpression($event)"
								/></div
						></el-form-item>
						<div class="co-fields">
							<el-form-item label="最多页数"><el-input-number v-model="definition.pagination.max_pages" :min="1" :max="100" /></el-form-item
							><el-form-item label="最多记录"><el-input-number v-model="definition.pagination.max_records" :min="1" :max="10000" /></el-form-item
							><el-form-item label="最长执行ms"
								><el-input-number v-model="definition.pagination.max_duration_ms" :min="1" :max="600000"
							/></el-form-item></div></el-form></el-card
				><el-card
					><template #header>输出端口：records</template>
					<p>这里只定义结果契约，不绑定正式数据表。任务阶段会设置输入来源、结果保存和后续采集器。</p>
					<el-form label-position="top"
						><el-form-item label="稳定输出身份字段（1..5个非空必填单值；业务ID建议string）"
							><el-select v-model="definition.output.identity_fields" multiple
								><el-option v-for="f in definition.output.fields" :key="f.key" :value="f.key" :label="f.label || f.key" /></el-select></el-form-item
					></el-form>
					<pre class="co-json">{{ JSON.stringify(definition.output.fields, null, 2) }}</pre>
				</el-card></el-tab-pane
			>
			<el-tab-pane label="⑤ 测试与样例" name="tests"
				><el-card
					><template #header>单输入测试</template>
					<p>当前样例独立执行，不访问上游采集器。测试最多3页 / 100候选 / 60秒；受限或未覆盖会保留原因，不报告全量成功。</p>
					<div class="co-tag-row">
						<el-button type="primary" :disabled="dirty || busy || resource.revision === 0 || resource.archived" @click="test('live')"
							>实时测试当前样例</el-button
						><el-select v-model="selectedSample" placeholder="选择离线样例" class="co-input"
							><el-option v-for="s in samples" :key="s.id" :value="s.id" :label="s.id + ' · revision ' + s.revision" /></el-select
						><el-button :disabled="dirty || busy || !selectedSample || resource.archived" @click="test('offline')">回归样例</el-button
						><el-button @click="loadEvidence">刷新证据</el-button>
					</div>
					<p v-if="dirty" class="co-muted">请先保存草稿；本地变化不能使用旧revision测试或发布。</p></el-card
				>
				<el-card v-if="result"
					><template #header
						><div class="co-tag-row">
							<strong>{{ result.status }} · {{ result.mode }}</strong
							><el-tag>正式写入 {{ result.formal_writes }}</el-tag
							><el-tag v-if="result.reason" type="warning">{{ result.reason }}</el-tag
							><el-button :disabled="busy || dirty || !activeTest" @click="saveSample">保存输入/快照/预期样例</el-button>
						</div></template
					><el-alert v-if="result.error" :title="result.error.message + ' · ' + result.error.code" type="error" :closable="false" />
					<el-button v-if="result.status === 'awaiting_confirmation'" @click="queryTestReceipt">查询原网页动作回执</el-button>
					<pre v-if="originalReceipt" class="co-json">{{ JSON.stringify(originalReceipt, null, 2) }}</pre>
					<p v-if="result.comparison">
						输出对比：{{ result.comparison.status }} · 实际{{ result.comparison.actual_count }} / 预期{{ result.comparison.expected_count }}
					</p>
					<el-table :data="result.candidates"
						><el-table-column label="状态" width="90"
							><template #default="{ row }"
								><el-tag :type="row.valid ? 'success' : 'danger'">{{ row.valid ? '有效' : '无效' }}</el-tag></template
							></el-table-column
						><el-table-column label="数据"
							><template #default="{ row }">
								<pre class="co-json">{{ JSON.stringify(row.values, null, 2) }}</pre>
							</template></el-table-column
						><el-table-column label="字段证据"
							><template #default="{ row }"
								><p v-for="(f, k) in row.fields" :key="k">{{ k }} · 命中{{ f.match_count }} · {{ f.status }} {{ f.message }}</p></template
							></el-table-column
						></el-table
					>
					<details>
						<summary>页面与检查点证据</summary>
						<pre class="co-json">{{ JSON.stringify(result.pages, null, 2) }}</pre>
					</details></el-card
				>
				<el-card
					><template #header>最近测试</template
					><el-table :data="tests"
						><el-table-column label="时间"
							><template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template></el-table-column
						><el-table-column label="状态"
							><template #default="{ row }">{{ row.status }} / {{ row.result?.status || '待完成' }}</template></el-table-column
						><el-table-column label="操作"
							><template #default="{ row }"
								><el-button link @click="selectTest(row)">查看/查询</el-button
								><el-button v-if="!['queued', 'running'].includes(row.status)" link type="danger" @click="deleteTest(row.id)"
									>删除</el-button
								></template
							></el-table-column
						></el-table
					><el-button v-if="testCursor" @click="loadMoreTests">加载更多</el-button></el-card
				><el-card
					><template #header>保存的样例</template>
					<div v-for="s in samples" :key="s.id" class="co-tag-row">
						<span>{{ s.id }} · revision {{ s.revision }} · {{ s.snapshots.length }}页</span
						><el-button
							link
							@click="
								selectedSample = s.id;
								sampleInput = cloneJSON(s.input);
							"
							>使用输入</el-button
						><el-button link type="danger" @click="deleteSample(s.id)">删除</el-button>
					</div>
					<details>
						<summary>导入离线HTML/JSON样例</summary>
						<p>导入对象包含 input、snapshots[{url,format,content}]、expected_outputs。合成样例只作离线证据，不能代替实时发布检查。</p>
						<el-input v-model="importText" type="textarea" :rows="8" /><el-button :disabled="busy || dirty || resource.archived" @click="importSample"
							>导入样例</el-button
						>
					</details></el-card
				></el-tab-pane
			>
			<el-tab-pane label="⑥ 发布与版本" name="publish"
				><el-card
					><template #header>冻结为可复用采集器版本</template>
					<p>选择当前revision实时测试。发布不要求正式表，已发布版本保持不变；预算受限的证据会注明覆盖边界。</p>
					<el-select v-model="publishEvidence" placeholder="选择实时测试证据" class="co-input"
						><el-option
							v-for="t in tests.filter((t) => t.status === 'succeeded' && t.result?.mode === 'live')"
							:key="t.id"
							:value="t.id"
							:label="t.id + ' · ' + t.result.status" /></el-select
					><el-button :disabled="busy || dirty || !publishEvidence || resource.archived" @click="checkPublish">检查发布条件</el-button
					><el-button type="primary" :disabled="busy || dirty || !publishCheck?.result?.allowed || resource.archived" @click="publish"
						>发布版本</el-button
					>
					<pre v-if="publishCheck" class="co-json">{{ JSON.stringify(publishCheck.result, null, 2) }}</pre>
					<el-table :data="versions"
						><el-table-column prop="number" label="版本" /><el-table-column prop="revision" label="草稿revision" /><el-table-column
							prop="definition_hash"
							label="冻结定义hash"
						/><el-table-column label="输入 → 输出"
							><template #default="{ row }"
								>{{ row.definition.input_fields.map((f: Field) => f.key).join(', ') }} →
								{{ row.definition.output.fields.map((f: Field) => f.key).join(', ') }}</template
							></el-table-column
						></el-table
					>
					<p class="co-muted">发布后可在编排任务中使用；在任务中确认输入、数据表与预算后执行。</p></el-card
				>
				<el-button type="primary" :disabled="!versions.length || resource.archived" @click="router.push({path:'/app/co/tasks/new',query:{collector_id:resource.id}})">创建单阶段任务</el-button>
				<el-button :disabled="busy || resource.archived" @click="archive">归档采集器</el-button>
				<details>
					<summary>高级：查看或替换定义JSON</summary>
					<el-input v-model="definitionText" type="textarea" :rows="12" /><el-button @click="definitionText = JSON.stringify(definition, null, 2)"
						>复制当前定义到编辑框</el-button
					><el-button @click="applyDefinition">应用到本地草稿</el-button>
				</details></el-tab-pane
			>
		</el-tabs>
	</section>
	<el-empty v-else-if="!problem" description="加载独立采集器…" /><el-alert v-else :title="problem" type="error" :closable="false" />
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { ElMessage, ElMessageBox } from 'element-plus';
import BrowserSelection from '../app/browser-selection.vue';
import { createBrowserFrame } from '../app/browser-frame';
import { createWheelQueue, framePoint, wheelPixels } from '../app/browser-workbench';
import type { BrowserSession, SelectionRule, EditorLocator, PageHighlight } from '../app/api';
import {
	call,
	coApi,
	selectionApi,
	describe,
	initialDefinition,
	key,
	type Action,
	type Candidate,
	type Collector,
	type Definition,
	type Field,
	type Operation,
	type Sample,
	type TestResult,
	type Version,
} from './api';
const route = useRoute(),
 router = useRouter(),
	collectorID = String(route.params.id),
	resource = ref<Collector>(),
	definition = ref<Definition>(initialDefinition()),
	sampleInput = ref<Record<string, any>>({ url: '' }),
	saved = ref(''),
	problem = ref(''),
	busy = ref(false),
	saving = ref(false),
	tab = ref('input'),
	capabilities = ref({ browser_ready: false }),
	tests = ref<Operation<TestResult>[]>([]),
	testCursor = ref(''),
	samples = ref<Sample[]>([]),
	versions = ref<Version[]>([]),
	selectedSample = ref(''),
	activeTest = ref<Operation<TestResult>>(),
	publishEvidence = ref(''),
	publishCheck = ref<Operation>(),
	pending = ref<Operation>(),
	importText = ref(''),
	definitionText = ref('');
const types = ['string', 'url', 'integer', 'number', 'boolean', 'date', 'datetime', 'json'],
	pageLabels: Record<string, string> = { none: '单页', url: '下一页链接', cursor: 'JSON游标', click: '点击下一页', load_more: '加载更多' },
	defaultStrategy = computed(() => (definition.value.fetch.kind === 'http_json' ? 'jsonpath' : 'css')),
	locatorKinds = computed(() => (definition.value.fetch.kind === 'http_json' ? ['jsonpath'] : ['css', 'xpath'])),
	pageKinds = computed(() =>
		definition.value.fetch.kind === 'browser'
			? ['none', 'url', 'click', 'load_more']
			: definition.value.fetch.kind === 'http_json'
				? ['none', 'url', 'cursor']
				: ['none', 'url']
	),
	dirty = computed(() => JSON.stringify(definition.value) !== saved.value),
	result = computed(() => (activeTest.value?.result?.candidates ? activeTest.value.result : undefined));
let live = true,
	pollTimer: ReturnType<typeof setTimeout> | undefined;
const actionKeys = new Map<string, string>();
const unresolvedRequest = ref<{ operation: string; resource_id: string; key: string }>();
const unconfirmedCommand = ref(false);
const editorReceipt = ref<unknown>(),
	unknownEditorCommand = ref<{ session_id: string; command_id: string }>();
async function queryEditorReceipt() {
	const c = unknownEditorCommand.value;
	if (c) editorReceipt.value = await attempt(() => call('/sessions/' + c.session_id + '/commands/' + c.command_id));
	else problem.value = '原会话或回执已不可用，请人工核对；不会自动重放';
}
function clearRequest() {
	unresolvedRequest.value = undefined;
	sessionStorage.removeItem('co:request:' + collectorID);
}
async function clearUnknownRequest() {
	if (await confirm('请求可能已经被后台接受。请先核对原操作；清除只影响本地等待，不撤回访问，也不自动重试。')) clearRequest();
}
async function recoverRequest() {
	const q = unresolvedRequest.value;
	if (!q) return;
	const o = await attempt(() =>
		call<Operation>(
			'/operations/by-key?operation=' +
				encodeURIComponent(q.operation) +
				'&resource_id=' +
				encodeURIComponent(q.resource_id) +
				'&key=' +
				encodeURIComponent(q.key)
		)
	);
	if (o) {
		pending.value = o;
		storePending();
		clearRequest();
		await queryPending();
	}
}
function operationKey(operation: string, payload: unknown) {
	const stamp = operation + JSON.stringify(payload);
	if (!actionKeys.has(stamp)) actionKeys.set(stamp, key());
	return actionKeys.get(stamp)!;
}
async function attempt<T>(fn: () => Promise<T>): Promise<T | undefined> {
	problem.value = '';
	try {
		return await fn();
	} catch (e) {
		problem.value = describe(e);
		return undefined;
	}
}
async function loadEvidence() {
	await attempt(async () => {
		const [t, s, v] = await Promise.all([coApi.tests(collectorID), coApi.samples(collectorID), coApi.versions(collectorID)]);
		tests.value = t.items;
		testCursor.value = t.next_cursor;
		samples.value = s.items;
		versions.value = v.items;
	});
}
async function reload() {
	if (resource.value && dirty.value && !(await confirm('重新加载会丢弃未保存规则，是否继续？'))) return;
	await attempt(async () => {
		const c = await coApi.get(collectorID);
		resource.value = c;
		definition.value = 'definition_version' in c.definition ? cloneJSON(c.definition as Definition) : initialDefinition();
		normalizeLocators();
		saved.value = JSON.stringify(definition.value);
		await loadEvidence();
	});
}
function normalizeLocators() {
	for (const m of definition.value.output.mapping) {
		m.locator ||= { strategy: defaultStrategy.value as any, expression: '' };
	}
	for (const a of definition.value.actions) {
		a.locator ||= { strategy: 'css', expression: '' };
	}
	if (definition.value.pagination.kind !== 'none') definition.value.pagination.next ||= { strategy: defaultStrategy.value as any, expression: '' };
}
async function save() {
	if (!resource.value) return false;
	saving.value = true;
	const value = await attempt(() => coApi.save(collectorID, resource.value!.revision, cleanDefinition()));
	saving.value = false;
	if (!value) return false;
	resource.value = value;
	definition.value = cloneJSON(value.definition as Definition);
	normalizeLocators();
	saved.value = JSON.stringify(definition.value);
	publishCheck.value = undefined;
	ElMessage.success('独立草稿已保存');
	return true;
}
function cloneJSON<T>(value: T): T {
	return JSON.parse(JSON.stringify(value));
}
function cleanDefinition() {
	const d = cloneJSON(definition.value);
	for (const a of d.actions) {
		if (!a.locator?.expression) delete a.locator;
		if (a.kind === 'click') delete a.value;
	}
	for (const m of d.output.mapping) {
		if (m.source !== 'element') {
			delete m.locator;
			delete m.extract;
			delete m.clean;
			delete m.attribute;
		}
		if (m.source !== 'input') delete m.input_key;
		if (m.source !== 'constant') delete m.value;
	}
	if (d.pagination.kind === 'none') {
		delete d.pagination.next;
		delete d.pagination.cursor_parameter;
	}
	if (d.pagination.end_condition !== 'empty_verified') delete d.pagination.empty_marker;
	if (d.fetch.method !== 'POST') delete d.fetch.body;
	return d;
}
function newField(k: string): Field {
	return { key: k, type: 'string', required: true, nullable: false, multiple: false };
}
function defaultText(f: Field) {
	return Object.hasOwn(f, 'default') ? JSON.stringify(f.default) : '';
}
function setDefault(f: Field, text: string) {
	try {
		if (!text.trim()) delete f.default;
		else f.default = JSON.parse(text);
	} catch {
		problem.value = '默认值需要合法JSON';
	}
}
function setInputJSON(k: string, text: string) {
	try {
		sampleInput.value[k] = JSON.parse(text);
	} catch {
		problem.value = '样例 ' + k + ' 需要合法JSON';
	}
}
function inferOrigin() {
	try {
		const url = new URL(String(sampleInput.value.url));
		definition.value.fetch.allowed_origins = [url.origin];
		problem.value = '';
	} catch {
		problem.value = '先填写样例url，再填入允许来源';
	}
}
function changeFetch() {
	definition.value.records = { strategy: defaultStrategy.value as any, expression: defaultStrategy.value === 'jsonpath' ? '$[*]' : 'main' };
	definition.value.pagination.kind = 'none';
	changePagination();
	if (definition.value.fetch.kind !== 'browser') definition.value.actions = [];
	else {
		delete definition.value.fetch.query;
		delete definition.value.fetch.body;
		definition.value.fetch.method = 'GET';
	}
	for (const m of definition.value.output.mapping) {
		if (m.locator) m.locator.strategy = defaultStrategy.value as any;
	}
}
function changeURLBinding() {
	if (definition.value.fetch.url.kind === 'field') {
		definition.value.fetch.url = { kind: 'field', path: 'input.url' };
	} else definition.value.fetch.url = { kind: definition.value.fetch.url.kind, value: '' };
}
function parse(text: string, assign: (v: any) => void, label: string) {
	try {
		assign(JSON.parse(text));
		problem.value = '';
	} catch {
		problem.value = label + '需要合法JSON';
	}
}
function setQuery(v: string) {
	parse(v, (x) => (definition.value.fetch.query = x), '查询参数');
}
function setBody(v: string) {
	parse(v, (x) => (definition.value.fetch.body = x), '请求体');
}
function setActionValue(a: Action, v: string) {
	parse(v, (x) => (a.value = x), '动作值');
}
function setConstant(i: number, v: string) {
	parse(v, (x) => (definition.value.output.mapping[i].value = x), '输出常量');
}
function addAction() {
	definition.value.actions.push({
		id: 'action_' + definition.value.actions.length,
		kind: 'wait',
		locator: { strategy: 'css', expression: '' },
		value: { kind: 'constant', value: '500' },
		timeout_ms: 10000,
		replay_class: 'read_only',
	});
}
function addField(rule?: SelectionRule) {
	const k = rule?.name && /^[a-z][a-z0-9_]{0,63}$/.test(rule.name) ? rule.name : 'field_' + definition.value.output.fields.length;
	definition.value.output.fields.push({ ...newField(k), ...(rule?.name ? { label: rule.name } : {}) });
	definition.value.output.mapping.push({
		key: k,
		source: 'element',
		locator: rule?.locator || { strategy: defaultStrategy.value as any, expression: '' },
		extract: rule?.extract || 'text',
		attribute: rule?.attribute,
		clean: 'trim',
	});
}
function syncMappingKey(i: number) {
	definition.value.output.mapping[i].key = definition.value.output.fields[i].key;
}
function removeField(i: number) {
	const k = definition.value.output.fields[i].key;
	definition.value.output.fields.splice(i, 1);
	definition.value.output.mapping.splice(i, 1);
	definition.value.output.identity_fields = definition.value.output.identity_fields.filter((x) => x !== k);
}
function changePagination() {
	const p = definition.value.pagination;
	if (p.kind === 'none') {
		p.max_pages = 1;
		p.end_condition = 'single_page';
		delete p.next;
	} else {
		p.max_pages = Math.max(p.max_pages, 3);
		p.end_condition = 'next_missing';
		p.next ||= { strategy: defaultStrategy.value as any, expression: '' };
	}
	p.resume = ['click', 'load_more'].includes(p.kind) ? 'restart_item' : 'url_or_cursor';
}
function setEmptyStrategy(s: string) {
	definition.value.pagination.empty_marker = { strategy: s as any, expression: definition.value.pagination.empty_marker?.expression || '' };
}
function setEmptyExpression(e: string) {
	definition.value.pagination.empty_marker = {
		strategy: (definition.value.pagination.empty_marker?.strategy || defaultStrategy.value) as any,
		expression: e,
	};
}
async function confirm(message: string) {
	try {
		await ElMessageBox.confirm(message, '确认操作', {
			type: 'warning',
			confirmButtonText: '继续',
			cancelButtonText: '取消',
			closeOnClickModal: false,
		});
		return true;
	} catch {
		return false;
	}
}
async function submit(path: string, operation: string, payload: any) {
	if (unresolvedRequest.value || pending.value) {
		problem.value = '先查询当前操作的结果，再发起新操作';
		return;
	}
	busy.value = true;
	const idem = operation === 'editorCommand' ? payload.command_id : operationKey(operation, payload);
	unresolvedRequest.value = {
		operation,
		resource_id: operation === 'editorCommand' ? browserSession.value?.session_id || '' : collectorID,
		key: idem,
	};
	sessionStorage.setItem('co:request:' + collectorID, JSON.stringify(unresolvedRequest.value));
	let o: Operation | undefined;
	try {
		o = await call<Operation>(path, 'post', payload, idem);
	} catch (e: any) {
		problem.value = describe(e);
		if (e?.error?.code && e.error.code !== 'INTERNAL_ERROR') clearRequest();
	}
	busy.value = false;
	if (o) {
		clearRequest();
		actionKeys.delete(operation + JSON.stringify(payload));
		pending.value = o;
		storePending();
		await queryPending();
	} else {
		try {
			const recovered = await call<Operation>(
				'/operations/by-key?operation=' +
					encodeURIComponent(operation) +
					'&resource_id=' +
					encodeURIComponent(operation === 'editorCommand' ? browserSession.value?.session_id || '' : collectorID) +
					'&key=' +
					idem
			);
			pending.value = recovered;
			clearRequest();
			actionKeys.delete(operation + JSON.stringify(payload));
			storePending();
			await queryPending();
		} catch {
			/* Keep same fingerprint/key for manual retry; no replay. */
		}
	}
	return o;
}
function storePending() {
	if (pending.value) sessionStorage.setItem('co:pending:' + collectorID, pending.value.id);
}
async function queryPending() {
	if (!pending.value || !live) return;
	const o = await attempt(() => coApi.operation(pending.value!.id));
	if (!o) return;
	pending.value = o;
	if (o.status === 'queued' || o.status === 'running') {
		clearTimeout(pollTimer);
		pollTimer = setTimeout(() => void queryPending(), 1500);
		return;
	}
	clearTimeout(pollTimer);
	sessionStorage.removeItem('co:pending:' + collectorID);
	if (o.status === 'failed') {
		if (o.operation === 'editorCommand' && o.error?.code === 'OUTCOME_UNCERTAIN') {
			unconfirmedCommand.value = true;
			unknownEditorCommand.value = { session_id: o.resource_id, command_id: o.result.command_id };
		}
		problem.value = describe({ error: o.error });
		if (o.operation === 'testCollector') activeTest.value = o as Operation<TestResult>;
	} else if (o.operation === 'testCollector') {
		activeTest.value = o as Operation<TestResult>;
		publishEvidence.value = o.id;
		await loadEvidence();
	} else if (o.operation === 'checkPublication') {
		publishCheck.value = o;
	} else if (o.operation === 'createEditorSession') {
		browserSession.value = o.result;
		await refreshFrame();
	} else if (o.operation === 'editorCommand') {
		await refreshFrame();
	}
	pending.value = undefined;
}
async function test(mode: 'live' | 'offline') {
	if (mode === 'live' && !(await confirm('实时测试会访问目标页面并执行已配置动作；不写正式数据。'))) return;
	const sample = samples.value.find((s) => s.id === selectedSample.value);
	if (mode === 'offline' && !sample) return;
	const input = mode === 'offline' ? sample!.input : sampleInput.value;
	await submit('/collectors/' + collectorID + '/tests', 'testCollector', {
		expected_revision: resource.value!.revision,
		input,
		mode,
		...(mode === 'offline' ? { sample_id: selectedSample.value } : {}),
	});
	tab.value = 'tests';
}
async function selectTest(t: Operation<TestResult>) {
	activeTest.value = t;
	const o = await attempt(() => coApi.operation<TestResult>(t.id));
	if (o) {
		activeTest.value = o;
		if (['queued', 'running'].includes(o.status)) {
			pending.value = o;
			await queryPending();
		}
	}
}
async function loadMoreTests() {
	await attempt(async () => {
		const p = await coApi.tests(collectorID, testCursor.value);
		tests.value.push(...p.items);
		testCursor.value = p.next_cursor;
	});
}
async function saveSample() {
	if (!activeTest.value) return;
	const payload = { expected_revision: resource.value!.revision, evidence_id: activeTest.value.id };
	await attempt(() => call('/collectors/' + collectorID + '/samples', 'post', payload, operationKey('saveSample', payload)));
	await loadEvidence();
}
async function importSample() {
	try {
		const data = JSON.parse(importText.value);
		const payload = { expected_revision: resource.value!.revision, ...data };
		await attempt(() => call('/collectors/' + collectorID + '/samples', 'post', payload, operationKey('importSample', payload)));
		await loadEvidence();
	} catch {
		problem.value = '导入内容需要合法JSON';
	}
}
async function deleteTest(id: string) {
	if (await confirm('删除这条测试证据？已被版本引用的证据会阻断删除。')) {
		await attempt(() => call('/operations/' + id, 'delete'));
		await loadEvidence();
	}
}
async function deleteSample(id: string) {
	if (await confirm('删除样例及有限快照？')) {
		await attempt(() => call('/samples/' + id, 'delete'));
		await loadEvidence();
	}
}
async function checkPublish() {
	await submit('/collectors/' + collectorID + '/publish-checks', 'checkPublication', {
		expected_revision: resource.value!.revision,
		evidence_id: publishEvidence.value,
	});
}
async function publish() {
	if (!publishCheck.value || !(await confirm('冻结当前输入、步骤与输出为新版本？已有版本不会改变。'))) return;
	const payload = { expected_revision: resource.value!.revision, check_id: publishCheck.value.id };
	await attempt(() => call('/collectors/' + collectorID + '/versions', 'post', payload, operationKey('publishCollector', payload)));
	await loadEvidence();
}
async function archive() {
	if (!(await confirm('归档后停止编辑与新测试，保留已发布版本及证据。'))) return;
	const payload = { expected_revision: resource.value!.revision };
	const c = await attempt(() =>
		call<Collector>('/collectors/' + collectorID + '/archive', 'post', payload, operationKey('archiveCollector', payload))
	);
	if (c) resource.value = c;
}
function applyDefinition() {
	parse(
		definitionText.value,
		(x) => {
			definition.value = x;
			normalizeLocators();
		},
		'定义'
	);
}
const browserSession = ref<BrowserSession>(),
	image = ref<HTMLImageElement>(),
	selection = ref<InstanceType<typeof BrowserSelection>>(),
	pickTarget = ref<'record' | 'field' | 'next'>('record'),
	recordScope = computed(() => (definition.value.records.strategy === 'jsonpath' ? undefined : (definition.value.records as EditorLocator)));

const originalReceipt = ref<unknown>();
async function queryTestReceipt() {
	if (activeTest.value) originalReceipt.value = await attempt(() => call('/operations/' + activeTest.value!.id + '/receipt'));
}
async function executeAction(action: Action) {
	if (!browserSession.value) return;
	if (
		['click', 'input', 'navigate'].includes(action.kind) &&
		!(await confirm('在当前编辑网页执行 ' + action.kind + '？此动作可能改变网页状态，未知结果只查询原回执。'))
	)
		return;
	let value = '';
	try {
		const b = action.value;
		if (b?.kind === 'field') {
			const k = b.path?.replace(/^input\./, '') || '';
			if (!Object.hasOwn(sampleInput.value, k)) throw new Error('缺少动作输入 ' + k);
			value = String(sampleInput.value[k]);
		} else if (b?.kind === 'constant') value = String(b.value);
		else if (b?.kind === 'template')
			value = String(b.value).replace(/\{\{input\.([a-z][a-z0-9_]*)\}\}/g, (_, k) => {
				if (!Object.hasOwn(sampleInput.value, k)) throw new Error('缺少动作输入 ' + k);
				return String(sampleInput.value[k]);
			});
		const commandID = key();
		await submit('/sessions/' + browserSession.value.session_id + '/commands', 'editorCommand', {
			command_id: commandID,
			page_state_id: browserSession.value.page_state_id,
			action: action.kind,
			confirmed: true,
			...(action.locator?.expression ? { locator: action.locator } : {}),
			value,
			timeout_ms: action.timeout_ms,
		});
	} catch (e) {
		problem.value = describe(e);
	}
}
const frameQueue = createBrowserFrame({
	load: (target) => coApi.frame({ ...browserSession.value!, frame_url: target.url, page_state_id: target.pageStateID }),
});
const highlights = ref<PageHighlight>();
const highlightBoxes = computed(() => {
	const h = highlights.value,
		s = browserSession.value;
	if (!h || !s || h.page_state_id !== frameQueue.frame.value?.pageStateID) return [];
	const v = frameQueue.frame.value?.state?.viewport || s.viewport;
	return h.elements
		.slice(0, 50)
		.map((e) => ({
			left: (100 * e.bounds.x) / v.width + '%',
			top: (100 * e.bounds.y) / v.height + '%',
			width: (100 * e.bounds.width) / v.width + '%',
			height: (100 * e.bounds.height) / v.height + '%',
		}));
});
async function openBrowser() {
	if (dirty.value && !(await save())) return;
	if (!(await confirm('以当前样例地址打开独立编辑会话？测试会另开会话，不共享DOM和登录状态。'))) return;
	await submit('/collectors/' + collectorID + '/editor-sessions', 'createEditorSession', {
		expected_revision: resource.value!.revision,
		input: sampleInput.value,
		ttl_seconds: 600,
		viewport: { width: 1280, height: 800 },
	});
}
async function refreshFrame() {
	if (!browserSession.value || busy.value) return;
	await attempt(async () => {
		const s = await coApi.session(browserSession.value!.session_id);
		browserSession.value = s;
		frameQueue.setTarget({ sessionID: s.session_id, pageStateID: s.page_state_id, url: s.frame_url, available: s.available, state: s.state });
		await frameQueue.refresh();
	});
}
function pick(event: MouseEvent) {
	if (!image.value || !frameQueue.frame.value || frameQueue.stale.value || busy.value) return;
	const f = frameQueue.frame.value;
	const p = framePoint(event.clientX, event.clientY, image.value.getBoundingClientRect(), f.width, f.height);
	if (p) selection.value?.framePick(p.x, p.y, f.pageStateID);
}
function applyRule(rule: SelectionRule) {
	if (rule.target === 'record') {
		definition.value.records = { ...rule.locator };
	} else if (rule.target === 'field') {
		addField(rule);
	} else if (rule.target === 'next') {
		definition.value.pagination.kind = rule.next_kind === 'link' ? 'url' : rule.next_kind || 'url';
		changePagination();
		definition.value.pagination.next = { ...rule.locator };
	}
}
function prepareClick(locator: EditorLocator) {
	addAction();
	const a = definition.value.actions.at(-1)!;
	a.kind = 'click';
	a.locator = { ...locator };
	a.replay_class = 'receipt_required';
	delete a.value;
	tab.value = 'fetch';
}
async function scroll(distance: number) {
	const s = browserSession.value;
	if (!s || busy.value || pending.value || unconfirmedCommand.value) return false;
	const commandID = key();
	await submit('/sessions/' + s.session_id + '/commands', 'editorCommand', {
		command_id: commandID,
		page_state_id: s.page_state_id,
		action: 'scroll',
		confirmed: true,
		value: String(Math.round(distance)),
		timeout_ms: 10000,
	});
	return !problem.value;
}
const wheels = createWheelQueue({ ready: () => !!browserSession.value && !busy.value && !pending.value, dispatch: scroll });
function onWheel(e: WheelEvent) {
	wheels.add(wheelPixels(e, browserSession.value?.viewport.height || 800));
}
async function closeBrowser() {
	if (!browserSession.value) return;
	if (unconfirmedCommand.value && !(await confirm('关闭会话会使原动作回执不可再查询。请先核对实际动作结果，是否仍关闭？'))) return;
	const id = browserSession.value.session_id;
	await attempt(() => call('/sessions/' + id, 'delete'));
	browserSession.value = undefined;
	unconfirmedCommand.value = false;
	unknownEditorCommand.value = undefined;
	frameQueue.clear();
}
function beforeUnload(e: BeforeUnloadEvent) {
	if (dirty.value) {
		e.preventDefault();
		e.returnValue = '';
	}
}
async function guard() {
	return !dirty.value || (await confirm('存在未保存规则，离开会丢失这些修改，是否继续？'));
}
onBeforeRouteLeave(guard);
onBeforeRouteUpdate(guard);
watch(definition, () => (publishCheck.value = undefined), { deep: true });
onMounted(async () => {
	window.addEventListener('beforeunload', beforeUnload);
	await reload();
	const caps = await attempt(() => call<{ browser_ready: boolean }>('/capabilities'));
	if (caps) capabilities.value = caps;
	const original = sessionStorage.getItem('co:pending:' + collectorID);
	if (original) {
		pending.value = { id: original, status: 'queued', resource_id: collectorID, operation: '', result: {}, created_at: '' };
		await queryPending();
	}
	if (!original) {
		try {
			const raw = sessionStorage.getItem('co:request:' + collectorID);
			if (raw) {
				unresolvedRequest.value = JSON.parse(raw);
				await recoverRequest();
			}
		} catch {
			problem.value = '原请求恢复信息无法读取，请核对测试历史';
		}
	}
});
onBeforeUnmount(() => {
	live = false;
	clearTimeout(pollTimer);
	wheels.dispose();
	frameQueue.dispose();
	window.removeEventListener('beforeunload', beforeUnload);
	if (browserSession.value && !unconfirmedCommand.value && pending.value?.operation !== 'editorCommand')
		void call('/sessions/' + browserSession.value.session_id, 'delete').catch(() => {});
});
</script>
<style src="./style.css"></style>

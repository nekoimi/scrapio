<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / SETTINGS</div>
		<h1>凭据与授权</h1>
		<p class="lead">秘密只在创建或替换时写入；方案、版本和样例保存引用。修改、撤销或过期后需重新打开授权会话。</p>
		<p v-if="error" class="app-error" role="status">{{ error }}</p>
		<div class="command-form-grid">
			<section class="app-card">
				<div class="pane-heading">
					<h2>我的凭据</h2>
					<button :disabled="busy" @click="load">刷新列表</button>
				</div>
				<p v-if="!encryptionReady" class="status-warn">服务端未配置加密密钥；可使用部署环境引用，加密保存暂不可用。</p>
				<button :disabled="busy || !!pending" @click="createNew">新增凭据</button>
				<article v-for="c in items" :key="c.credential_id" class="credential-row">
					<button :disabled="busy || !!pending" @click="select(c)">{{ c.name }}</button>
					<p>{{ c.kind === 'browser_cookie' ? '浏览器 Cookie' : 'HTTP 认证头' }} · {{ c.status }} · revision {{ c.revision }}</p>
					<p>{{ c.origin }} · 到期 {{ c.expires_at }}</p>
					<code>{{ c.credential_ref }}</code
					><button v-if="c.status !== 'revoked'" :disabled="busy || !!pending" @click="revoke(c)">撤销</button>
				</article>
				<p v-if="!busy && !error && !items.length">尚无凭据。列表最多保留100项，旧业务凭据不进入此页面。</p>
			</section>
			<section class="app-card">
				<h2>{{ form.expected_revision ? '更新凭据' : '新增凭据' }}</h2>
				<fieldset :disabled="busy || !!pending || selectedRevoked">
					<label>名称<input v-model="form.name" maxlength="80" /></label
					><label
						>用途<select v-model="form.kind" :disabled="form.expected_revision > 0">
							<option value="http_header">HTTP 认证头</option>
							<option value="browser_cookie">浏览器 Cookie</option>
						</select></label
					><label
						>准确目标 origin<input v-model="form.origin" placeholder="https://example.com" :disabled="form.expected_revision > 0"
					/></label>
					<p class="muted">包含协议和端口、不含路径；浏览器授权仅支持 HTTPS，跨 origin 导航、重定向和资源会被阻止。</p>
					<label
						>保存方式<select v-model="form.storage">
							<option value="encrypted">加密保存</option>
							<option value="environment">部署环境引用</option>
						</select></label
					><template v-if="form.kind === 'http_header'"
						><label
							>认证头<select v-model="form.header">
								<option>Authorization</option>
								<option>Cookie</option>
								<option>X-API-Key</option>
								<option>API-Key</option>
								<option>X-Auth-Token</option>
							</select></label
						><label>固定前缀<input v-model="form.prefix" placeholder="Bearer （末尾含空格）" /></label></template
					><label v-if="form.storage === 'environment'">环境变量名<input v-model="form.env" placeholder="SCRAPIO_SECRET_EXAMPLE" /></label
					><label v-else
						>{{ form.kind === 'browser_cookie' ? 'Cookie 请求头内容，如 sid=...; session=...' : '秘密值'
						}}<input
							v-model="form.secret"
							type="password"
							autocomplete="new-password"
							:placeholder="form.expected_revision ? '留空保留当前秘密' : '只写入，不回显'" /></label
					><label>到期时间（最多366天）<input v-model="expiresLocal" type="datetime-local" /></label
					><label
						>页面遮挡 CSS（每行一个，最多10项）<textarea
							v-model="masks"
							rows="4"
							placeholder=".account-info
#private-profile"
						/>
					</label>
					<p class="muted">
						浏览器授权会话自动遮挡输入框、文本域和 iframe 截图；所选 CSS
						同时遮挡快照与截图。已有样例不会被追溯修改，保存前仍须人工检查敏感信息。
					</p>
					<button :disabled="!dirty || (form.storage === 'encrypted' && !encryptionReady)" @click="save">保存凭据</button>
				</fieldset>
				<p v-if="pending" class="status-warn">请求未确认。本地编辑保留；先读取服务器版本核对，勿重复创建或替换。</p>
				<button v-if="pending" :disabled="busy" @click="recover">读取原凭据状态</button>
				<p v-if="selectedRevoked">已撤销，秘密已清除；请新增凭据并更新方案引用。</p>
			</section>
		</div>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type Credential, type CredentialInput } from './api';
const items = ref<Credential[]>([]),
	busy = ref(false),
	error = ref(''),
	encryptionReady = ref(false),
	pending = ref(''),
	selectedRevoked = ref(false);
const defaults = (): CredentialInput => ({
	credential_id: crypto.randomUUID(),
	expected_revision: 0,
	name: '',
	kind: 'http_header',
	origin: '',
	storage: 'encrypted',
	header: 'Authorization',
	prefix: '',
	env: '',
	secret: '',
	expires_at: '',
	mask_selectors: [],
});
const form = ref(defaults()),
	expiresLocal = ref(''),
	masks = ref('');
let alive = true;
const state = () => JSON.stringify([form.value, expiresLocal.value, masks.value]);
const saved = ref(state());
const dirty = computed(() => state() !== saved.value);
const msg = (e: any) => e?.response?.data?.error?.message || e?.error?.message || e?.message || '请求未确认';
async function load() {
	if (busy.value) return;
	busy.value = true;
	error.value = '';
	try {
		const v = await appApi.credentials();
		if (alive) {
			items.value = v.items;
			encryptionReady.value = v.encryption_ready;
		}
	} catch (e) {
		if (alive) error.value = msg(e);
	} finally {
		if (alive) busy.value = false;
	}
}
function discard() {
	return !dirty.value || window.confirm('放弃本地凭据编辑？秘密不会保存在页面之外。');
}
function createNew() {
	if (!discard()) return;
	form.value = defaults();
	expiresLocal.value = '';
	masks.value = '';
	selectedRevoked.value = false;
	saved.value = state();
}
function apply(c: Credential) {
	form.value = {
		credential_id: c.credential_id,
		expected_revision: c.revision,
		name: c.name,
		kind: c.kind,
		origin: c.origin,
		storage: c.storage,
		header: c.header,
		prefix: c.prefix,
		env: c.env,
		secret: '',
		expires_at: c.expires_at,
		mask_selectors: c.mask_selectors,
	};
	const date = new Date(c.expires_at);
	expiresLocal.value = new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
	masks.value = c.mask_selectors.join('\n');
	selectedRevoked.value = c.status === 'revoked';
	saved.value = state();
}
function select(c: Credential) {
	if (discard()) apply(c);
}
async function save() {
	if (busy.value || pending.value) return;
	const expiry = new Date(expiresLocal.value);
	if (!Number.isFinite(expiry.getTime())) {
		error.value = '请选择有效到期时间';
		return;
	}
	const i = {
		...form.value,
		expires_at: expiry.toISOString(),
		mask_selectors: masks.value
			.split('\n')
			.map((s) => s.trim())
			.filter(Boolean),
	};
	if (i.kind === 'browser_cookie') {
		i.header = '';
		i.prefix = '';
	}
	if (i.storage === 'environment') {
		i.secret = '';
	} else {
		i.env = '';
	}
	busy.value = true;
	error.value = '';
	try {
		const c = await appApi.saveCredential(i);
		if (alive) {
			apply(c);
			error.value = '已保存。授权网站是否接受仍需打开页面或实时试采验证。';
		}
	} catch (e) {
		if (alive) {
			pending.value = i.credential_id;
			error.value = msg(e);
		}
	} finally {
		if (alive) busy.value = false;
	}
	if (!pending.value) void load();
}
async function recover() {
	if (busy.value || !pending.value) return;
	busy.value = true;
	try {
		const c = await appApi.credential(pending.value);
		if (alive && window.confirm(`服务器当前 revision ${c.revision}，读取会清除本地秘密编辑。确认以服务器元数据为准？`)) {
			apply(c);
			pending.value = '';
			error.value = '已读取元数据，秘密不回显。';
		}
	} catch (e: any) {
		if (alive) {
			error.value = msg(e);
			if (e?.response?.status === 404 || e?.error?.code === 'NOT_FOUND') pending.value = '';
		}
	} finally {
		if (alive) busy.value = false;
	}
}
async function revoke(c: Credential) {
	if (!window.confirm(`撤销 ${c.name}？后续取样和运行会拒绝此引用；既有结果保留。`)) return;
	busy.value = true;
	try {
		const v = await appApi.revokeCredential(c.credential_id, c.revision);
		if (alive) {
			if (form.value.credential_id === v.credential_id) apply(v);
		}
	} catch (e) {
		if (alive) {
			pending.value = c.credential_id;
			error.value = msg(e);
		}
	} finally {
		if (alive) busy.value = false;
	}
	if (!pending.value) void load();
}
onMounted(load);
onBeforeUnmount(() => {
	alive = false;
	form.value.secret = '';
});
function leaveCredentials() { return (!dirty.value && !pending.value && !busy.value) || window.confirm('凭据编辑未保存或请求未确认，确认离开？本地秘密不会保留，服务器已接受的写入仍可能完成。'); }
onBeforeRouteLeave(leaveCredentials);
onBeforeRouteUpdate((to,from)=>to.query.tab===from.query.tab || leaveCredentials());
</script>
<style scoped>
.credential-row {
	border-bottom: 1px solid #e5e9f0;
	padding: 14px 0;
}
.credential-row p {
	overflow-wrap: anywhere;
	font-size: 13px;
}
.credential-row code {
	display: block;
	font-size: 12px;
	overflow-wrap: anywhere;
}
fieldset {
	border: 0;
	padding: 0;
}
input,
select,
textarea {
	width: 100%;
	margin-bottom: 12px;
}
</style>

# A07：HTTP 与 JSON 入口

本阶段提供「保存请求 → 获取或粘贴快照 → 选择 JSON 数组和字段 → 检查固定输入」的编辑流程。纯 JSON 不依赖 scrapio-browser，不创建浏览器会话。快照和诊断保存在新表 `v22_captures`，不读取旧采集数据，不写正式业务记录，也不投递下载插件。

## 页面操作

1. 在新工作台创建 JSON 入口方案，进入草稿页的「HTTP / 离线输入」。填写 GET/POST、查询参数、普通请求头、可选 JSON body 和超时，保存入口请求。查询参数和请求头使用字符串值的 JSON 对象。
2. 选择 HTTP 取样并确认实际访问，或选择离线方式粘贴 JSON/HTML。离线不访问网站、不解析凭据；绑定已保存的方案 revision。HTTP POST 可能在目标网站产生副作用，取样失败不表示请求没有执行。
3. 成功的 JSON 快照显示响应树。选择数组，再选择其中某个元素的字段，设置字段名并保存规则。空 JSON Pointer 代表根数组；`/data/items` 代表嵌套数组；字段 `/title` 相对每条数组记录。
4. 点击「检查已保存快照」，展示每条记录的字段原值和未命中项。这个检查不访问网站，可用旧快照验证新规则；结果标注快照哈希和规则 revision，修改后提示过期。
5. 未保存修改会阻止 HTTP 取样；版本冲突保留本地修改，可明确放弃后重新加载服务器草稿。未知或多份 JSON 记录配置用原始 JSON 编辑，表单不覆盖。

JSON 树按层展开，最多展示 20 层、每层 500 个子项；手工 Pointer 不受树展示限制。JavaScript 数字展示可能丢失大整数精度，以保存内容和服务端 `raw_value` 为准。

## 草稿格式

```json
{
  "definition_version": 1,
  "entry_url": "https://example.com/api/items",
  "http_request": {
    "method": "GET",
    "query": {"page": "1"},
    "headers": {"Accept": "application/json"},
    "timeout_ms": 10000,
    "credential_ref": "${secret:example_api}"
  },
  "steps": [{
    "step_id": "items",
    "type": "json_records",
    "config": {
      "array_pointer": "/data/items",
      "max_records": 10,
      "fields": [{"name": "title", "pointer": "/title"}]
    }
  }]
}
```

公开接口省略 `credential_ref`。POST 的 `body` 为 JSON 值；GET 不支持 body。步骤只描述规则，本阶段还没有调度/正式执行 `json_records`。

## 服务端配置

在当前加载的配置文件中添加：

```yaml
http_entry:
  allow_private_network: false
  credentials:
    example_api:
      env: SCRAPIO_EXAMPLE_API_TOKEN
      origins: ["https://example.com"]
      owner_ids: [1]
      header: Authorization
      prefix: "Bearer "
```

`env` 只写环境变量名称，实际值通过部署环境注入；配置、草稿和快照日志均不保存解析后的秘密。`owner_ids` 必须是允许使用该凭据的实际用户 ID；origin 精确匹配协议、主机和显式端口，不使用通配符。可配置 `Cookie` 或 `X-API-Key` 等认证头。缺少变量、用户或 origin 不匹配时返回持久化的 `CREDENTIAL_UNAVAILABLE`，且不访问网络。凭据管理页面、加密存储及浏览器登录状态由 D04 承接。

默认禁止私网、回环和非全局单播目的地址；DNS 检查后直接拨号到已验证 IP，不使用系统 HTTP 代理。自建内网 API 需要管理员明确启用 `allow_private_network: true`，也可用 `HTTP_ENTRY_ALLOW_PRIVATE_NETWORK=true` 覆盖；该开关影响所有有权限的用户。HTTP 公共 GET 最多三次同 origin 重定向；带凭据请求与 POST 不跟随重定向。

## 接口与恢复语义

接口契约见[OpenAPI](阶段0接口契约草案.yaml)，身份认证沿用原始 JWT。

| 接口 | 行为 |
| --- | --- |
| `POST /api/v3/captures` | body 为 collector_id、expected_revision、source、format、confirmed，以及离线 content；`Idempotency-Key` 必填。HTTP 请求来自已保存草稿，不从 body 接收任意 URL/请求配置。 |
| `GET /api/v3/captures/by-key` | 携带原 `Idempotency-Key` 查询；草稿变化也可恢复，不执行 HTTP。 |
| `GET /api/v3/captures/{id}` | 当前 owner 查询快照状态与内容。 |
| `POST /api/v3/captures/{id}/json-checks` | body 为 expected_revision、step_id；从当前草稿读取规则并检查保存内容。 |

快照日志先提交，再发起有界网络访问；同 owner/key 的相同输入复用原快照，改变输入返回 409。快照完成后内容固定。获取/解析失败也返回创建成功的快照 DTO，但其中 `status=failed` 和错误阶段不代表取样成功。POST 传输异常为 `uncertain`；进程退出后超过 25 秒仍未完成的日志在查询时转为 `uncertain`，不会再次执行。

客户端连接中断或存储失败时保存原请求键，恢复后仅查询。running/uncertain 阻止直接新取样；结束本地等待需要明确确认，不撤销原请求。404 也不自动重发，可能是日志尚未创建或存储失败。幂等在本平台内防止重发，不保证外部网站的 exactly-once。

`dry_run=true` 表示零正式业务记录写入；`network_accessed=true` 表示已尝试网络，false 表示确认未尝试，null 表示执行中/崩溃恢复且尚未知。JSON 检查与离线取样始终无网络访问。

## 限制与脱敏

- 请求超时默认 10 秒、最大 15 秒，body 最大 64 KiB；查询参数最多 30 个，请求头最多 20 个。响应解码后和离线内容最多 1 MiB；外层 API 请求体最多 1 MiB + 64 KiB，JSON 转义后的传输大小也受限制。
- JSON 最多 64 层、100000 个节点；数组检查最多 20 条记录、30 个字段。JSON Pointer 使用 RFC 6901，`~0` 表示 `~`，`~1` 表示 `/`。
- 密码、Authorization、Cookie、API key、token 等已知秘密不能直接放入请求 URL、参数、body 或普通头，应使用认证头引用。
- JSON 对已知秘密键和注入凭据的精确字符串回显脱敏；HTML 删除 script/style、input 值和 textarea 内容。只保存规范化快照，不保存响应 cookies/headers 或原始离线内容日志。哈希针对保存内容；byte_count 是读取到的原内容字节数。
- 这是基础脱敏，无法自动识别任意网站的个人信息、非标准秘密键或编码后的秘密回显。输入和快照仍属于当前用户的数据；完整保留/删除策略留给 B02。
- A07 的基础 json-checks 只看 Pointer 原值；固定 JSON/HTML 提取、转换/必填/多值、共用解释器预览与浏览器快照现已由 [B01](B01-提取与即时预览.md) 接入。正式执行和样例引用继续在 B02/B04/B06 推进。

## 本轮验证

只运行短编译、离线纯单元检查和 Vue 组件编译；没有访问真实网站、浏览器或开发数据库。发布时验证新增迁移、owner 隔离/并发幂等、真实 GET/POST、凭据和恢复流程，清单见[阶段待办](采集平台v2.2接口落地与页面对接待办清单.md)。

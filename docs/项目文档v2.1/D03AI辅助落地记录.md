# D03 AI 辅助落地记录

## 范围和边界

- 采集器工作台的 AI 辅助页只接受**已保存的草稿版本和样例**。用户先查看裁剪后的发送内容、模型和 SHA，再明确勾选同意，才会发起一次请求。可输入最多 1000 字的失败说明。
- 使用配置的、兼容 Chat Completions 的**完整 HTTPS URL**（localhost 可用 HTTP）及模型。配置项 `ai_assist.enabled/base_url/api_key/model/max_requests_per_day`，环境变量前缀 `AI_ASSIST_`；默认关闭。密钥仅在服务端使用，配置日志脱敏。
- HTML 输入移除 `script/style/form/iframe` 块及常见敏感属性，再裁剪到最多 12 KB；请求超时 20 秒，输出上限 64 KB 和 1000 tokens。每日请求上限在 PostgreSQL 中串行预留；失败的外部请求也计数，避免无限重试消耗费用。用户仍须检查待发送内容是否含私密数据。
- 模型输出只允许已声明字段的 CSS/JSONPath 选择器；服务端要求选择器在选中样例中提取到非空值。模型输出和解释作为带模型、样例哈希、token 用量的 `pending_review` 建议保存。用户可接受或拒绝；接受后仅填入当前规则编辑器，必须再次保存草稿、回归样例、发布，才影响后续运行。删除草稿样例会同时清理关联建议和请求记录。
- 旧 `ai.ParseResponse` 的高置信度自动接受行为改为一律待审核。`ai_extractions` 仍是历史基础设施，**未接入正式 worker**；D03 没有实现运行时 AI 结构化提取，也不把模型输出自动写入记录。

## 验证

- `go test -timeout 120s ./...`：通过。
- `SCRAPIO_TEST_DEV_D03=1 go test -timeout 120s ./internal/repo/ai_repo -run TestDevD03SuggestionReview -count=1`：在 `get_magnet_dev` 通过，覆盖请求限额、建议持久化、人工接受、重复审核保护和删除草稿样例后的关联清理。测试清理自身创建的数据。
- `internal/ai` 模拟兼容端点测试覆盖输入裁剪、敏感 HTML 属性、请求协议、建议字段校验和 URL 约束。
- `web` 的 `npm run build`：通过；项目既有 Vite/Sass 弃用提示仍存在。
- `npx tsc --noEmit --project tsconfig.json` 仍被既有 `userInfo.ts`、`arrayOperation.ts`、`other.ts` 类型错误阻断，本阶段没有修改这些文件。

## 发版验证

真实模型端点、模型输出质量和费用需要发版后联调。工作台当前保存新草稿后不自动继承旧样例；接受 AI 建议后，应将样例重新保存到新草稿并执行回归。运行时 AI 提取仍需另行设计单次及累计费用上限、Schema 验证和人工复核工作流。

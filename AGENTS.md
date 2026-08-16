# AGENTS.md

> 本文件面向 AI 编码代理，介绍本项目的架构、约定与开发方式。阅读前无需任何背景知识。
> 更详细的设计文档见 [docs/design.md](docs/design.md)（整体方案）与 [docs/frontend-design.md](docs/frontend-design.md)（视觉与交互）。代码注释、文档、提交说明均使用中文，请保持一致。

## 项目概述

**AI 智能笔记（agent_note）**：单用户本地部署的个人 AI 笔记应用。

- 分类笔记 + Markdown 编辑（ByteMD 编辑器，gfm / 代码高亮 / mermaid / 公式，粘贴图片自动上传）
- 多 AI 配置（OpenAI 兼容：baseUrl / apiKey / model），同时仅一个激活
- 保存笔记后由后台 worker 异步调用 AI 生成元数据（简介 / 标签 / 实体），失败可重试
- AI 对话：首页全局助手检索笔记回答（关键词 + tags + 实体 ILIKE 检索，**不做 RAG / 向量**）；编辑页写作助手可通过工具直接修改当前笔记；对话为 SSE 流式
- 单管理员账号登录（env 配置），JWT 不含 exp（永不过期）

### 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Vue 3 + TypeScript + Vite + Vue Router + Pinia + Tailwind CSS v4 + ByteMD |
| 后端 | Go 1.25 + Gin + GORM + golang-jwt v5 + bwmarrin/snowflake |
| AI 调用 | 自写 OpenAI 兼容 HTTP 客户端（`internal/ai`），SSE 流式 + 工具循环 |
| 数据库 | PostgreSQL（GORM AutoMigrate 自动建表） |

## 目录结构

```
├── main.go                  # 入口：config.Load → snowflake.Init → database.Init → worker.Start → router.Setup
├── internal/
│   ├── config/              # .env 加载，全局 config.C
│   ├── database/            # GORM 连接（postgres）+ AutoMigrate，全局 database.DB
│   ├── models/              # 全部数据模型（单文件 models.go）
│   ├── handlers/            # HTTP 接口层（gin handler，按资源分文件）
│   ├── services/            # 业务逻辑（笔记查询/检索/VO 转换、AI 配置、钩子）
│   ├── ai/                  # OpenAI 兼容客户端（client.go）、提示词（prompts.go）、工具注册与执行（tools.go）、子代理运行器（subagent.go）、agent 循环收敛保障（agentloop.go）
│   ├── router/              # 路由注册（router.go）+ SPA 托管（spa.go）
│   ├── middleware/          # CORS / JWTAuth
│   ├── snowflake/           # 雪花 ID 生成（snowflake.Next()）
│   └── worker/              # 元数据生成 worker pool（2 个 goroutine，chan 队列 + sync.Map 去重；调用记录落 ai_call_logs）
├── web/                     # Vue 3 + TS 前端（Vite）
│   └── src/
│       ├── views/           # HomeView / EditorView / SettingsView / LoginView
│       ├── components/      # ChatPanel、NoteCard、CategoryNav、TopBar、GlassDropdown、AuroraBackground
│       ├── api/             # client.ts（axios 实例 + token 拦截）、sse.ts（SSE 客户端）
│       ├── stores/          # Pinia：auth / notes / categories / aiConfigs / chat
│       ├── styles/          # bytemd-glass.css（ByteMD 玻璃拟态覆写）
│       └── types/           # bytemd.d.ts（ByteMD 官方 d.ts 缺陷的本地补全，勿删）
├── docs/                    # 设计文档
├── uploads/                 # 上传图片（UPLOAD_DIR，不入库 git）
├── logs/                    # 运行日志（LOG_DIR）
└── Dockerfile               # 三阶段：前端构建 → Go 静态编译 → alpine 运行
```

注：`app/` 目前为空目录；仓库根部的 `*.exe` / `*.exe~` / `server.log` 是本地构建与运行产物，已 gitignore。

## 构建与运行

### 后端

```bash
cp .env.example .env   # 配置数据库连接、JWT_SECRET、ADMIN_USERNAME / ADMIN_PASSWORD
go run .               # 监听 :7562（SERVER_PORT 可改），首次启动自动建表
go build -o server .   # 生产构建
go vet ./...           # 静态检查
```

要求：Go 1.25+，可用的 PostgreSQL（需先手动 `CREATE DATABASE db;`，表结构自动迁移）。

### 前端（web/）

```bash
cd web
npm install
npm run dev       # http://localhost:5173，已代理 /api 和 /uploads 到 :7562
npm run build     # vue-tsc -b && vite build，产物在 web/dist（含类型检查）
npm run preview
```

### 生产模式

`web/dist` 存在时，后端直接托管前端（`internal/router/spa.go`：`NoRoute` 回退 index.html，`/api`、`/uploads` 除外，`/assets/` 长缓存）。单进程即可运行，无需独立静态服务器。

### Docker

```bash
docker build -t agent-note .
docker run -d -p 7562:7562 \
  -e DB_HOST=... -e DB_PORT=5432 -e DB_USER=postgres -e DB_PASSWORD=... \
  -e DB_NAME=db -e DB_SCHEMA=public \
  -e JWT_SECRET=... -e ADMIN_USERNAME=admin -e ADMIN_PASSWORD=... \
  -v agent-note-uploads:/app/uploads agent-note
```

镜像内含前端产物与静态编译的后端二进制（CGO 关闭），PostgreSQL 外置。前端构建阶段必须用 glibc 镜像（node:22-bookworm-slim），rolldown / lightningcss 原生绑定不支持 musl。

## 硬性技术约束（修改代码时必须遵守）

1. **HTTP 接口只允许 GET / POST**。写操作统一走子路径：`/api/xxx/create`、`/api/xxx/update`、`/api/xxx/delete`；不用 PUT / DELETE。路由集中在 `internal/router/router.go`。
2. **数据库无外键约束**（`DisableForeignKeyConstraintWhenMigrating: true`）；**所有表带 `is_active INT DEFAULT 1`，只做软删除**，查询一律带 `is_active = 1`；级联清理由应用层事务完成（参考 `handlers/note.go` 的 DeleteNote）。非必要唯一索引降级为普通索引，重名 / 关联冲突在应用层处理（如 worker 里按 name 复用 tag / entity）。
3. **主键为雪花 ID（int64）**，由后端 `snowflake.Next()` 生成，不自增。**返回前端一律序列化为字符串**（模型 tag 用 `json:"id,string"`），前端入参同样以字符串提交（int64 会超出 JS `Number.MAX_SAFE_INTEGER`）。
4. **错误响应统一 `{"error": "中文消息"}`**，成功响应直接返回业务对象或 `{"ok": true}`；列表接口返回 `{"total": n, "items": [...]}`。
5. **AI 服务商地址不写死**，一律从 `ai_configs` 表激活配置读取（`services.GetActiveAIConfig()`）。
6. 笔记元数据生成与 HTTP 请求解耦：保存只落库 + 调用 `services.OnNoteContentChanged(noteID)` 钩子（worker 启动时注入 `worker.Enqueue`）；仅内容 hash 变化才重新生成（`MetaContentHash` 判重）。`meta_status` 状态机：`none|pending|processing|done|failed`。

## 代码风格约定

- 注释、日志、错误消息、设计文档均使用**中文**；标识符用英文。日志带包前缀，如 `[worker] note %d meta done`。
- Go：标准 gofmt；handler 保持「解析参数 → 调用 service / 直接 GORM → 返回 JSON」的瘦结构，复杂业务放 `services`。GORM 用 `Model(&models.X{}).Where(...)` 链式调用 + `Updates(map[string]any{...})`。
- 新增 AI 工具：在 `internal/ai/tools.go` 的 `init()` 里 `register`，并把工具名加入 `toolOrder`；写作类工具设 `Writing: true`（仅编辑页绑定笔记的会话可用）。子代理（run_subagent）可用工具由 `SubAgentTools()` 单独圈定（只读检索类），实现见 `internal/ai/subagent.go`。聊天 agent 循环在 `handlers/chat.go`：不设固定轮数上限，收敛靠 `internal/ai/agentloop.go` 的停滞检测（连续相同调用 → 无工具强制收尾）与上下文压缩（`CompactLoopMessages`，以上一轮接口返回的输入 token 超 400K 为触发，无 usage 时按字符数兜底）；token 用量归一化（输入/输出/缓存/思考）后随 assistant 消息落库。会话历史按总字数预算回溯，不按条数截断。
- 前端：组合式 API + `<script setup lang="ts">`；接口请求统一走 `web/src/api/client.ts` 的 axios 实例（自动带 Bearer token，401 自动跳登录）；SSE 走 `web/src/api/sse.ts`。雪花 ID 在前端始终以 `string` 处理。
- 前端视觉体系（深色「夜墨」默认 / 浅色「晨雾」，青玉色 = AI、靛蓝 = 用户）见 `docs/frontend-design.md`，改 UI 前先读它。

## 测试

后端各包已有不依赖数据库的单元测试（`*_test.go`，如 `internal/ai/tools_test.go`）；触库代码（`services.ListNotes/SearchNotesForAI`、`worker.process`、多数 handler 的成功路径）暂不覆盖，保持零新依赖、不起真实数据库。前端用 Vitest + happy-dom（`web/src/**/*.test.ts`），覆盖 SSE 解析、chat/auth store 与纯函数。变更后的验证方式：

- 后端：`go build ./...` 编译通过；`go vet ./...` 与 `go test ./...` 全绿；`go run .` 启动后观察日志（`[database] connected & migrated`、`[worker] started 2 workers`）。
- 前端：`cd web && npm run test`（vitest run）全绿；`npm run build` 会跑 `vue-tsc` 类型检查，必须无错。
- 接口：用 curl / 前端页面手动冒烟（登录 → 建笔记 → AI 元数据生成 → AI 对话）。
- 调试：`.env` 设 `DEBUG=true` 可输出全部 SQL 语句和参数（GORM logger Info 级）。

## 安全注意事项

- ** secrets 只放 `.env`**（已 gitignore），`.env.example` 仅作模板；不要把真实 API key / 密码提交进仓库。AI 配置的 `api_key` 存数据库，接口返回时用 `MaskedKey()` 脱敏（前 3 后 4）。
- JWT 签名密钥来自 `JWT_SECRET`，默认值 `dev-secret-please-change` 会在启动时打 WARNING；生产必须修改。JWT 不含 exp（永不过期），这是已确认的设计决策。
- 单管理员账号由 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 环境变量定义，无注册功能。
- CORS 中间件目前 `Access-Control-Allow-Origin: *`（面向开发环境 vite dev server），生产部署时留意。
- 上传限制：仅图片扩展名（png/jpg/jpeg/gif/webp/svg）、10MB 上限，文件名用雪花 ID 重写后存 `UPLOAD_DIR`，经 `/uploads` 静态服务暴露（**无鉴权**）。
- SPA 托管路径拼接使用 `filepath.Clean("/"+p)` 防目录逃逸，改动 `spa.go` 时保留该防护。

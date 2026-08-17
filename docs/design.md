# AI 智能笔记 · 整体设计文档

> 版本 v1.0 · 2026-08-16 · 需求已确认，待实施
> 前端视觉与交互设计详见 [frontend-design.md](./frontend-design.md)

## 1. 项目概述

个人 AI 笔记，单用户本地部署。核心能力：

- **分类笔记 + Markdown 编辑**：单层分类，ByteMD 编辑器
- **多 AI 配置**：OpenAI 兼容（baseUrl / apiKey / model），可配多个，同时仅一个激活
- **保存后异步生成元数据**：AI 提取简介（summary）、标签（tags）、实体（entities），用于检索
- **AI 对话**：搜索笔记（关键词 + tags + 实体检索，**不做 RAG**）、辅助写作（AI 产出修改提案，用户在前端 diff 审核接受后才落库）

## 2. 已确认决策与默认规则

| 项 | 决策 |
|---|---|
| 分类结构 | 单层平铺，笔记可属于一个分类（可空 = 未分类） |
| Markdown 编辑器 | ByteMD（`@bytemd/vue-next` + gfm/highlight/mermaid/math 插件） |
| AI 工具循环 | 后端执行（Go 调 AI + 执行工具 + 多轮循环），SSE 推送 |
| AI 对话历史 | 持久化；编辑页对话绑定笔记（note_id），首页为独立会话 |
| 用户体系 | 单管理员账号（env `ADMIN_USERNAME` / `ADMIN_PASSWORD`），无注册 |
| 登录 token | JWT **不含 exp**（永不过期）；改密码后旧 token 仍有效，可接受 |
| 元数据生成时机 | 内容 hash 变化才重新生成；失败可手动重试 |
| 图片 | 编辑器粘贴自动上传到 UPLOAD_DIR |
| 主题 | 深 / 浅切换，默认深色 |
| 列表搜索框 | 调接口按关键词过滤（含 tag / 实体命中） |
| 检索 | ILIKE + 关联查询，不做向量；已建 pg_trgm 扩展与 notes(title/content_md) GIN 索引（启动时容错 `CREATE EXTENSION IF NOT EXISTS` + 建索引，失败仅警告不中断启动） |
| 新建笔记 | 标题留空时自动取正文首行；删除笔记级联删除其绑定会话 |
| 消息存储 | `user / assistant / tool` 三类（含 tool_calls JSONB），支持上下文回放 |
| AI 过程提示 | 前端显示工具执行状态（"正在搜索笔记…"、"正在修改内容…"） |
| **HTTP 接口** | **只允许 GET / POST**，不用 PUT / DELETE；写操作统一 `/xxx/create`、`/xxx/update`、`/xxx/delete` 子路径 |
| **数据库约束** | **所有表无外键约束**；所有表带 `is_active INT DEFAULT 1`，**只做软删除**（查询一律 `is_active = 1`）；**非必要唯一索引一律降级为普通索引** |
| **主键 ID** | **雪花算法（Snowflake, int64）**，由后端统一生成，不自增；**返回前端时序列化为字符串**（int64 可能超出 JS `Number.MAX_SAFE_INTEGER`，会精度丢失），前端入参同样以字符串提交 |

## 3. 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Vue 3.5 + TypeScript + Vite + Vue Router + Pinia + Tailwind CSS v4 + ByteMD |
| 后端 | Go + Gin + GORM + golang-jwt |
| AI 调用 | 自写 OpenAI 兼容 HTTP 客户端（任意 baseUrl 通用），SSE 流式 |
| 数据库 | PostgreSQL |

## 4. 系统架构

```
┌─────────────┐   HTTP/SSE    ┌────────────────────────────────┐
│  浏览器 SPA  │ ────────────▶ │ Gin 服务                       │
│  Vue 3 + TS │ ◀──────────── │  ├─ middleware  鉴权/CORS/日志   │
└─────────────┘               │  ├─ handlers    资源接口        │
                              │  ├─ services    笔记/检索/元数据  │
                              │  ├─ ai          OpenAI兼容客户端 │
                              │  │               agent循环+工具  │
                              │  └─ worker      元数据生成池     │
                              └───────┬───────────────┬────────┘
                                GORM  │               │ HTTPS (OpenAI 兼容 API)
                              ┌───────▼──────┐  ┌─────▼──────────┐
                              │  PostgreSQL  │  │ 任意 AI 服务商  │
                              │ (元数据/会话) │  │ (当前激活配置)   │
                              └──────────────┘  └────────────────┘
```

要点：

- AI 服务商地址不写死，全部来自 `ai_configs` 表中激活的配置
- 元数据生成与 HTTP 请求解耦：保存笔记只落库 + 入队，worker 后台处理
- 聊天接口为 SSE 长连接：文本增量 + 工具调用事件 + 笔记更新事件同一条流下发

## 5. 页面总览

```
┌─────────────────────────────────────────────────────────────┐
│                      背景（毛玻璃画布，左右留白）               │
│   ┌───────────────────────────────────────────────────────┐ │
│   │ 顶栏：Logo | 搜索框 | ＋新增笔记 | ⚙设置                  │ │
│   ├──────────┬──────────────────────────┬─────────────────┤ │
│   │ 分类导航   │ 笔记列表                  │ AI 对话栏         │ │
│   └──────────┴──────────────────────────┴─────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

| 路由 | 页面 |
|---|---|
| `/login` | 登录页 |
| `/` | 首页：分类导航 + 笔记列表 + 全局 AI 对话 |
| `/note/new` | 新建笔记（直接进入编辑模式） |
| `/note/:id` | 笔记页：**默认预览模式**（ByteMD Viewer），点「编辑」进入编辑模式（ByteMD Editor）；右侧为绑定当前笔记的 AI 对话 |
| `/settings` | 设置页：AI 配置管理 |

视觉规格（配色、组件、动效）见 [frontend-design.md](./frontend-design.md)。

## 6. 数据模型

```sql
-- 公共字段说明：所有表均含
--   id         BIGINT PRIMARY KEY       -- 雪花 ID（Snowflake, int64），应用层生成，非自增
--   is_active  INT NOT NULL DEFAULT 1   -- 软删除标记：1=有效 0=已删除
-- 所有查询一律携带 is_active = 1；不使用外键约束，级联清理在应用层完成。
-- 主键与所有外键引用字段（category_id / note_id / tag_id / entity_id / conversation_id）均为 BIGINT 雪花 ID。

-- 分类（单层）
CREATE TABLE categories (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  name       VARCHAR(64) NOT NULL,
  sort       INT NOT NULL DEFAULT 0,
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_categories_name   ON categories(name);
CREATE INDEX idx_categories_active ON categories(is_active);

-- 笔记
CREATE TABLE notes (
  id                BIGINT PRIMARY KEY,                                -- 雪花 ID
  title             TEXT NOT NULL DEFAULT '',
  content_md        TEXT NOT NULL DEFAULT '',
  category_id       BIGINT,                                            -- NULL=未分类；无外键
  summary           TEXT NOT NULL DEFAULT '',                          -- AI 生成简介
  meta_status       VARCHAR(16) NOT NULL DEFAULT 'none',  -- none|pending|processing|done|failed
  meta_error        TEXT NOT NULL DEFAULT '',
  meta_content_hash VARCHAR(64) NOT NULL DEFAULT '',     -- 内容 hash，判断是否需要重新生成
  is_active         INT NOT NULL DEFAULT 1,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notes_category ON notes(category_id, is_active);
CREATE INDEX idx_notes_updated  ON notes(is_active, updated_at DESC);

-- 标签（AI 生成，name 去重靠应用层）
CREATE TABLE tags (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  name       VARCHAR(64) NOT NULL,
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tags_name   ON tags(name);
CREATE INDEX idx_tags_active ON tags(is_active);

-- 笔记-标签关联
CREATE TABLE note_tags (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  note_id    BIGINT NOT NULL,
  tag_id     BIGINT NOT NULL,
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_note_tags_note ON note_tags(note_id, is_active);
CREATE INDEX idx_note_tags_tag  ON note_tags(tag_id,  is_active);

-- 实体（AI 生成，type: person|organization|location|technology|product|event|other）
CREATE TABLE entities (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  name       VARCHAR(128) NOT NULL,
  type       VARCHAR(32) NOT NULL DEFAULT 'other',
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_entities_name   ON entities(name);
CREATE INDEX idx_entities_active ON entities(is_active);

-- 笔记-实体关联
CREATE TABLE note_entities (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  note_id    BIGINT NOT NULL,
  entity_id  BIGINT NOT NULL,
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_note_entities_note   ON note_entities(note_id,   is_active);
CREATE INDEX idx_note_entities_entity ON note_entities(entity_id, is_active);

-- AI 配置（多配置，仅一个激活；active 为激活标记，语义唯一性由应用层保证）
CREATE TABLE ai_configs (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  name       VARCHAR(64) NOT NULL,
  base_url   VARCHAR(256) NOT NULL,
  api_key    TEXT NOT NULL,
  model      VARCHAR(128) NOT NULL,
  active     INT NOT NULL DEFAULT 0,       -- 激活标记：1=当前激活（至多一条）
  is_active  INT NOT NULL DEFAULT 1,       -- 软删标记
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ai_configs_active ON ai_configs(active, is_active);

-- AI 会话（note_id 非空 = 绑定笔记的写作会话，NULL = 首页全局会话）
CREATE TABLE conversations (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  note_id    BIGINT,                                                 -- 无外键
  title      VARCHAR(128) NOT NULL DEFAULT '新对话',
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_conversations_note   ON conversations(note_id, is_active);
CREATE INDEX idx_conversations_active ON conversations(is_active, updated_at DESC);

-- 消息（含工具消息，支持上下文回放）
CREATE TABLE messages (
  id              BIGINT PRIMARY KEY,      -- 雪花 ID
  conversation_id BIGINT NOT NULL,
  role            VARCHAR(16) NOT NULL,          -- user | assistant | tool
  content         TEXT NOT NULL DEFAULT '',
  tool_calls      JSONB,                         -- assistant 消息的工具调用（原样回放）
  reasoning       TEXT,                          -- assistant 思考内容（推理模型）
  usage           JSONB,                         -- assistant 该轮 token 用量（归一化：输入/输出/合计/缓存命中/思考）
  tool_call_id    VARCHAR(64),                   -- tool 消息归属
  name            VARCHAR(64),                   -- tool 消息的工具名
  is_active       INT NOT NULL DEFAULT 1,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_messages_conv ON messages(conversation_id, is_active, id);

-- AI 调用记录（非会话类调用，如元数据提取；会话类调用的用量随 messages.usage 落库）
CREATE TABLE ai_call_logs (
  id          BIGINT PRIMARY KEY,      -- 雪花 ID
  kind        VARCHAR(32) NOT NULL,          -- 调用类型：meta_extract 等
  note_id     BIGINT,                        -- 关联笔记（元数据提取）
  model       VARCHAR(128) NOT NULL DEFAULT '',
  attempt     INT NOT NULL DEFAULT 1,        -- 同一任务内的重试序号
  usage       JSONB,                         -- 归一化 token 用量（输入/输出/合计/缓存命中/思考）
  success     BOOLEAN NOT NULL DEFAULT FALSE,
  error       TEXT NOT NULL DEFAULT '',
  duration_ms BIGINT NOT NULL DEFAULT 0,
  is_active   INT NOT NULL DEFAULT 1,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ai_call_logs_kind ON ai_call_logs(kind, is_active);

-- 图片上传
CREATE TABLE uploads (
  id         BIGINT PRIMARY KEY,           -- 雪花 ID
  filename   VARCHAR(256) NOT NULL,
  path       VARCHAR(512) NOT NULL,
  size       BIGINT NOT NULL DEFAULT 0,
  mime       VARCHAR(64) NOT NULL DEFAULT '',
  is_active  INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

说明：

- **主键为雪花 ID**（Snowflake, int64），由应用层 `snowflake` 包统一生成，非自增；node id 从 env `SNOWFLAKE_NODE` 读取（单机部署默认 1）
- 不建 users 表，账号来自环境变量（单用户）
- **无外键约束**：删除笔记 / 分类 / 会话 / 配置时的级联清理（见下）全部在应用层事务内完成
- **软删除**：所有表 `is_active INT DEFAULT 1`，删除 = 置 0；所有查询强制 `is_active = 1`
- **唯一性语义靠应用层**：
  - 「同一时间仅一个激活的 AI 配置」：`ai_configs.active` 为激活标记（与软删标记 `is_active` 区分）；激活操作在同一事务内执行 `UPDATE ai_configs SET active=0 WHERE active=1 AND id != $1` + `UPDATE ... SET active=1 WHERE id=$1`（双重校验）
  - tags / entities 按 name 去重：插入前先按 name 查询，命中则复用 id（单用户并发低，查询后插入足够）
- **级联清理规则**（应用层，均在事务内软删）：
  - 删除分类 → 其下笔记 `category_id` 置 NULL
  - 删除笔记 → 软删其 note_tags / note_entities / 绑定会话（及消息）
  - 删除会话 → 软删其消息
  - 删除 AI 配置 → 若它是激活配置，删除后系统处于无激活状态，直到用户另行激活

### 6.1 ID 与前端交互

- 雪花 ID 为 int64（最大约 9.22×10¹⁸），**超出 JS `Number.MAX_SAFE_INTEGER`（2⁵³−1 ≈ 9×10¹⁵）**，直接用 Number 会精度丢失
- **后端返回前端的所有 ID 一律序列化为字符串**（`"id": "123456789012345678"`），引用字段（category_id / note_id / tag_id / entity_id / conversation_id）同样为字符串
- 前端请求体中的 ID 参数同样以字符串提交，后端解析为 int64
- Go 模型定义示例：

```go
type Note struct {
    ID         int64 `json:"id,string"`
    CategoryID int64 `json:"category_id,string"`
    ...
}
```

## 7. API 设计

统一前缀 `/api`，除登录与静态文件外全部需要 `Authorization: Bearer <token>`。

**HTTP 方法约束**：仅使用 **GET**（查询）与 **POST**（一切写操作）；不用 PUT / DELETE / PATCH。写操作统一命名：

- `POST /api/xxx/create` — 新建
- `POST /api/xxx/update` — 修改（id 在请求体）
- `POST /api/xxx/delete` — 删除（id 在请求体，软删除）
- `POST /api/xxx/<action>` — 其他动作（activate / test / regenerate …）

### 7.1 认证

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/login` | `{username, password}` → `{token, username}`；失败 401 |
| GET | `/api/auth/me` | → `{username}` |

### 7.2 分类

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/categories` | → `[{id, name, sort, note_count}]` |
| POST | `/api/categories/create` | `{name}` |
| POST | `/api/categories/update` | `{id, name}` |
| POST | `/api/categories/delete` | `{id}`；其下笔记置为未分类 |

### 7.3 笔记

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/notes?category_id&keyword&tag&entity&page&page_size` | 列表 → `{total, items: [NoteListItem]}` |
| POST | `/api/notes/create` | `{title?, content, category_id?}` → Note；触发异步生成 |
| GET | `/api/notes/:id` | 全文 → Note |
| POST | `/api/notes/update` | `{id, title, content, category_id}`；内容 hash 变化 → 重新生成元数据 |
| POST | `/api/notes/delete` | `{id}`；软删笔记并级联软删绑定会话 |
| POST | `/api/notes/batch/delete` | `{ids: string[]}`；批量软删，级联同上单篇删除 |
| POST | `/api/notes/batch/move` | `{ids: string[], category_id: string \| null}`；批量移动分类，null = 未分类 |
| POST | `/api/notes/meta/regenerate` | `{id}`；手动重新生成元数据 |

```
NoteListItem = { id: string, title, summary, meta_status, tags: string[], entities: [{name, type}], updated_at }
Note         = NoteListItem + { content_md, category_id: string | null, meta_error, created_at }
-- 所有 ID 字段均为字符串（雪花 int64 序列化为 string，避免 JS Number 精度丢失）
```

### 7.4 AI 配置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ai-configs` | 列表，api_key 脱敏返回（前 3 后 4，如 `sk-***abcd`） |
| POST | `/api/ai-configs/create` | `{name, base_url, api_key, model}` |
| POST | `/api/ai-configs/update` | `{id, name, base_url, api_key, model}` |
| POST | `/api/ai-configs/delete` | `{id}`；若删除的是激活配置，激活状态自动失效 |
| POST | `/api/ai-configs/activate` | `{id}`；设为激活（同一事务内先将其他全部置 0，再置当前为 1） |
| POST | `/api/ai-configs/test` | `{id}`；发一个最小 chat 请求验证连通性 → `{ok, error?}` |

### 7.5 会话与聊天

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/conversations?note_id=` | 会话列表（按 updated_at 倒序） |
| POST | `/api/conversations/create` | `{note_id?}` → conversation |
| POST | `/api/conversations/delete` | `{id}`；软删会话并级联软删消息 |
| GET | `/api/conversations/:id/messages` | 历史消息（tool 消息前端折叠展示） |
| POST | `/api/chat` | **SSE**，见下 |

### 7.6 SSE 事件协议（`POST /api/chat`）

请求体：`{conversation_id?: string, note_id?: string, content: string}`（ID 均为字符串）。无 conversation_id 时后端自动创建会话（note_id 绑定传入的笔记）。

| 事件 | data | 说明 |
|---|---|---|
| `meta` | `{"conversation_id":"123…","user_message_id":"456…"}` | 首帧，回传会话/消息 id（字符串） |
| `delta` | `{"content":"…"}` | assistant 文本增量 |
| `tool_start` | `{"name":"search_notes","input":{…}}` | 开始执行工具 |
| `tool_end` | `{"name":"search_notes","ok":true,"summary":"找到 3 条笔记"}` | 工具结束（summary 供状态行展示） |
| `note_updated` | `{"note_id":"789…"}` | 直写场景（标题修改、新建笔记等）落库后，前端刷新编辑器（字符串） |
| `note_proposal` | `{"note_id":"789…","tool":"replace_note_section","content":"<完整新正文>"}` | 正文修改提案（replace_note_section / append_note_content 产出，不落库）；前端弹出行级 diff 审核，「接受」后走 `/api/notes/update` 落库，「拒绝」丢弃 |
| `done` | `{"conversation_id":"123…"}` | 本轮结束 |
| `error` | `{"message":"…"}` | 出错终止 |

agent 循环期间每 15s 额外发送一行 SSE 注释心跳（`: ping\n\n`），防止中间层将长循环判定为空闲断连；前端解析器天然忽略注释行。

### 7.7 上传

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/uploads` | multipart file → `{url}`（`/uploads/xxx.png`） |
| GET | `/uploads/**` | 静态文件服务 |

## 8. AI 能力

### 8.1 配置与模型选择

- 所有 AI 调用（聊天、元数据生成、测试连接）一律读取**当前激活的** `ai_configs`
- 无激活配置时：聊天接口返回 `error` 事件（"未配置 AI"），元数据生成标记 `failed` 并记录原因
- 切换激活配置后，新消息即用新模型；历史会话上下文不变

### 8.2 工具定义（发给模型的 function definitions）

**检索类**

```json
{
  "name": "search_notes",
  "description": "在笔记库中搜索笔记。按关键词、标签、实体名检索，返回匹配笔记的简介列表。用户想查找、回忆过往笔记时必须先调用本工具。",
  "parameters": {
    "type": "object",
    "properties": {
      "keywords": {"type": "array", "items": {"type": "string"}, "description": "搜索关键词，可多个"},
      "tags":     {"type": "array", "items": {"type": "string"}, "description": "按标签过滤，可为空"},
      "entities": {"type": "array", "items": {"type": "string"}, "description": "按实体名过滤，可为空"},
      "limit":    {"type": "integer", "default": 10, "description": "返回条数上限"}
    },
    "required": ["keywords"]
  }
}
```

```json
{
  "name": "get_note",
  "description": "获取指定笔记的完整正文。search_notes 只返回简介，确认相关后调用本工具读取全文。",
  "parameters": {
    "type": "object",
    "properties": {"note_id": {"type": "string"}},
    "required": ["note_id"]
  }
}
```

```json
{
  "name": "list_categories",
  "description": "列出全部笔记分类，用于新建笔记时选择分类。",
  "parameters": {"type": "object", "properties": {}}
}
```

```json
{
  "name": "list_all_notes",
  "description": "获取笔记库中全部笔记的概览列表：标题、简介、标签、实体、创建与修改时间（不含正文）。用户想纵览全库、按时间或标签整体梳理时使用；需要正文再调 get_note。",
  "parameters": {"type": "object", "properties": {}}
}
```

**委派类**

```json
{
  "name": "run_subagent",
  "description": "委派一个子代理处理需要阅读大量笔记的长上下文任务（如全库总结、多篇笔记对比归纳）。子代理拥有独立上下文，可自行检索并通读笔记全文，完成后返回精炼结论；它只读不写。当任务需要通读多篇笔记、直接处理会让对话上下文过长时，优先使用本工具。",
  "parameters": {
    "type": "object",
    "properties": {
      "task": {"type": "string", "description": "交给子代理的任务描述，需自包含（目标、范围、期望产出）"}
    },
    "required": ["task"]
  }
}
```

**写作类**（编辑页会话可用）

```json
{
  "name": "replace_note_section",
  "description": "用 new_text 精确替换笔记中的 old_text 片段。old_text 必须与原文一字不差（从 get_note 结果中逐字复制），否则替换失败。",
  "parameters": {
    "type": "object",
    "properties": {
      "note_id":  {"type": "string"},
      "old_text": {"type": "string"},
      "new_text": {"type": "string"}
    },
    "required": ["note_id", "old_text", "new_text"]
  }
}
```

```json
{
  "name": "append_note_content",
  "description": "在笔记末尾追加内容。",
  "parameters": {
    "type": "object",
    "properties": {"note_id": {"type": "string"}, "content": {"type": "string"}},
    "required": ["note_id", "content"]
  }
}
```

```json
{
  "name": "update_note_title",
  "description": "修改笔记标题。",
  "parameters": {
    "type": "object",
    "properties": {"note_id": {"type": "string"}, "title": {"type": "string"}},
    "required": ["note_id", "title"]
  }
}
```

```json
{
  "name": "create_note",
  "description": "新建一篇笔记（如 AI 产出一篇完整文章时）。",
  "parameters": {
    "type": "object",
    "properties": {
      "title":       {"type": "string"},
      "content":     {"type": "string"},
      "category_id": {"type": "string", "description": "可选，用 list_categories 查询"}
    },
    "required": ["title", "content"]
  }
}
```

**工具执行规则**

- 工具在 Go 侧执行；执行结果作为 `tool` 消息回传模型继续循环，直到模型不再产生 tool_calls
- **正文修改走提案审核制**：`replace_note_section` / `append_note_content` 不直接写库，执行后产出 `NoteProposal{NoteID, Tool, NewContent}`（修改后的完整正文），经 SSE `note_proposal` 事件推给前端；前端弹出行级 diff（DiffReviewModal），用户「接受」后走常规 `POST /api/notes/update` 落库（天然复用「保存 → 元数据重新生成」链路），「拒绝」则丢弃。`update_note_title` / `create_note` 保持直写，仍推 `note_updated` 事件
- 写作工具执行时服务端强制校验参数 note_id 必须等于会话绑定的笔记（`ai.WithBoundNoteID` 注入 ctx），越权修改其他笔记直接报错
- agent 循环不设固定轮数上限（超限报错是让用户为工程问题买单），收敛由工程手段保证（`internal/ai/agentloop.go`）：
  - 停滞检测：连续 3 轮完全相同的工具调用判定为模型卡死，下一轮改为不带工具调用，强制模型基于已有信息收尾
  - 上下文压缩：上一轮接口返回的输入 token 数超过 400K 预算时（当前主流模型上下文已达 1M，400K 以内无压力），较早的 tool 结果替换为占位说明（保留最近 4 条原文），被省略的内容模型可重新调用工具获取；服务商未返回 usage 时按字符数兜底判断
- `run_subagent` 启动只读子代理：独立消息上下文，仅可用 search_notes / get_note / list_all_notes（`ai.SubAgentTools()`，不含写作工具与 run_subagent 自身以防递归），且**服务端执行前强制白名单校验**（白名单外调用不执行，直接返回错误工具结果并记 `[subagent]` 日志，不只靠提示词约束），收敛机制与主循环相同；最终结论原样作为工具结果回传主代理；子代理的 AI 调用随主 SSE 连接的 ctx 一并取消

### 8.3 系统提示词

**首页全局助手**

```
你是一个个人笔记库的 AI 助手。笔记库中的每篇笔记有：标题、简介(summary)、标签(tags)、实体(entities)和正文(markdown)。
规则：
1. 用户的问题涉及查找、回忆、总结过往笔记时，必须先调用 search_notes 检索，不得凭空说"你的笔记里没有"。
2. search_notes 返回的只是简介。先根据简介判断相关性，再对相关笔记调用 get_note 读取正文后回答。
3. 用户想纵览全部笔记（如"我都有哪些笔记"、按时间/标签整体梳理）时，调用 list_all_notes。
4. 需要通读多篇笔记全文的繁重任务（如全库总结、多篇笔记对比归纳），调用 run_subagent 委派子代理处理，避免大量原文挤占对话上下文。
5. 引用笔记内容时说明笔记标题。
6. 确实没有找到相关笔记时如实告知，并说明用了什么关键词搜索。
```

**写作助手**（编辑页，动态注入当前笔记 id，字符串形式）

```
你是一个写作助手，帮助用户撰写和修改当前正在编辑的笔记（note_id="{id}"）。
可用工具：
- get_note：查看笔记当前内容
- replace_note_section：精确替换笔记中的某段文字（old_text 必须与原文一字不差）
- append_note_content：在笔记末尾追加内容
- update_note_title：修改标题（立即生效）
- create_note：新建一篇笔记（立即生效）
- search_notes / get_note：检索其他笔记作为参考资料
- list_all_notes：查看全库笔记概览
- run_subagent：委派子代理处理需要通读大量笔记的长上下文任务
规则：
1. 用户要求"写一段/改写/扩写/润色"时，直接对笔记本身发起修改，而不是只在对话里给出文字。
2. 使用 replace_note_section 前先调用 get_note，old_text 从原文逐字复制。
3. replace_note_section / append_note_content 的修改不会直接写入笔记，而是作为提案提交给用户审核，用户接受后才生效。因此绝对不要声称"已保存""已写入""修改已完成"，只能说明"修改已提交给用户审核"。
4. 修改提交后简要说明改了什么。
```

**子代理**（run_subagent 委派，只读）

```
你是一个子任务代理，由笔记助手主代理委派，负责处理需要大量阅读笔记的长上下文任务（如全库梳理、多篇笔记归纳对比）。
可用工具：search_notes（检索笔记）、get_note（读取笔记全文）、list_all_notes（全库概览）。
规则：
1. 围绕主代理交给你的任务自主规划检索与阅读，不要向主代理索取更多背景。
2. 你只读不写：不要尝试修改或创建任何笔记。
3. 完成后直接输出结论本身（要点、归纳、清单等），不要复述任务，不要提及"子代理"等元信息。
4. 结论必须自包含：主代理只能看到你的最终文字，看不到你的检索过程。
```

**元数据提取**（worker 使用，通过工具调用返回结构化结果，**不依赖提示词约束 JSON 输出**）

```
你是一个笔记元数据提取器。分析用户给你的笔记，然后调用 submit_note_metadata 工具提交提取结果：
- summary：1-3 句话概括笔记内容
- tags：3-8 个简洁标签，覆盖主题和类型
- entities：真实存在的专有名词（人物、组织、地点、技术、产品、事件等）
必须通过工具调用返回结果，不要用普通文本回复。
```

配套工具 `submit_note_metadata(summary, tags[], entities[{name, type}])`（entities.type 用 JSON Schema enum 约束：person|organization|location|technology|product|event|other）。

调用规则（`ChatToolCall`）：

1. 请求带 `tool_choice: {"type":"function","function":{"name":"submit_note_metadata"}}` 强制调用
2. 服务商不支持 tool_choice（HTTP 400/422）→ 降级为仅靠提示词引导重试一次
3. 模型最终未发起工具调用 → 返回 `ErrNoToolCall`，worker 重试，**最多 3 次**
4. 工具参数 JSON 解析失败同样重试
5. 每次调用（含每次重试）落一条 `ai_call_logs` 记录：model、attempt、归一化 usage（JSONB）、success、error、duration_ms；写库失败仅打日志，不影响元数据主流程

### 8.4 上下文管理

- 每次请求从最新消息开始按总字数预算（6 万字符）回溯会话历史，按时间序组装为 OpenAI 消息数组；不按固定条数截断，至少保留最新一条
- `assistant` 的 `tool_calls` 存 JSONB，回放时原样携带；`tool` 消息带 `tool_call_id`
- 循环内上下文过长由 `CompactLoopMessages` 压缩较早 tool 结果处理（见 8.2 工具执行规则）
- 流式请求带 `stream_options.include_usage`；每轮 token 用量归一化后（输入 / 输出 / 合计 / 缓存命中 / 思考）随 assistant 消息落库（`messages.usage` JSONB）；子代理累计用量写入 run_subagent 工具结果并打 `[subagent]` 日志
- 每轮完成后将 user / assistant / tool 消息全部落库

## 9. 关键流程

### 9.1 登录鉴权

1. `POST /api/auth/login`：与 env 中 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 比对（`crypto/subtle` 常量时间比较）
2. 通过后签发 JWT（HS256，claims：`{sub: username}`，**不设 exp**）
3. 中间件校验 `Authorization: Bearer`；除 `/api/auth/login` 与 `/uploads/**` 外全部保护
4. 前端 token 存 localStorage，axios 拦截器 401 → 跳登录页

### 9.2 保存笔记 → 异步生成元数据

```
保存笔记(POST /api/notes/create 或 /api/notes/update)
   │  计算 sha256(content_md)
   ├─ hash 与 meta_content_hash 相同 ──▶ 不重新生成（只更新标题/分类/时间）
   └─ hash 变化
        ├─ 更新 meta_content_hash
        ├─ meta_status = 'pending'
        └─ 投递到 worker channel
              │
   worker pool（2 个 goroutine，按 note_id 去重）
        ├─ 取当前激活配置；无配置 → failed（"未设置激活的 AI 配置"）
        ├─ meta_status = 'processing'
        ├─ 调 AI：submit_note_metadata 工具调用（tool_choice 强制，不支持则降级），
        │       模型未调用工具 / 参数解析失败 → 重试，最多 3 次
        ├─ 事务：应用层 upsert tags/entities（先按 name 查 is_active=1，命中复用 id），
        │       软删旧 note_tags/note_entities → 插入新关联，
        │       更新 summary，meta_status = 'done'
        └─ 失败 → meta_status = 'failed' + meta_error
```

- 服务启动时扫描 `pending` / `processing` 笔记恢复任务（processing 重置为 pending）
- 生成完成前，列表搜索框仍按标题/正文关键词降级检索
- 手动重试：`POST /api/notes/meta/regenerate` `{id}` 将状态置回 pending

### 9.3 AI 搜索笔记（非 RAG）

1. 用户提问 → 模型产生 `search_notes(keywords, tags?, entities?)` 工具调用
2. Go 查询（示意）：

```sql
SELECT DISTINCT n.*
FROM notes n
LEFT JOIN note_tags nt     ON nt.note_id = n.id AND nt.is_active = 1
LEFT JOIN tags t           ON t.id = nt.tag_id  AND t.is_active = 1
LEFT JOIN note_entities ne ON ne.note_id = n.id AND ne.is_active = 1
LEFT JOIN entities e       ON e.id = ne.entity_id AND e.is_active = 1
WHERE n.is_active = 1
  AND (n.title ILIKE ANY($keywords) OR n.content_md ILIKE ANY($keywords)
   OR  t.name = ANY($tags) OR e.name = ANY($entities))
ORDER BY 命中权重 DESC, n.updated_at DESC
LIMIT $limit;
-- 命中权重：实体/标签命中 3 分 > 标题命中 2 分 > 正文命中 1 分
```

3. 返回 `[{id, title, summary, tags, entities}]`（**只回简介，不回全文**）
4. 模型基于简介判断相关性 → 对相关笔记调 `get_note` 读全文 → 组织回答

### 9.4 AI 写作（后端循环 + 提案审核）

1. 编辑页发消息前，前端**自动保存**当前编辑器内容（保证库中为最新）
2. 请求携带 `note_id`；无会话则创建绑定该笔记的会话
3. 后端 agent 循环：模型 → 产生工具调用 → 执行（读库/产出提案）→ 结果回传 → 循环，直到仅剩文本
4. 正文修改工具产出提案 → SSE 推 `note_proposal`（完整新正文）→ 前端 DiffReviewModal 行级 diff 审核：「接受」走 `POST /api/notes/update` 落库并刷新编辑器，「拒绝」丢弃；标题修改 / 新建笔记直写库 → SSE 推 `note_updated` → 前端拉取最新内容替换编辑器
5. 落库同样触发元数据重新生成（§9.2）

### 9.5 软删除与级联清理

所有删除均为**应用层软删除**（`is_active = 0`），无外键约束，级联在事务内完成：

| 操作 | 级联动作（同事务） |
|---|---|
| 删除分类 | 该分类下笔记 `category_id` 置 NULL |
| 删除笔记 | 软删其 note_tags / note_entities；软删绑定会话及其 messages |
| 删除会话 | 软删其 messages |
| 删除 AI 配置 | 若为激活配置，系统进入无激活状态 |

**垃圾回收**（可选，后续）：定期物理删除 `is_active = 0` 且 `updated_at` 早于 N 天的记录（由定时任务执行，不影响接口）。

## 10. 目录结构规划

```
agent_note/
├── main.go
├── go.mod
├── .env.example
├── docs/
│   ├── design.md              # 本文档
│   └── frontend-design.md     # 前端视觉与交互设计
├── internal/
│   ├── config/                # 环境变量加载与校验（含 SNOWFLAKE_NODE）
│   ├── database/              # GORM 连接 + AutoMigrate
│   ├── snowflake/             # 雪花 ID 生成器（单例，返回 int64）
│   ├── models/                # 数据模型（id 字段 `json:"id,string"` 序列化为字符串）
│   ├── router/                # 路由注册
│   ├── middleware/            # JWT 鉴权 / CORS / 请求日志
│   ├── logger/                # 按天切割的文件日志（log_yyyyMMdd.log，同时输出 stdout）
│   ├── handlers/              # HTTP 处理器（auth/category/note/ai_config/conversation/chat/upload）
│   ├── services/              # 业务层（笔记、检索、元数据）
│   ├── ai/                    # OpenAI 兼容客户端、SSE 转发、agent 循环、工具定义与执行
│   └── worker/                # 元数据生成 worker pool
├── uploads/                   # 上传图片（运行时生成）
└── web/                       # 前端
    └── src/
        ├── api/               # axios 实例、各资源 API、SSE 客户端
        ├── router/
        ├── stores/            # pinia：auth / categories / notes / chat / aiConfigs / theme
        ├── views/             # LoginView / HomeView / EditorView / SettingsView
        ├── components/        # 基础组件 + 业务组件（见 frontend-design.md §5）
        ├── composables/       # useSSE / useDebounce / useAutoSave
        └── styles/            # design tokens、全局样式、ByteMD 主题覆盖
```

## 11. 环境变量

沿用现有 `.env.example`：

| 变量 | 说明 |
|---|---|
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` / `DB_SCHEMA` | PostgreSQL 连接 |
| `JWT_SECRET` | JWT 签名密钥 |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | 登录账号（单用户） |
| `SERVER_PORT` | 服务端口 |
| `LOG_DIR` | 日志目录（按天切割写 `log_yyyyMMdd.log`，写时检查日期跨天自动切换新文件，同时保留 stdout） |
| `UPLOAD_DIR` | 上传文件目录 |
| `WEB_DIST_DIR` | 前端构建产物目录（默认 `web/dist`）；存在时后端直接托管页面，未命中的 GET 路径回退 `index.html`（SPA history 模式），`/api`、`/uploads` 除外 |
| `SNOWFLAKE_NODE` | 雪花算法节点号（单机部署默认 `1`） |
| `DEBUG` | Debug 模式（日志级别 + 输出 SQL） |

AI 配置不放在 env，存数据库（需求：可配置多个、前端管理）。

## 12. 实施阶段

| 阶段 | 内容 | 产出 |
|---|---|---|
| 1. 基础设施 | Go 骨架（config/db/models/router/鉴权/登录接口）；前端骨架（Tailwind 主题、路由、Pinia、登录页、全局布局） | 可登录的空应用 |
| 2. 分类与笔记 | 分类 CRUD；笔记 CRUD；首页三栏布局；编辑页 ByteMD + 图片上传 | 可写笔记、浏览笔记 |
| 3. AI 配置与对话 | ai_configs CRUD / 激活 / 测试；设置页；聊天 SSE 流式 + 会话持久化 | 可对话（无工具） |
| 4. 元数据生成 | worker 异步生成；列表页展示 summary/tags/entities；失败重试 | 检索元数据就绪 |
| 5. AI 工具 | search_notes / get_note 检索链路；写作工具 + note_updated 编辑器同步 | 完整 AI 能力 |
| 6. 打磨 | 深/浅主题、动效、空状态、错误处理、快捷键 | 可交付 |

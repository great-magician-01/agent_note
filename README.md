# AI 智能笔记

个人 AI 笔记：Vue 3 + TS 前端 · Go + Gin 后端 · PostgreSQL · OpenAI 兼容 AI

设计文档：[docs/design.md](docs/design.md)（整体方案）· [docs/frontend-design.md](docs/frontend-design.md)（视觉与交互）

## 功能

- **分类笔记 + Markdown**：单层分类，ByteMD 编辑器（gfm/高亮/mermaid/公式），粘贴图片自动上传
- **AI 配置**：OpenAI 兼容（baseUrl / apiKey / model），多配置，同时仅一个激活
- **异步元数据**：保存笔记后 AI 自动生成简介 / 标签 / 实体（tags & entities），失败可重试
- **AI 对话**：
  - 首页全局助手：检索笔记回答（关键词 + tags + 实体，非 RAG）
  - 编辑页写作助手：直接修改当前笔记（替换/追加/改标题/新建）
- **登录**：单管理员账号，JWT 永不过期

## 运行

### 1. 准备数据库

创建 PostgreSQL 数据库（首次启动自动建表）：

```sql
CREATE DATABASE db;
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env：数据库连接、JWT_SECRET、ADMIN_USERNAME / ADMIN_PASSWORD
```

### 3. 启动后端

```bash
go run .
# 监听 :7562（SERVER_PORT 可改）
```

### 4. 启动前端（开发）

```bash
cd web
npm install
npm run dev
# http://localhost:5173 ，已配置 /api 代理到 :7562
```

### 5. 生产构建

```bash
cd web && npm run build   # 产物在 web/dist
go build -o server . && ./server
# 后端检测到 web/dist 存在时会直接托管前端页面（SPA 回退 index.html）
# 访问 http://localhost:7562 即可，无需单独静态服务器
```

### 6. Docker

```bash
docker build -t agent-note .
docker run -d --name agent-note -p 7562:7562 \
  -e DB_HOST=your-pg-host -e DB_PORT=5432 \
  -e DB_USER=postgres -e DB_PASSWORD=your-password \
  -e DB_NAME=db -e DB_SCHEMA=public \
  -e JWT_SECRET=please-change-me \
  -e ADMIN_USERNAME=admin -e ADMIN_PASSWORD=please-change-me \
  -v agent-note-uploads:/app/uploads \
  agent-note
```

镜像内包含前端构建产物与后端二进制，单容器即可运行（PostgreSQL 外置）。

## 使用流程

1. 登录（.env 中的管理员账号）
2. **设置页** → 新增 AI 配置（baseUrl/apiKey/model）→ 测试连接 → 启用
3. 写笔记 → 保存后 AI 异步生成简介/标签/实体（列表页呼吸点 = 生成中）
4. 首页右侧 AI 助手：「我上周写了什么」「找关于 XX 的笔记」
5. 编辑页右侧写作助手：「帮我把这篇文章写下去」「润色这篇笔记」

## 快捷键

| 键 | 作用 |
|---|---|
| `Ctrl+N` | 新建笔记（首页） |
| `Ctrl+K` | 聚焦搜索框（首页） |
| `Ctrl+S` | 保存笔记（编辑页） |

## 项目结构

```
├── main.go                  # 入口：加载配置 → 雪花ID → 数据库 → 异步worker → Gin
├── Dockerfile               # 三阶段：前端构建 → 后端编译 → alpine 运行（后端托管页面）
├── internal/
│   ├── config/              # .env 配置加载
│   ├── database/            # GORM 连接 + 自动建表
│   ├── models/              # 数据模型（雪花ID、is_active 软删）
│   ├── handlers/            # HTTP 接口层
│   ├── services/            # 业务逻辑（笔记元数据异步生成等）
│   ├── ai/                  # OpenAI 兼容客户端 + 工具循环
│   ├── router/              # 路由（仅 GET/POST，JWT 鉴权）
│   ├── middleware/          # CORS / JWT
│   ├── snowflake/           # 雪花 ID 生成
│   └── worker/              # 后台任务（AI 元数据生成）
├── web/                     # Vue 3 + TS 前端（Vite）
│   └── src/
│       ├── views/           # 首页 / 编辑页 / 设置页 / 登录页
│       ├── components/      # ByteMD 编辑器封装、AI 对话面板等
│       ├── api/             # 接口封装（雪花ID以字符串传递）
│       ├── stores/          # Pinia 状态
│       └── types/           # 含 ByteMD 官方 d.ts 缺陷的本地补全
└── docs/                    # 设计文档
```

## 技术约束

- 接口仅 GET / POST（写操作走 `/xxx/create|update|delete` 子路径）
- 数据库无外键；全表 `is_active` 软删除；级联清理由应用层事务完成
- 主键为雪花 ID（int64），返回前端一律序列化为字符串

## 许可证

[MIT](LICENSE)

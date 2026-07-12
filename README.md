# ChatAPI
[[Telegram](https://t.me/hutao_space)] |  [[LinuxDO](https://linux.do/u/hutao)] | [[BiliBili](https://www.bilibili.com/video/BV11PLg6LEbB)]  
本项目是一个让各类 AI 客户端用 OpenAI Responses 风格接口调用人类的项目，并带有一个 Web 控制台界面，可以帮你组装 Tool Calling 请求，或设置自动回复规则。  
通过这个项目，你可以让别人把你配置到 Agent 或 聊天机器人中，然后自己扮演 AI 助手被调用。
也可以在自己开发 Agent 的时候作为 Mock LLM 使用。

- 后端：Go
- 前端：React + Vite + Ant Design
- 数据存储：SQLite / PostgreSQL（规划中，当前首批已落 SQLite）

默认提供：

- Go 后端重构进行中，当前分支已切换到新的 Go 工程骨架（可能并不稳定）
- 支持 `/v1/chat/completions`、`/v1/responses`、`/messages` 三套接口
- `serve` / `lab` 双模式入口
- Lab 模式默认 SQLite、本地自动开浏览器、可作为 Mock LLM 调试入口


## 1. 部署
### 单二进制构建

在项目根目录执行：

```bash
make build EMBED_FRONTEND=1
```

该命令会先构建前端，再使用 Go `embed_frontend` build tag 将前端产物写入
后端可执行文件，输出为 `build/chatapi`。部署时只需要分发这个二进制文件，
不需要设置 `CHATAPI_WEB_DIST_DIR`。

使用 `make build` 可以只构建后端，并继续从 `CHATAPI_WEB_DIST_DIR` 提供前端文件。

### 无需 Nginx 一键部署
#### 构建前端

```bash
cd ./frontend
npm i
npm run build
```

首页默认显示当前访问来源作为 API 基址；如需在构建时指定其他基址，可在构建前设置 `VITE_HOMEPAGE_API_BASE_URL`。

#### 设置 `.env`
```env
CHATAPI_DATA_DIR=./data
CHATAPI_DB_DRIVER=sqlite
CHATAPI_DB_DSN=./data/chatapi.sqlite3
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5000
CHATAPI_WEB_DIST_DIR=../frontend/dist
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

#### 启动 Go 后端

```bash
cd ./backend
go run ./cmd/chatapi serve
```
### dev部署

#### 启动后端

```bash
cd ./backend
go run ./cmd/chatapi serve
```

#### 启动前端

```bash
cd ./frontend
npm i
npm run dev
```

## 3. 配置环境变量

先复制配置模板：

```bash
cp backend/.env.example backend/.env
```

建议至少确认以下配置：

```env
CHATAPI_DATA_DIR=./data
CHATAPI_DB_DRIVER=sqlite
CHATAPI_DB_DSN=./data/chatapi.sqlite3
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5000
```

如果部署配置保存在项目目录之外，可以设置外部 env 文件路径：

```env
CHATAPI_ENV_FILE=/path/to/chatapi.env
```

外部 env 文件与项目内 `.env` 使用相同格式，已存在的进程环境变量不会被文件中的值覆盖。

建议同时确认以下配置：

```env
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

可选配置：

```env
# 直接让 Go 后端对外托管前端静态文件
# CHATAPI_WEB_DIST_DIR=../frontend/dist

# Lab 模式可选
# CHATAPI_OPEN_BROWSER=1
# CHATAPI_LAB_TOKEN=
# CHATAPI_LAB_PASSWORD=
# CHATAPI_ALLOW_REMOTE_LAB=0
```

## Lab 模式

本地调试可直接启动：

```bash
cd ./backend
go run ./cmd/chatapi lab
```

默认行为：

- 绑定 `127.0.0.1:5000`
- 使用 SQLite
- 自动打开浏览器
- `/api/auth/session` 直接返回已登录 Lab 用户

如果要远程暴露 Lab，必须显式允许并配置一次性 token 或密码：

```bash
cd ./backend && CHATAPI_HOST=0.0.0.0 CHATAPI_ALLOW_REMOTE_LAB=1 CHATAPI_LAB_TOKEN=xxx go run ./cmd/chatapi lab
```



调用示例：  

```bash
curl https://127.0.0.1:5173/v1/responses \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-i-love-you-hutao' \
  -d '{
    "model": "胡桃酱",
    "input": [
      {
        "type": "message",
        "role": "user",
        "content": [
          {
            "type": "input_text",
            "text": "在这里打字就可以和胡桃酱本人对话！"
          }
        ]
      }
    ],
    "stream": true
  }'

```

Anthropic Messages 兼容接口使用 `/messages`，例如：

```bash
curl https://127.0.0.1:5173/messages \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-i-love-you-hutao' \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "你好"
      }
    ],
    "stream": true
  }'
```

# Hysteria2 Web 控制面板

轻量级 Hysteria2 多节点管理面板，面板运行时采用 `Go Panel + MySQL + Vue 3 + TypeScript` 架构。

## 功能

- 节点管理、用户管理、流量统计、权限控制
- 支持用户订阅链接，同用户名可聚合多节点下发
- `Agent` / `SSH` 双运维模式，默认推荐 `Agent`
- 邮件通知、登录防暴力破解、在线升级
- GitLab CI 自动部署与 Tag 发布

## 运维边界

- `SSH`：首次安装、首次写配置、首次下发 Agent、卸载清理
- `Agent`：启停、日志、状态、流量、配置下发
- `trafficStats`：节点本地统计接口，由 Agent 或 SSH 请求

## 系统要求

- Go `>= 1.23`
- MySQL `>= 5.7` 或 MariaDB `>= 10.2`
- Node.js `>= 18`（前端开发和构建）
- Nginx / Caddy

## 部署

- 下载仓库代码
- 生产入口由 `Caddy/Nginx` 反代到 Go 面板监听地址
- Go 面板支持无配置首启：直接运行二进制会默认监听 `127.0.0.1:18080`，完成首次初始化后自动写出 `config/panel.env`
- `config/panel.env.example` 只保留最小启动项；`public_api_base_url` 等运营配置可在首次安装或系统设置中补齐
- 默认配置、数据库初始化脚本和外部静态目录都会优先按 `config/panel.env` 或 `build/panel/mxinhy-panel` 的实际位置推导，不再依赖当前 shell 的工作目录
- `PANEL_PUBLIC_API_BASE_URL` 建议显式配置为可被节点和订阅客户端访问的公网 API 地址，例如 `https://panel.example.com/api`；如果误填为站点根地址，后端会自动规范化为 `/api`
- 数据库继续使用 MySQL，节点与 Agent 部署方式保持不变
- 新环境按纯 Go 面板部署，不依赖 PHP-FPM、Apache 或 legacy PHP 服务
- 首次节点安装前会自动检查并补装常用依赖；当前支持在常见 Linux 包管理器上自动安装 `curl`，自签证书模式还会自动补装 `openssl`，若当前账号缺少 root/sudo 权限则会直接提示

## 目录结构

```text
artifacts/agent/        Agent 可分发二进制
cmd/                    Go 可执行入口
config/                 面板环境变量示例
database/               初始化结构与迁移脚本
deploy/systemd/         systemd 服务模板
internal/               Go 面板核心实现
public/                 Web 构建产物与运行目录
storage/                运行时目录
web/                    Vue 3 前端源码
```

## 开发

```bash
npm install
npm run dev
npm run build
go run ./cmd/mxinhy-panel
go run ./cmd/mxinhy-panel -port 18081
```

重新构建 Agent 二进制：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o artifacts/agent/mxinhy-agent-linux-amd64 ./cmd/mxinhy-agent
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o artifacts/agent/mxinhy-agent-linux-arm64 ./cmd/mxinhy-agent
```

## CI

- 推送到 `main`：仅在受保护分支且推送者与 `DEPLOY_TRIGGER_LOGIN` 一致时，自动执行构建并部署到生产环境
- 推送版本 Tag：仅在受保护 Tag 且推送者与 `DEPLOY_TRIGGER_LOGIN` 一致时，自动执行构建、部署，并在 GitLab 创建对应 Release
- Tag 发版时，Panel 二进制会使用 `release_embed` 构建标签将当前 `public/` 前端资源嵌入到 `mxinhy-panel` 中
- CI 构建阶段会同时生成前端产物、`build/panel/mxinhy-panel`，以及 `artifacts/agent/` 下的 Linux `amd64`/`arm64` Agent 二进制
- `main` 分支部署包仍包含外部 `public/` 静态目录以保持兼容；Tag 最终部署包会省略 `public/`，只保留最小运行时目录
- CI 最终部署包只包含运行时所需目录：`build/panel/`、`artifacts/agent/`、`deploy/systemd/`、`config/panel.env.example`、`database/schema.sql`、`database/migrations/`、`package.json`，以及仅在非 Tag 部署包中保留的 `public/`
- 由于仓库需要同步到 GitHub 作为公开镜像，`.gitlab-ci.yml` 不应包含任何真实主机、目录、密钥或账号，所有敏感信息都必须放在 GitLab CI Variables 中

推荐变量：

- `DEPLOY_TRIGGER_LOGIN`
- `SSH_PRIVATE_KEY`
- `DEPLOY_HOST`
- `DEPLOY_PORT`
- `DEPLOY_USER`
- `DEPLOY_PATH`
- `DEPLOY_KNOWN_HOSTS`
- `DEPLOY_KEEP_RELEASES`
- `PANEL_SERVICE_NAME`
- 登录页密码加密不再依赖前端构建时注入长期 seed；前端会在 `/api/auth/login-challenge` 响应中读取后端下发的一次性派生 seed

## 常用命令

```bash
go build -o build/panel/mxinhy-panel ./cmd/mxinhy-panel
go run ./cmd/mxinhy-panel
./build/panel/mxinhy-panel -port 18081
./build/panel/mxinhy-panel -config config/panel.env run-job install <job-id>
```

## 安全说明

- 禁止在仓库中提交真实域名、真实 IP 或生产路径
- SSH 私钥必须通过上传流程进入 `storage/ssh-keys`
- 面板公网入口统一由 `Caddy/Nginx` 反代到 Go 面板
- 所有多条数据接口都必须维持后端分页
- 管理后台桌面端必须保持 `100vh` 固定布局，超出内容只允许局部滚动

## 许可证

本项目采用 MIT 许可证。

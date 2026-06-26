# Hysteria2MX

Hysteria2MX 是一个面向 Hysteria2 的 Web 控制面板，用于集中管理多节点服务、用户账号、流量统计和订阅分发。项目采用 Go 后端、Vue 3 前端和 MySQL 存储，适合自建 Hysteria2 节点的个人或小团队使用。

## 功能特性

- 多节点管理：新增、编辑、安装、卸载、启停和配置下发
- 用户管理：账号状态、到期时间、流量配额、实时用量和订阅链接
- 节点 Agent：支持节点状态上报、任务执行、日志读取和本地认证同步
- 流量统计：展示节点总览、用户用量、在线连接和历史趋势
- 权限与审计：支持管理员角色、操作审计、登录保护和安全提示
- 系统设置：支持站点信息、公网 API 地址、Mock 演示数据和 SMTP 通知
- 首次安装向导：浏览器中完成数据库初始化和管理员账号创建

## 技术栈

- 后端：Go `1.23+`
- 前端：Vue 3、TypeScript、Vite
- 数据库：MySQL `5.7+` 或 MariaDB `10.2+`
- 部署：推荐使用 Linux、systemd，以及 Nginx 或 Caddy 反向代理

## 部署形态

Hysteria2MX 支持两种运行形态：

- 生产部署：构建前端静态资源和 Go 面板二进制，由 Go 面板提供 Web 页面与 API，再通过 Nginx 或 Caddy 暴露公网入口
- 开发部署：Go 后端和 Vite 前端分开运行，前端开发服务器通过代理访问后端 API

## 生产部署

安装依赖并构建前端静态资源：

```bash
npm ci
npm run build
```

构建 Go 面板：

```bash
go build -o build/panel/mxinhy-panel ./cmd/mxinhy-panel
```

启动生产服务：

```bash
./build/panel/mxinhy-panel
```

默认监听地址为 `127.0.0.1:18080`。首次启动时可以不准备 `config/panel.env`，打开面板页面后按安装向导填写 MySQL 信息和管理员账号，系统会自动初始化数据库并写入运行配置。

如需临时指定端口：

```bash
./build/panel/mxinhy-panel -port 18081
```

生产环境建议使用 Nginx 或 Caddy 将公网 HTTPS 入口反向代理到面板监听地址，例如 `127.0.0.1:18080`。

最小配置示例见 `config/panel.env.example`：

```env
PANEL_BIND_ADDR=127.0.0.1:18080
PANEL_ENCRYPTION_KEY=change-this-to-a-random-secret
PANEL_LOGIN_AES_SEED=change-this-to-a-random-seed

DB_HOST=127.0.0.1
DB_NAME=hy2_panel
DB_USER=root
DB_PASSWORD=change-me
```

如果需要节点 Agent、订阅链接或远端回调正常工作，请在安装向导或系统设置中配置公网 API 地址，例如：

```text
https://panel.example.com/api
```

项目提供 systemd 模板：

- `deploy/systemd/mxinhy-panel.service`
- `deploy/systemd/mxinhy-agent.service`

模板中的占位符需要替换为实际安装路径后再使用。

## 开发部署

开发时建议分别启动后端和前端。

后端默认开发端口可使用 `8080`，与 Vite 代理配置保持一致：

```bash
go run ./cmd/mxinhy-panel -port 8080
```

前端开发服务器：

```bash
npm ci
npm run dev
```

如果后端使用其他端口，可以通过 `VITE_API_TARGET` 指定 API 代理目标：

```bash
VITE_API_TARGET=http://127.0.0.1:18080 npm run dev
```

运行测试：

```bash
go test ./...
```

## Agent 构建

节点 Agent 用于减少 SSH 依赖，并让节点主动上报状态、执行任务和同步本地认证数据。

构建 Linux Agent：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o artifacts/agent/mxinhy-agent-linux-amd64 ./cmd/mxinhy-agent
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o artifacts/agent/mxinhy-agent-linux-arm64 ./cmd/mxinhy-agent
```

首次安装节点仍需要可用的 SSH 连接。面板会通过 SSH 完成初始安装、配置写入和 Agent 下发；后续常规操作优先通过 Agent 执行。

## 目录结构

```text
cmd/                    Go 可执行入口
internal/panel/         面板后端核心代码
web/                    Vue 前端源码
public/                 前端构建产物
config/                 面板配置示例
database/               初始化结构、迁移脚本和变更说明
deploy/systemd/         systemd 服务模板
artifacts/agent/        Agent 构建产物
storage/                运行时数据目录
```

## 安全说明

- 不要将真实密钥、数据库密码、SSH 私钥、生产域名或服务器 IP 提交到仓库
- `PANEL_ENCRYPTION_KEY` 和 `PANEL_LOGIN_AES_SEED` 生产环境必须改为随机值
- 面板公网入口应使用 HTTPS，并只暴露反向代理后的服务
- SSH 私钥应通过面板上传流程管理，不建议手动填写服务器本地路径
- 面板返回给用户的错误信息应保持安全、简洁，不暴露内部路径、SQL 或堆栈信息

## 许可证

本项目采用 MIT 许可证。

# 1panel-agent

1Panel 多机管理补充：Master 统一入口 + Agent 内网穿透，在浏览器里切换访问各节点上的 1Panel。

Agent 主动用 WebSocket 连到 Master（Token 认证），之上用 smux 做流复用，效果类似 FRP：节点可在 NAT/内网后，无需对面板端口做公网暴露。

## 架构

```text
Browser ──HTTP──▶ Master ──smux/WS──▶ Agent ──HTTP/WS──▶ 本机 1Panel
                     ▲
                     │ WebSocket + Token
                   Agent 主动接入
```

| 组件 | 职责 |
|------|------|
| Master | 接受 Agent 接入；节点列表；`/n/{id}/` 反代到对应 Agent |
| Agent | 注册到 Master；把隧道请求转到本机 1Panel；可注入 API Key |

## 构建

需要 Go 1.23+。

```bash
go build -o bin/1panel-agent ./cmd/1panel-agent
```

## 使用

### 1. 启动 Master

```bash
./bin/1panel-agent master --listen :8080 --token YOUR_SECRET
```

浏览器打开 `http://<master>:8080/` 可看到在线节点。

### 2. 配置 Agent 本机 1Panel

在每台装有 1Panel 的机器上：

```bash
./bin/1panel-agent agent set \
  --panel-url http://127.0.0.1:20560 \
  --panel-key YOUR_1PANEL_API_KEY
```

- `--panel-url`：本机面板地址（默认 `http://127.0.0.1:20560`）
- `--panel-key`：1Panel「设置 → 面板」中的 API 接口密钥；有配置时，Agent 会自动补 `1Panel-Token` / `1Panel-Timestamp`

配置文件：`~/.1panel-agent/agent.json`（含稳定 `id`，重连不换）。

### 3. 注册并运行 Agent

```bash
./bin/1panel-agent agent register <master_ip>:<port>/<token>
# 例：
./bin/1panel-agent agent register 1.2.3.4:8080/YOUR_SECRET
```

`register` 会写入 Master 地址与 Token，并前台保持连接（可用 systemd 托管）。已注册过可直接：

```bash
./bin/1panel-agent agent run
```

### 4. 切换节点

打开 Master 首页 → 点击节点，或直接访问：

```text
http://<master>:8080/n/<agent_id>/
```

流量路径：浏览器 → Master → 隧道 → Agent → 本机 1Panel。

## CLI 一览

```text
1panel-agent master --listen :8080 --token SECRET
1panel-agent agent register host:port/token
1panel-agent agent set --panel-url URL --panel-key KEY
1panel-agent agent run
```

## Docker 联调

仓库自带假 1Panel + Master + Agent 编排，用于验证隧道：

```bash
./deploy/docker/test.sh
```

或手动：

```bash
cd deploy/docker
docker compose up -d --build
# Master: http://127.0.0.1:18080/
docker compose down -v
```

## 说明与限制

- 单二进制，无数据库；在线节点表在 Master 内存中，重启后等 Agent 重连。
- 反代使用路径前缀 `/n/{id}/`，会改写 `Location` 与 `Set-Cookie` 的 `Path`。若 1Panel 前端写死绝对路径，个别资源可能异常；届时可改为「Cookie 选节点 + 根路径反代」。
- 只做 1Panel 的 HTTP/WebSocket 穿透，不是通用端口映射工具。
- Master Token 仅用于 Agent 接入认证，请使用足够长的随机串，并限制 Master 端口暴露面。

## 开发

```bash
go test ./...
./deploy/docker/test.sh
```

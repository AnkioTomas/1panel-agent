# 1pm 项目架构

> 仓库：`1panel-agent`（安装名 `1pm`）  
> 语言：Go 1.25  
> 依赖：`github.com/coder/websocket`、`github.com/xtaci/smux`

---

## 一句话定义

**1pm 是 1Panel 社区版多机网关**：Master 接管本机 1Panel 公网端口；Agent 经 WebSocket + smux 反向隧道接入；浏览器在 Master 上管理节点并切换到子机面板（Agent 侧自动登录）。

---

## 整体架构

```text
Browser ──HTTP/HTTPS──▶ Master (:原面板端口)
                    │   （面板 secret 证书就绪时 cmux：HTTPS + HTTP→HTTPS）
                    ├─ /__mp/              节点管理 UI（本机 1Panel 登录态 → mp_auth）
                    ├─ /agent/ws           Agent WebSocket（HMAC；TLS 时为 wss）
                    ├─ /agent.sh           签名安装脚本
                    ├─ /agent.bin          Master 自身二进制
                    ├─ mp_node 有效        → 根路径隧道反代 Agent 本机 1Panel
                    └─ 默认               → 反代本机 1Panel（http(s)://127.0.0.1:内部端口）

Agent ──ws/wss+HMAC──▶ Master /agent/ws
         └─ smux ←── Master OpenStream（HTTP / WS / Stats / Update / PanelUpgrade）
```

---

## 目录结构

```text
1panel-agent/
├── cmd/1panel-agent/main.go     CLI：master / agent / uninstall / version
├── install.sh                   Master 一键安装（角色互斥检查）
├── internal/
│   ├── agent/
│   │   ├── client.go            WS 注册 + smux Accept（HTTP/WS/Stats）
│   │   ├── detect.go            自动探测本机 1Panel
│   │   ├── install.go           agent install 落盘配置
│   │   ├── stats.go             /proc 采 CPU/内存 + 版本
│   │   └── uninstall.go         Clean / Uninstall
│   ├── master/
│   │   ├── server.go            HTTP 服务、Agent WS、HTTP/WS 隧道
│   │   ├── auth.go              /__mp/ 鉴权（mp_auth + 本机会话校验）
│   │   ├── prepare.go           Takeover
│   │   ├── registry.go          在线 Session + Stats 字段
│   │   ├── stats.go             经 smux 拉 HostStats
│   │   ├── install.go           /agent.sh、/agent.bin、install-command
│   │   ├── cookies.go           面板 Cookie 过滤 / Path 归一化 / 本机暂存辅助
│   │   ├── switch.go            /__mp/go/{id}、/__mp/local（内存暂存本机会话）
│   │   ├── token.go             Token 轮换
│   │   ├── inject.go            侧栏节点切换 Hook
│   │   ├── ui.go                /__mp/ 管理页 + /api/agents
│   │   └── uninstall.go         Clean / Uninstall
│   ├── panel/                   1Panel CLI / 登录 / 加密（不读 core.db）
│   ├── protocol/                Register / RequestMeta / HostStats / 分块帧
│   ├── config/                  agent.json、master.json、HMAC Sign、AES-GCM
│   ├── role/                    Master/Agent 互斥检测
│   └── buildinfo/               构建期 Version
├── docs/
└── .github/workflows/release.yml
```

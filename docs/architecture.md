# 1pm 项目架构分析

> 项目名：`1panel-agent`（安装名 `1pm`）  
> 语言：Go 1.25  
> 核心依赖：`github.com/coder/websocket`、`github.com/xtaci/smux`

---

## 一句话定义

**1pm 是一个 1Panel 多机网关**：Master 节点接管本机 1Panel 端口作为统一入口，Agent 节点通过 WebSocket + smux 隧道接入（类 FRP），浏览器在 Master 上可查看所有在线子节点、一键切换到子节点面板（自动完成账号登录）。

---

## 整体架构

```
Browser ──HTTP──▶ Master (:原面板端口)
                    │
                    ├─ /__mp/              节点管理 UI（鉴权：本机 1Panel 登录态）
                    ├─ /agent/ws           Agent WebSocket 接入点
                    ├─ /agent.sh           Agent 安装脚本（含 Token）
                    ├─ /agent.bin          Agent 二进制（Master 自身复制）
                    ├─ mp_node cookie 有效  → 隧道反代选中的 Agent 本机 1Panel
                    └─ 默认               → 反代本机 1Panel（takeover 迁移后的内部端口）

Agent ──WebSocket+Token──▶ Master /agent/ws
         └─ smux 多路复用 ←── Master 主动 OpenStream 发请求
```

---

## 目录结构

```
1panel-agent/
├── cmd/1panel-agent/
│   └── main.go             CLI 入口（master / agent 子命令）
├── internal/
│   ├── agent/              Agent 侧逻辑
│   │   ├── client.go       WebSocket 连接 + smux 流处理（HTTP/WS 双模式）
│   │   ├── detect.go       自动检测本机 1Panel 地址
│   │   └── register.go     解析注册目标 + 持久化配置
│   ├── master/             Master 侧逻辑
│   │   ├── server.go       HTTP 服务器、Agent WS 接入、HTTP/WS 隧道代理
│   │   ├── auth.go         /__mp/ 鉴权（mp_auth cookie + 本机 1Panel 会话验证）
│   │   ├── prepare.go      Takeover：迁移 1Panel 到内部端口、重启 1panel-core
│   │   ├── registry.go     在线 Agent 注册表（内存，smux Session 持有）
│   │   ├── install.go      /agent.sh 脚本生成 + /agent.bin 二进制分发
│   │   ├── switch.go       /__mp/go/{id} 切换节点（写 mp_node Cookie + 重定向）
│   │   ├── token.go        Token 管理（轮换）
│   │   ├── inject.go       HTML 注入（1Panel 侧边栏节点切换按钮）
│   │   ├── cookies.go      Cookie 命名空间隔离（mp_r_* 前缀）
│   │   ├── ui.go           /__mp/ 管理页面（Go template HTML）
│   │   └── upgrade.go      检查 Master/Agent 版本更新
│   ├── panel/              1Panel 交互层
│   │   ├── settings.go     通过 1panel/1pctl CLI 读取端口/安全入口/用户名/版本
│   │   ├── login.go        1Panel v2 登录（RSA+AES 混合加密密码）
│   │   ├── encrypt.go      RSA 公钥加密 + AES 密码封装
│   │   └── token.go        API Key 注入（X-Panel-Key）
│   ├── protocol/
│   │   └── protocol.go     二进制帧协议：JSON 帧 + 分块传输
│   └── config/
│       ├── config.go       Agent 配置（~/.1panel-agent/agent.json）
│       └── master.go       Master 状态（/var/lib/1pm/master.json）
├── deploy/
│   ├── systemd/            systemd 单元文件模板
│   └── docker/             集成测试环境
├── scripts/                辅助脚本
└── install.sh              Master 一键安装脚本（下载 + checksum + systemd）
```

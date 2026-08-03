# 核心流程说明

## 1. Takeover（端口接管）

Master 启动时执行：

```text
1. 1panel user-info 读当前面板端口（如 52045）
2. InternalPort = OriginalPort + 10000（>65535 则 -10000）
   例：52045 → 62045
3. 1panel update port → 1Panel 迁到内部端口
4. systemctl restart 1panel-core（最多等 45s 端口就绪）
5. 1pm 监听原 :52045
6. 写入 /var/lib/1pm/master.json（original_port / internal_port / token）
```

卸载（`1pm uninstall` / `master uninstall`）会尝试把端口改回 `original_port`。

---

## 2. Agent 接入

```text
Agent                           Master
  │                               │
  │── WS /agent/ws?timestamp=&sign= ──▶│  VerifyToken（HMAC，±5 分钟）
  │                               │
  │── Register{ID,Hostname,PanelURL,  │
  │            PanelVersion,AgentVersion} ▶│
  │◀── RegisterOK{OK:true} ───────│
  │                               │
  │   smux Client / Server        │  reg.Put(Session)
  │   AcceptStream 循环           │
  │   KeepAlive 20s / 超时 60s    │
```

断线后 Agent 指数退避重连（1s → … → 30s）。  
本机已装 Master 时 `agent run` / `agent install` 会拒绝。

---

## 3. HTTP 隧道（Browser → Agent 1Panel）

```text
Browser ──GET /api/xxx──▶ Master
                            │  mp_node → sess = reg.Get(id)
                            │  OpenStream + WriteRequestMeta(HTTP)
                            │  CopyChunks(body)
                            │
                            │              Agent: 必要时自动登录本机 1Panel
                            │              Agent: 转发并回写 ResponseMeta + chunks
                            │
                            │  text/html → 注入侧栏 Hook
                            │  Set-Cookie → 真名 psession（当前在远端节点）
Browser ◀── 响应 ──────────┘
```

自动登录发生在 **Agent 侧**（解密 `panel_password_enc`），Master **不存**子机/主节点面板密码。

---

## 4. WebSocket 隧道

`StreamTypeWS`：Agent 对本机 1Panel 做 HTTP Upgrade 后双向 `io.Copy`。

---

## 5. Stats 隧道

管理页每 5 秒请求 `GET /__mp/api/agents` 时，Master 对每个在线 Agent：

1. `OpenStream` + `StreamTypeStats`  
2. Agent 回 `HostStats`（CPU%、内存、Agent/1Panel 版本）  
3. `Registry.UpdateStats` 后返回 JSON 列表  

---

## 6. 节点切换

### `/__mp/go/{id}`

校验 Agent 在线 → 本机会话暂存 `mp_l_*` → 隧道预热自动登录 → 写入远端 `psession` → 写 `mp_node` → 302 `/`。

### `/__mp/local`

清除 `mp_node` → 302 回本机面板。

后续根路径：有 `mp_node` 且在线 → 隧道；否则 → 本机 `localProxy`。

---

## 7. `/__mp/` 鉴权

```text
1. mp_auth cookie 与内存 sessionSecret 常量时间比较 → 通过
2. 否则用浏览器 Cookie 调本机 1Panel /api/v2/dashboard/base/os
   code=200 → 生成新 sessionSecret，签发 mp_auth，放行
3. 否则：API → 401 JSON；页面 → 302 到安全入口（?mp_return=/__mp/）
```

`mp_auth` 为随机 token（非 Token-HMAC、无固定 7 天 TTL）；新签发会顶掉旧会话。

---

## 8. HTML 注入（侧栏）

对 `text/html` 注入 `<script data-mp-hook="1">`：

- 替换侧栏底部区域为节点切换按钮  
- 轮询 `/__mp/api/agents` 渲染列表  
- 选子节点 → 整页跳转 `/__mp/go/{id}`  
- 「主节点」→ `/__mp/local`  
- 「管理节点…」→ `/__mp/`  

---

## 9. Cookie

| 场景 | Cookie | 说明 |
|------|--------|------|
| 当前面板会话 | `psession` 等 | 本机或子节点，取决于是否选中远端 |
| 本机暂存 | `mp_l_*` | 切到子节点时保存，切回恢复 |
| 当前节点 | `mp_node` | Agent ID；空=本机 |
| 管理页 | `mp_auth` | 内存 sessionSecret |

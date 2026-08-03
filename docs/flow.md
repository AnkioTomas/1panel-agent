# 核心流程说明

## 1. Takeover（端口接管）

Master 启动时默认执行 takeover：

```
1. 调用 1panel user-info 读取当前面板端口（如 52045）
2. 计算内部端口：InternalPort = 原端口 + 100000（若 >65535 则 +10000，兜底用 152045）
   例：52045 → 152045
3. 调用 1panel update port 把 1Panel 监听地址改为 127.0.0.1:152045
4. systemctl restart 1panel-core（最多等待 45s 确认端口可用）
5. 1pm 自己监听在原来的 :52045
6. 状态写入 /var/lib/1pm/master.json（OriginalPort/InternalPort 记录）
```

卸载时需将端口改回原值，或从 master.json 的 `original_port` 恢复。

---

## 2. Agent 接入流程

```
Agent                           Master
  │                               │
  │──── WebSocket /agent/ws?token=<T> ──▶│  验证 Token
  │                               │
  │◀─── 等待 Register JSON ───────│
  │──── Register{ID,Hostname,PanelURL,PanelVersion} ──▶│
  │◀─── RegisterOK{OK:true} ──────│
  │                               │
  │   smux 多路复用层建立（Client/Server）│
  │   Agent: AcceptStream 循环    │  Master: reg.Put(Session)
  │                               │
  │   心跳：KeepAlive 20s，超时 60s │
```

连接断开后 Agent 指数退避重连（1s → 2s → 4s → … 最大 30s）。

---

## 3. HTTP 隧道请求流程（Browser → Agent 1Panel）

```
Browser ──GET /api/xxx──▶ Master
                            │  (mp_node cookie 指向 agent-id)
                            │  sess = reg.Get(agent-id)
                            │  stream = sess.Mux.OpenStream()
                            │
                            │──[stream] WriteRequestMeta(type=HTTP, method, path, headers)──▶ Agent
                            │──[stream] CopyChunks(requestBody) ──▶ Agent
                            │
                            │                       Agent: 转发给本机 1Panel
                            │                       Agent: 读响应回写 stream
                            │
                            │◀──[stream] ReadJSON(ResponseMeta{status, headers}) ──
                            │◀──[stream] ChunkReader(responseBody) ──
                            │
                            │  如果是 text/html → 注入侧边栏 JS hook
                            │  Set-Cookie 重命名为 mp_r_* 前缀
                            │
Browser ◀── 响应 ──────────┘
```

---

## 4. WebSocket 隧道流程

WebSocket 请求走独立分支（StreamTypeWS=0x02），Agent 侧直接 TCP dial 本机 1Panel，完成 HTTP Upgrade 握手后双向 io.Copy，实现完整 WebSocket 透传。

---

## 5. 节点切换（/__mp/go/{id}）

```
Browser ──GET /__mp/go/{id}──▶ Master
                                │
                                │  1. 通过隧道（tunnelTransport）发 HTTP 请求到 Agent 1Panel
                                │  2. 访问安全入口（若有），获取 panel_public_key cookie
                                │  3. 调用 /api/v2/core/auth/captcha 获取 RSA 公钥
                                │  4. RSA 加密密码，POST /api/v2/core/auth/login
                                │  5. 收集登录成功后的 Set-Cookie
                                │
                                │  写入 Cookie：
                                │    mp_node = agent-id
                                │    mp_r_psession = <远端 psession 值>
                                │    mp_r_pcsrftoken = <远端 csrf 值>
                                │    mp_r_SecurityEntrance = <base64 安全入口>
                                │
Browser ◀── 302 重定向到 / ────┘

后续所有根路径请求：
  Master 读 mp_node cookie → 路由到对应 Agent 隧道
  Cookie 头中 mp_r_* 脱前缀后发给 Agent 1Panel（不污染本地 psession）
```

---

## 6. 鉴权机制（/__mp/）

```
访问 /__mp/ 时：
  1. 检查 mp_auth cookie（HMAC-SHA256，以 Token 为密钥，含过期时间戳）
     有效 → 直接放行
  2. 向本机 1Panel /api/v2/dashboard/base/os 转发请求 Cookie
     返回 code=200 → 说明本机 1Panel 已登录
     → 签发 mp_auth cookie（TTL=7天），放行
  3. 否则 → 302 重定向到 1Panel 登录页（携带 mp_return 参数）
```

---

## 7. HTML 注入（侧边栏节点切换）

Master 对所有经过隧道/本地反代的 `text/html` 响应注入一段 `<script data-mp-hook="1">` 到 `</body>` 之前：

- 在 1Panel Vue 侧边栏底部替换原生用户按钮为「节点切换」按钮
- 点击弹出节点列表（轮询 `/__mp/api/agents`）
- 选择节点 → 整页跳转到 `/__mp/go/{id}`（避开 Vue Router，因 1Panel 前端绝对路径 /assets 在前缀模式下失效）
- 「主节点」选项 → 跳转 `/__mp/local`（清除 mp_node cookie）

---

## 8. Cookie 命名空间隔离

| 场景 | Cookie 名 | 说明 |
|------|-----------|------|
| 本机 1Panel 会话 | `psession` | 正常 1Panel 登录 cookie |
| 远端 Agent 会话 | `mp_r_psession` | 远端会话，不覆盖本地 |
| 节点选择 | `mp_node` | 当前激活的 Agent ID |
| 管理页鉴权 | `mp_auth` | HMAC 签名，TTL 7天 |

请求经隧道发往 Agent 时，`mp_r_*` 前缀自动剥除还原为原始 cookie 名。

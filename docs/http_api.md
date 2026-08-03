# HTTP API 参考

Master 对外暴露以下 HTTP 端点（监听在原 1Panel 端口）。

---

## 公开端点（HMAC：timestamp + sign）

鉴权与 Agent WebSocket 相同：`sign = hex(HMAC-SHA256(token, "timestamp=<unix>"))`，允许 ±5 分钟时钟偏差。**不接受裸 `token=` 查询参数。**

### GET /agent/ws?timestamp=\<ts\>&sign=\<sign\>

Agent WebSocket 接入点。

- 校验 timestamp + sign
- 升级为 WebSocket，读取 Register 握手包
- 建立 smux 多路复用会话
- Agent 掉线时自动从注册表移除

### GET /agent.sh?timestamp=\<ts\>&sign=\<sign\>

返回 Agent 安装 shell 脚本（text/plain）。

- 脚本内嵌 Master 地址与 Token（仅签名通过后下发）
- 安装流程：签名下载 /agent.bin → `agent install` 落盘 → systemd `agent run`
- 可选交互/`PANEL_PASS` 调用 `agent setpwd`（密码加密存储）
- UI 展示的 curl 命令含当前签名，约 5 分钟过期

### GET /agent.bin?timestamp=\<ts\>&sign=\<sign\>

返回 Master 自身的二进制文件（application/octet-stream）。

- 即 `os.Executable()` 所指向的文件
- 响应头包含 `X-1pm-GOOS` / `X-1pm-GOARCH`

---

## 管理 API（需 mp_auth 或本机 1Panel 会话）

### GET /__mp/

节点管理 UI 主页（HTML）。

展示：在线 Agent 数量、注册地址、安全入口、子节点安装命令、在线节点列表、版本信息。

**鉴权流程**：检查 `mp_auth` cookie → 验证本机 1Panel 登录态 → 未登录则 302 到 1Panel 登录页。

### GET /__mp/api/agents

返回在线 Agent 列表（JSON）。

```json
[
  {
    "id": "a1b2c3d4e5f6a7b8",
    "hostname": "ubuntu-node2",
    "panel_url": "http://127.0.0.1:152045",
    "remote_ip": "10.211.55.15",
    "panel_version": "v2.4.1",
    "agent_version": "v0.1.0",
    "cpu_percent": 12.5,
    "mem_total": 8589934592,
    "mem_used": 2147483648,
    "open_url": "/__mp/go/a1b2c3d4e5f6a7b8"
  }
]
```

管理页打开时每 5 秒轮询本接口；Master 经隧道向 Agent 拉取 CPU/内存/版本后再返回。

### GET /__mp/api/install-command

实时签发带 HMAC 的一键安装命令（约 5 分钟有效）。管理页「复制命令」会先调本接口再写入剪贴板。

```json
{
  "install": "curl -fsSL \"http://10.0.0.1/agent.sh?timestamp=...&sign=...\" | sudo bash"
}
```

### POST /__mp/api/rotate-token

轮换隧道 Token。轮换后所有已注册 Agent 立即失效，需重新执行安装命令。

```json
{
  "install": "curl -fsSL \"http://10.211.55.14:52045/agent.sh?timestamp=<ts>&sign=<sig>\" | sudo bash"
}
```

### GET /__mp/go/{id}

切换到指定 Agent 节点（写 mp_node Cookie 后重定向）。

- 通过隧道完成远端 1Panel 登录（RSA+AES 加密）
- 写入 `mp_node` + `mp_r_*` cookies
- 302 重定向到 1Panel 主页

### GET /__mp/local

切换回主节点（清除 `mp_node` cookie），302 重定向到本机 1Panel。

### GET /__mp/touch

心跳端点：验证本机 1Panel 会话有效则签发 `mp_auth` cookie（204 No Content），否则 401。

---

## 根路径路由逻辑

```
GET / (及所有非 /__mp 路径)
  ├─ 有 mp_node cookie 且 Agent 在线 → 隧道反代到对应 Agent 的 1Panel
  │   ├─ WebSocket 请求 → proxyWebSocket（smux stream 双向透传）
  │   └─ HTTP 请求 → proxyHTTP（smux stream，text/html 注入侧边栏 JS）
  ├─ 无 mp_node 且 localProxy 已配置 → 反代本机 1Panel（内部端口）
  └─ 无 localProxy → 404
```

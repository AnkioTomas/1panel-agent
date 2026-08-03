# HTTP API 参考

Master 监听在原 1Panel 公网端口。

---

## 公开端点（HMAC：timestamp + sign）

`sign = hex(HMAC-SHA256(token, "timestamp=<unix>"))`，允许 ±5 分钟偏差。**不接受**裸 `token=`。

### GET /agent/ws?timestamp=\<ts\>&sign=\<sign\>

Agent WebSocket：校验签名 → 读 Register → smux Server → 登记 Registry。

### GET /agent.sh?timestamp=\<ts\>&sign=\<sign\>

Agent 安装脚本：

1. 签名下载 `/agent.bin`  
2. `agent install` 落盘  
3. 交互或 `PANEL_PASS` → `agent setpwd`  
4. systemd：`agent run`  

若本机已有 Master → 脚本直接失败。

### GET /agent.bin?timestamp=\<ts\>&sign=\<sign\>

Master 自身二进制；响应头 `X-1pm-GOOS` / `X-1pm-GOARCH`。

---

## 管理 API（需 mp_auth 或本机 1Panel 会话）

### GET /__mp/

节点管理页 HTML：安装命令、在线表（Agent/1Panel 版本、CPU、内存）、回主节点入口。

### GET /__mp/api/agents

先经 smux Stats 刷新各 Agent，再返回：

```json
[
  {
    "id": "a1b2c3d4e5f6a7b8",
    "hostname": "ubuntu-node2",
    "name": "机房A-web1",
    "group": "生产",
    "display_name": "机房A-web1",
    "panel_url": "http://127.0.0.1:52045",
    "remote_ip": "10.211.55.14",
    "panel_version": "v2.2.4",
    "agent_version": "v0.0.0-dev",
    "cpu_percent": 12.5,
    "mem_total": 8589934592,
    "mem_used": 2147483648,
    "open_url": "/__mp/go/a1b2c3d4e5f6a7b8"
  }
]
```

前端约每 5 秒轮询。

### GET /__mp/api/install-command

可选 query：`name`、`group`（写入安装脚本，Agent 上线后按此展示）。

```json
{
  "install": "curl -fsSL \"http://host:port/agent.sh?timestamp=...&sign=...&name=...&group=...\" | sudo bash"
}
```

「复制命令」会先调本接口再写入剪贴板。

### POST /__mp/api/rotate-token

轮换 Token；旧 Agent 全部失效。响应含新的 `install` 命令。

### GET /__mp/go/{id}

Agent 须在线；写 `mp_node` 后 302 到安全入口。自动登录发生在后续隧道请求的 **Agent 侧**。

### GET /__mp/local

清除 `mp_node`，302 回本机面板。

### GET /__mp/touch

保活：已登录则 204；否则 401。

---

## 根路径路由

```text
任意非 /__mp 路径
  ├─ mp_node 且 Agent 在线 → 隧道（WS 或 HTTP；HTML 注入 Hook）
  ├─ 无 mp_node → 本机 localProxy（内部端口）
  └─ 未配置 localProxy → 404
```

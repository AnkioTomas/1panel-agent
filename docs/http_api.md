# HTTP API 参考

Master 对外暴露以下 HTTP 端点（监听在原 1Panel 端口）。

---

## 公开端点（无需鉴权）

### GET /agent/ws?token=\<TOKEN\>

Agent WebSocket 接入点。

- 验证 `token` 查询参数
- 升级为 WebSocket，读取 Register 握手包
- 建立 smux 多路复用会话
- Agent 掉线时自动从注册表移除

### GET /agent.sh?token=\<TOKEN\>

返回 Agent 安装 shell 脚本（text/plain）。

- 脚本内嵌 Master 地址、Token、目标架构信息
- Agent 安装脚本会：下载 /agent.bin → 写 systemd 单元 → 启动服务
- Token 轮换后需重新执行此脚本

### GET /agent.bin?token=\<TOKEN\>

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
    "open_url": "/__mp/go/a1b2c3d4e5f6a7b8"
  }
]
```

### GET /__mp/api/upgrade-check

检查 Master 和所有 Agent 的 1Panel 版本是否有可用更新。

```json
{
  "master_version": "v2.4.0",
  "latest": "v2.4.1",
  "master_status": "outdated",
  "agents": [
    {
      "id": "a1b2c3d4e5f6a7b8",
      "version": "v2.4.1",
      "status": "latest"
    }
  ]
}
```

`status` 取值：`latest` | `outdated` | `unknown`

### POST /__mp/api/rotate-token

轮换隧道 Token。轮换后所有已注册 Agent 立即失效，需重新执行安装命令。

```json
{
  "install": "curl -fsSL \"http://10.211.55.14:52045/agent.sh?token=<新TOKEN>\" | sudo bash"
}
```

### GET /__mp/go/{id}

切换到指定 Agent 节点（需设置 `panel-pass`）。

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

# 设计决策说明

## 1. 为什么用根路径反代而不是路径前缀反代

1Panel 前端大量使用绝对路径加载静态资源，例如 `/assets/js/chunk.xxx.js`。

如果用路径前缀模式（如 `/node/xxx/assets/...`），前端无法正确加载这些绝对路径资源。

解决方案：用 **Cookie 选节点 + 根路径反代**：
- `mp_node` cookie 标识当前激活的 Agent
- 所有根路径请求通过 cookie 路由，1Panel 的 `/assets/` 绝对路径完全正常工作

## 2. 为什么需要 Cookie 命名空间隔离

同一浏览器需要同时维护两套会话：
- 本机 1Panel（`psession`、`pcsrftoken`）
- 远端 Agent 1Panel（同名 cookie）

如果直接共享 cookie jar，切换节点时本机会话会被覆盖。

解决方案：远端会话 cookie 统一加 `mp_r_` 前缀存储，经隧道发往 Agent 时自动剥除前缀还原为原始名称。

## 3. 为什么不读 core.db

1Panel 的 SQLite 数据库（`core.db`）格式可能随版本变化，且需要文件锁。

所有信息均通过官方 CLI 读取：
- 端口/用户名/安全入口：`1panel user-info`（或 `1pctl user-info`）
- 版本：`1pctl version`（或 `1panel version`）
- 改端口：`1panel update port`（stdin 输入端口号）

**绝不直接操作数据库**，保证跨版本兼容性。

## 4. 为什么 Agent 安装脚本直接从 Master 下载二进制

Master 把自身二进制通过 `/agent.bin` 端点对外提供。安装脚本直接从 Master 拉取。

好处：
- Agent 和 Master 必然版本一致，无兼容性问题
- Agent 机器不需要访问公网，只需能访问 Master
- Token 轮换后安装脚本随之更新，旧 Token 无法下载

## 5. Smux 而不是原始 WebSocket 帧

WebSocket 是单工的全双工流，不原生支持多路复用。

使用 `github.com/xtaci/smux` 在 WebSocket 连接之上建立多路复用层：
- Master 主动 `OpenStream()` 向 Agent 发起请求（反向隧道）
- 多个并发 HTTP 请求共用一条 WebSocket 连接
- 无需为每个请求建立独立 WebSocket 连接

## 6. 节点切换为什么用整页跳转而非 fetch

1Panel 是 Vue SPA，其路由基于 `history.pushState`。

如果用 fetch + Vue Router 内部跳转，URL 变化可能触发 Vue 重新渲染，且绝对路径资源（`/assets/`）已通过根路径反代解决，整页跳转最简单可靠。

## 7. 鉴权设计：不存储密码，复用现有登录态

`/__mp/` 的鉴权不要求用户额外输入密码：

1. 已登录本机 1Panel → 验证通过（转发 cookie 调 `/api/v2/dashboard/base/os`）
2. 签发 `mp_auth` cookie（HMAC-SHA256，以 Token 为密钥）
3. 后续请求直接验证 `mp_auth`，不再调用 1Panel

这样：
- 不需要额外的账号系统
- 1Panel 密码只在节点切换时需要（`--panel-pass`），且明文存在 master.json（需 root，权限 0600）
- `mp_auth` TTL 7天，合理的自动过期

## 8. 版本比较

版本比较实现在 `upgrade.go`，支持 semver 格式（`v2.4.1`），通过询问本机 1Panel 的 `/api/v2/core/settings/upgrade` 端点获取最新版本，复用已有的浏览器 1Panel 会话（不需要另存密码）。

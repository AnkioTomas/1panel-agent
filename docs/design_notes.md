# 设计决策说明

## 1. 根路径反代，而不是路径前缀

1Panel 前端大量使用绝对路径（如 `/assets/js/...`）。前缀反代（`/node/xxx/...`）会打断静态资源。

做法：`mp_node` Cookie 选节点 + **根路径**反代，绝对路径原样工作。

## 2. 本机会话在 Master，远端会话在 Agent

同一浏览器不能同时用两套同名 `psession`。做法：

- 切到子节点：Master 把本机面板 Cookie **暂存进进程内存**，并清掉浏览器里的面板 Cookie；设 `mp_node` 后 302
- 隧道请求 **不转发** 面板会话 Cookie；Agent 用内存会话自动登录，并把 `Set-Cookie` 写回浏览器（覆盖为远端会话）
- 切回主节点：清 `mp_node`，Master 把内存里的本机会话再 `Set-Cookie` 写回浏览器
- Master **不持有** Agent Cookie；不做切换预热

不在浏览器用 `mp_l_*` 暂存——远端自动登录会覆盖浏览器 Cookie，本机会话只能由 Master 持有。

## 3. 不读 core.db

端口 / 用户 / 入口 / 版本一律走官方 CLI（`1panel` / `1pctl`），不碰 SQLite，减少跨版本脆裂。

版本行解析需兼容中英文：`版本:` / `version:`（`1panel -l en` 为小写）。

## 4. Agent 从 Master 拉二进制

`/agent.bin` 即 Master 自身可执行文件。好处：版本对齐、Agent 无需公网、Token 轮换后旧签名立刻失效。

## 5. WebSocket 之上用 smux

单条 WS 不够开多路 HTTP。smux 让 Master 主动 `OpenStream`，并发 HTTP / WS / Stats 共用一条出站连接（反向隧道，NAT 友好）。

## 6. 节点切换用整页跳转

1Panel 是 Vue SPA。选节点后整页进 `/__mp/go/{id}`，躲开前端路由与绝对路径坑；不做 fetch + 假路由。

## 7. `/__mp/` 鉴权：复用本机登录，不存主节点密码

1. 校验浏览器对本机 1Panel 是否已登录（转发 Cookie 打 `/api/v2/dashboard/base/os`）  
2. 通过则签发内存 `sessionSecret` → `mp_auth` Cookie  
3. Master **不写**主节点面板密码  

子机自动登录密码只存在 **Agent**（`agent setpwd`，AES-GCM）。

## 8. 安装命令实时重签

HMAC 约 5 分钟有效。管理页「复制命令」同步请求 `/__mp/api/install-command` 再写入剪贴板（HTTP 下避免异步丢用户手势）。

## 9. 同机禁止 Master + Agent

Takeover 与 Agent 代理会争同一面板端口与职责。安装脚本与 `role` 包按 unit/状态文件互斥；`1pm uninstall` 按检测结果自动卸。

## 10. 不做官方式「检查更新」

社区场景不需要再包一层 1Panel upgrade API。管理页只展示 Agent / 1Panel **当前版本**（注册 + Stats 上报）。

1pm 自身更新另走通路：主节点 `POST /__mp/api/update-master` 从 Release 拉二进制；子节点 `POST /__mp/api/force-update` 从主节点 `/agent.bin` 同步。这与 1Panel 官方升级无关。

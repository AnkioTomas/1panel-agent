# 1pm — 1Panel 多机网关

Master 接管本机 1Panel 端口做统一入口；Agent 经 WebSocket 隧道接入（类 FRP）。浏览器可在 Master 上查看在线节点、复制注册命令，并一键切换到子节点面板（自动账号密码登录）。

> 安装名用 `1pm`，避免与官方 `/usr/bin/1panel-agent` 冲突。

## 架构

```text
Browser ──▶ Master(:面板端口)
              │
              ├─ /__mp/          节点管理 UI
              ├─ mp_node cookie  选中远程节点时，根路径隧道反代 Agent 本机 1Panel
              └─ 默认            反代本机 1Panel（takeover 后监听在 127.0.0.1:内部端口）

Agent ──WebSocket+Token──▶ Master
```

## 构建

```bash
go build -o bin/1pm ./cmd/1panel-agent
# 交叉编译到 Linux ARM64：
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/1pm ./cmd/1panel-agent
```

## Master（需 root）

自动读取 `/opt/1panel/db/core.db`：把本机 1Panel 挪到内部端口，Master 监听原端口并反代。

```bash
sudo ./bin/1pm master \
  --host 10.211.55.14 \
  --token mp-tunnel-secret \
  --panel-user ankio \
  --panel-pass 'your-password'
```

| 参数 | 说明 |
|------|------|
| `--host` | 给 Agent 复制注册命令用的对外 IP |
| `--token` | Agent 接入密钥（可省略，自动生成并落盘） |
| `--panel-user/pass` | 切换子节点时预登录用的 1Panel 账号 |
| `--no-takeover` | 不挪动本机 1Panel（需自行 `--listen` / `--upstream`） |

管理页：`http://<master>:<原面板端口>/__mp/`（**需先登录本机 1Panel**；未登录会跳转安全入口）

左侧菜单：Master 启动时写入 `HideMenu`「多机节点」，并注入脚本强制整页跳转（避开 Vue 路由）。

状态文件：`/var/lib/1pm/master.json`

systemd 示例：[`deploy/systemd/1pm-master.service`](deploy/systemd/1pm-master.service)

## Agent

```bash
# 可选：显式指定本机面板（默认会读 core.db）
sudo 1pm agent set --panel-url http://127.0.0.1:52045 --entrance tomas

# 注册并前台运行（命令可在 Master UI 一键复制）
sudo 1pm agent register 10.211.55.14:52045/mp-tunnel-secret
```

systemd 示例：[`deploy/systemd/1pm-agent.service`](deploy/systemd/1pm-agent.service)

## 使用

1. 打开 `http://master:端口/__mp/`
2. 复制「子节点注册命令」到 Agent 机器执行
3. 列表出现节点后点「进入面板」——Master 经隧道登录子节点；远程会话存在 `mp_r_*` cookie，**不会覆盖**本机 `psession`
4. 点「切换回本机 1Panel」只清除 `mp_node`，本机登录态保留，无需重新登录

## 实验室验证（已通过）

| 机器 | IP | 角色 |
|------|-----|------|
| ubuntu | 10.211.55.14 | Master + 本机 1Panel |
| ubuntu | 10.211.55.15 | Agent + 本机 1Panel |

验证结果：

- Master takeover：`1pm` 监听 `52045`，本机 1Panel 迁到 `62045`
- Agent 在线出现在 `/__mp/`
- `/__mp/go/{id}` 预登录成功（`psession`），隧道访问 `/api/v2/dashboard/base/os` 返回子机系统信息
- `/assets/js/...` 经 `mp_node` 根路径隧道可拉取（约 270KB）

## 开发测试

```bash
go test ./...
./deploy/docker/test.sh
```

## 说明

- 切换远程节点使用 **Cookie 选节点 + 根路径反代**，避免 1Panel 前端绝对路径 `/assets` 在前缀模式下失效。
- 登录密码按 1Panel v2 前端逻辑做 RSA+AES 混合加密。
- Takeover 会改 `ServerPort` 并 `systemctl restart 1panel-core`；卸载时需把端口改回或恢复 `master.json` 中的 `original_port`。

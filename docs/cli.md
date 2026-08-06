# CLI 命令参考

安装名 `1pm`（避免与官方 `/usr/bin/1panel-agent` 冲突）。版本由构建期注入：`-X 1panel-agent/internal/buildinfo.Version=...`。

---

## 顶层命令

```text
1pm <command>

  master                 启动 Master（takeover + 监听原面板端口）
  update                 按本机角色自更新 1pm 并重启对应服务
  agent install …        安装时写入配置（不启动长连接）
  agent run              用已有配置启动 Agent
  agent setpwd […]      设置本机 1Panel 密码（AES-GCM 加密存储）
  uninstall              自动检测本机角色并卸载
  version | -v | --version
  help  | -h | --help
```

---

## 1pm master

无额外参数。启动后：

1. 若本机已装 Agent → 拒绝（角色互斥）  
2. `EnsureTakeover`：把本机 1Panel 迁到内部端口，Master 监听原公网端口  
3. Token 空则自动生成并写入 `/var/lib/1pm/master.json`

```bash
1pm master
```

systemd：`ExecStart=/usr/local/bin/1pm master`

---

## 1pm update

无额外参数。按本机已安装角色更新二进制并重启服务：

| 角色   | 来源                                                                             | 重启单元             |
|--------|----------------------------------------------------------------------------------|----------------------|
| Master | GitHub Release（沿用 `master.json` 里安装时的 `GITHUB_*` / `INSTALL_CDN`）       | `1pm-master.service` |
| Agent  | GitHub Release（含 CDN/镜像，与 Master 同源；可用 `VERSION` / `INSTALL_CDN` 等） | `1pm-agent.service`  |

Master 已是最新 tag 时跳过下载与重启。可用环境变量 `VERSION=vX.Y.Z` 固定 Master 目标版本。

```bash
1pm update
VERSION=v0.1.0 1pm update   # 仅 Master：钉死 Release tag
```

建议：主节点与子节点均可直接 `1pm update`。管理页「更新子节点」只发隧道信号，等价于各子节点执行 `1pm update`（各自从 CDN
拉包，不再经主节点推二进制）。

---

## 1pm uninstall

按本机是否存在 Master unit/状态、Agent unit/配置自动清理；二进制只删一次。

```bash
1pm uninstall
```

检测路径（与安装脚本一致）：

| 角色 | 判定文件 |
|------|----------|
| Master | `/etc/systemd/system/1pm-master.service` 或 `/var/lib/1pm/master.json` |
| Agent | `/etc/systemd/system/1pm-agent.service` 或 `/root/.1panel-agent/agent.json` |

---

## 1pm agent install

写入 Master/Token；面板 URL/用户/安全入口由 `1panel user-info` 自动探测。若本机已装 Master → 拒绝。

```bash
1pm agent install <host:port> <token> [--name NAME] [--group GROUP]
```

也可用环境变量 `NODE_NAME` / `NODE_GROUP`。正常由 `/agent.sh` 调用；systemd 只跑 `agent run`。

---

## 1pm agent run

```bash
1pm agent run
```

---

## 1pm agent setpwd

密码 AES-GCM 加密存储（密钥 `~/.1panel-agent/secret.key`），供隧道内自动登录子机 1Panel。

```bash
1pm agent setpwd --password 'secret'
PANEL_PASS='secret' 1pm agent setpwd
1pm agent setpwd   # 交互输入
```

---

## 配置文件位置

| 角色 | 路径（root） |
|------|-------------|
| Master 状态 | `/var/lib/1pm/master.json` |
| Agent 配置 | `/root/.1panel-agent/agent.json` |
| Agent 密钥 | `/root/.1panel-agent/secret.key` |

非 root 时 Master/Agent 配置落在 `~/.1panel-agent/`（仅开发/测试；生产安装脚本以 root 为准）。

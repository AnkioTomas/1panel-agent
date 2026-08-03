# CLI 命令参考

安装名 `1pm`（避免与官方 `/usr/bin/1panel-agent` 冲突）。版本由构建期注入：`-X 1panel-agent/internal/buildinfo.Version=...`。

---

## 顶层命令

```text
1pm <command>

  master                 启动 Master（takeover + 监听原面板端口）
  master uninstall       仅卸载 Master（恢复端口、清状态、删二进制）
  agent install …        安装时写入配置（不启动长连接）
  agent run              用已有配置启动 Agent
  agent setpwd […]      设置本机 1Panel 密码（AES-GCM 加密存储）
  agent uninstall        仅卸载 Agent
  uninstall              自动检测本机角色并卸载（推荐）
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

## 1pm uninstall

按本机是否存在 Master unit/状态、Agent unit/配置自动清理；二进制只删一次。

```bash
1pm uninstall
1pm master uninstall
1pm agent uninstall
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
1pm agent install <host:port>/<token> [--name NAME] [--group GROUP]
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

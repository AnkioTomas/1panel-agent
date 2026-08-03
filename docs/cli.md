# CLI 命令参考

## 二进制名称

安装名 `1pm`（避免与官方 `/usr/bin/1panel-agent` 冲突）

---

## 顶层命令

```
1pm <command>

命令：
  master        启动 Master 节点
  agent         Agent 子命令
  version       显示版本
  help          显示帮助
```

---

## 1pm master

启动 Master 网关（默认启用 takeover）。

```bash
1pm master [选项]

选项：
  --listen  <addr>      监听地址（默认自动使用原 1Panel 端口，如 :52045）
  --token   <token>     覆盖隧道 Token（默认从 master.json 读取或自动生成）
  --host    <host>      公网/NAT 地址覆盖（用于生成 Agent 安装命令）
  --panel-user <user>   面板用户名（默认从 1pctl user-info 读取）
  --panel-pass <pass>   面板密码（节点切换自动登录用）
  --entrance <path>     安全入口路径（默认从 1panel user-info 读取）
  --no-takeover         不执行端口接管
  --upstream <url>      手动指定本机 1Panel 上游地址（如 http://127.0.0.1:62045）
```

**systemd 示例**：

```ini
[Service]
ExecStart=/usr/local/bin/1pm master
```

---

## 1pm master set

修改 master.json 配置（不重启服务立即生效，重启后生效）。

```bash
1pm master set [选项]

选项：
  --host      <host>   设置公网地址
  --panel-user <user>  设置面板用户名
  --panel-pass <pass>  设置面板密码（节点切换自动登录必需）
  --token     <token>  手动设置 Token
  --entrance  <path>   设置安全入口
```

**典型用法**：

```bash
# 设置面板密码（节点切换自动登录必需）
1pm master set --panel-pass 'MySecretPassword'

# 设置 NAT 公网地址（生成的 Agent 安装命令使用此地址）
1pm master set --host 1.2.3.4
```

---

## 1pm agent register

注册并启动 Agent（一步完成）。

```bash
1pm agent register <master-host>:<port>/<token>

示例：
1pm agent register 10.211.55.14:52045/abc123def456
```

格式：支持 `http://`、`https://`、`ws://`、`wss://` 前缀（自动剥除）。

---

## 1pm agent run

用已有配置启动 Agent（需先执行过 register）。

```bash
1pm agent run
```

---

## 1pm agent set

修改 Agent 配置。

```bash
1pm agent set --panel-url <url> [--panel-key <key>]

选项：
  --panel-url <url>  手动指定本机 1Panel 地址
  --panel-key <key>  设置 API Key
```

---

## 配置文件位置

| 角色 | 路径（root） | 路径（非 root） |
|------|-------------|----------------|
| Master 状态 | `/var/lib/1pm/master.json` | `~/.1panel-agent/master.json` |
| Agent 配置 | `~/.1panel-agent/agent.json` | `~/.1panel-agent/agent.json` |

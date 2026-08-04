# 核心数据结构

## config.Agent

**文件**：`/root/.1panel-agent/agent.json`（0600）

```go
type Agent struct {
    ID               string // 首次 Save 自动生成
    Master           string // host:port
    Token            string // 与 Master 共享的 HMAC 密钥
    MasterTLS        bool   // Master 开面板 SSL 时用 wss
    Name             string
    Group            string
    PanelURL         string // 本机 1Panel，CLI 探测
    PanelUser        string
    PanelEntrance    string // 安全入口路径段，CLI 探测
    PanelPasswordEnc string // agent setpwd → AES-GCM
}
```

## config.Master

**文件**：root 下 `/var/lib/1pm/master.json`

```go
type Master struct {
    Token        string // 隧道 HMAC 密钥；UI 可轮换
    OriginalPort int    // takeover 前公网端口
    InternalPort int    // 1Panel 内部端口（仅 127.0.0.1）
    PublicHost   string // 可选；安装命令宣告地址，默认用请求 Host
    GitHubAPI    string // 安装时 Release 源，供自更新复用
    GitHubDL     string
    InstallCDN   string // auto | global | cn
}
```

Master **不存储**面板密码。

## protocol.Register（Agent → Master）

```go
type Register struct {
    ID           string
    Hostname     string
    Name         string // 展示名，可选
    Group        string // 分组，可选
    PanelURL     string
    PanelVersion string // 1panel -l en version
    AgentVersion string // buildinfo.Version
}
```

## protocol.HostStats（Stats 流响应）

```go
type HostStats struct {
    CPUPercent   float64
    MemTotal     uint64
    MemUsed      uint64
    AgentVersion string
    PanelVersion string
    GOOS, GOARCH string
}
```

## protocol.RequestMeta / ResponseMeta

```go
// Type 写在 JSON 之前 1 字节：1=HTTP, 2=WS, 3=Stats
type RequestMeta struct {
    Type    byte
    Method  string
    Path    string
    Headers map[string][]string
}

type ResponseMeta struct {
    Status  int
    Headers map[string][]string
}
```

## master.AgentInfo / Session

```go
type AgentInfo struct {
    ID, Hostname, Name, Group, PanelURL, RemoteIP string
    PanelVersion, AgentVersion                    string
    CPUPercent                                    float64
    MemTotal, MemUsed                             uint64
    StatsAt                                       int64 // unix 秒
}

type Session struct {
    Info AgentInfo
    Mux  *smux.Session
}
```

---

## 二进制协议帧

### JSON 帧

```text
[4 字节大端长度][JSON]
上限 16 MiB
```

### 分块 Body

```text
[4 字节长度][数据] … [4 字节 0] 结束
单块上限 8 MiB
```

### 请求流开头

```text
[1 字节 Type][RequestMeta JSON 帧][分块 Body]
```

### HMAC 签名（下载 / WS）

```text
sign = hex(HMAC-SHA256(token, "timestamp=<unix>"))
允许时钟偏差 ±5 分钟；禁止裸 token= 查询参数
```

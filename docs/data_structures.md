# 核心数据结构

## config.Agent（Agent 配置）

**文件**：`~/.1panel-agent/agent.json`（权限 0600）

```go
type Agent struct {
    ID       string // 节点唯一标识，8 字节随机 hex，首次 Save 自动生成
    Master   string // Master 地址，格式 host:port
    Token    string // 接入令牌
    PanelURL string // 本机 1Panel 地址，默认 http://127.0.0.1:20560，自动检测
    PanelKey string // 可选 API Key，注入 X-Panel-Key 请求头
}
```

## config.Master（Master 状态）

**文件**：root 时用 `/var/lib/1pm/master.json`，否则 `~/.1panel-agent/master.json`（权限 0600）

```go
type Master struct {
    Token         string // 隧道接入令牌，自动生成，UI 可轮换
    OriginalPort  int    // takeover 前 1Panel 原始端口
    InternalPort  int    // 1Panel 迁移后的内部端口（仅 127.0.0.1 监听）
    Entrance      string // 1Panel 安全入口路径（如 "myentrance"）
    PanelUser     string // 面板用户名（从 1pctl user-info 读取，不存密码）
    PanelPassword string // 面板密码（仅用于节点切换自动登录）
    PublicHost    string // NAT 公网地址覆盖，默认用 HTTP Host 头
}
```

## protocol.Register（握手消息：Agent→Master）

```go
type Register struct {
    ID           string // Agent ID
    Hostname     string // os.Hostname()
    PanelURL     string // 本机 1Panel 地址
    PanelVersion string // 1Panel 版本（可选）
}
```

## protocol.RequestMeta（隧道请求头：Master→Agent）

```go
type RequestMeta struct {
    Type    byte                // 0x01=HTTP, 0x02=WebSocket（写在 JSON 之前）
    Method  string              // HTTP 方法
    Path    string              // 请求路径（含查询串）
    Headers map[string][]string // HTTP 请求头（已处理 Cookie 命名空间）
}
```

## protocol.ResponseMeta（隧道响应头：Agent→Master）

```go
type ResponseMeta struct {
    Status  int                 // HTTP 状态码
    Headers map[string][]string // HTTP 响应头
}
```

## master.AgentInfo（在线节点信息，内存）

```go
type AgentInfo struct {
    ID           string    // Agent ID
    Hostname     string    // 主机名
    PanelURL     string    // Agent 本机 1Panel 地址
    RemoteIP     string    // WebSocket 来源 IP
    PanelVersion string    // 1Panel 版本
}
```

## master.Session（在线会话，内存）

```go
type Session struct {
    Info AgentInfo
    Mux  *smux.Session  // smux 多路复用会话（Master 侧为 Server 模式）
}
```

---

## 二进制协议帧格式

### JSON 帧（握手 + 元数据）

```
[4字节 Big-Endian 长度][JSON 字节流]
最大帧大小：16 MiB
```

### 分块流（Body 传输）

```
[4字节长度][数据] ... [4字节 0x00000000]（结束标志）
最大单块：8 MiB
```

### RequestMeta 帧（流开头）

```
[1字节 Type][4字节长度][RequestMeta JSON]
```

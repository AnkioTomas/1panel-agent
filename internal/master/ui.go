package master

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/hoststats"
	"1panel-agent/internal/panel"
)

// handleMP 处理 /__mp/：鉴权后分发 API、节点切换与管理页。
func (s *Server) handleMP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/__mp")

	// 切回本机必须先于鉴权：此时浏览器里是远端 psession，
	// localPanelCodeOK 会失败；本机会话在 Master 内存里，由 handleLocal 写回。
	if path == "/local" {
		s.handleLocal(w, r)
		return
	}

	// 统一鉴权门禁：校验失败直接拦截（API 返 401，页面请求重定向）
	if !s.ensureMPAuth(w, r, path) {
		return
	}

	if path == "/touch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch path {
	case "/api/agents":
		s.apiAgents(w, r)
		return
	case "/api/install-command":
		s.handleInstallCommand(w, r)
		return
	case "/api/rotate-token":
		s.handleRotateToken(w, r)
		return
	case "/api/force-update":
		s.handleForceUpdate(w, r)
		return
	case "/api/update-master":
		s.handleUpdateMaster(w, r)
		return
	case "/api/upgrade-panel":
		s.handleUpgradePanel(w, r)
		return
	case "/api/upgrade-panel-master":
		s.handleUpgradePanelMaster(w, r)
		return
	case "/api/panel-ssl":
		s.handlePanelSSL(w, r)
		return
	}

	if path == "" || path == "/" {
		s.renderNodes(w, r)
		return
	}
	switch {
	case strings.HasPrefix(path, "/go/"):
		id := strings.TrimPrefix(path, "/go/")
		id = strings.Trim(id, "/")
		s.handleSwitch(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// apiAgents 拉取各 Agent 最新资源快照后返回在线列表（含主节点）。
func (s *Server) apiAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.refreshAgentStats()
	type item struct {
		ID           string  `json:"id"`
		Hostname     string  `json:"hostname"`
		Name         string  `json:"name,omitempty"`
		Group        string  `json:"group,omitempty"`
		DisplayName  string  `json:"display_name"`
		IsMaster     bool    `json:"is_master,omitempty"`
		PanelURL     string  `json:"panel_url"`
		RemoteIP     string  `json:"remote_ip"`
		PanelVersion string  `json:"panel_version"`
		AgentVersion string  `json:"agent_version"`
		CPUPercent   float64 `json:"cpu_percent"`
		MemTotal     uint64  `json:"mem_total"`
		MemUsed      uint64  `json:"mem_used"`
		OpenURL      string  `json:"open_url"`
	}

	hs := hoststats.Collect()
	hostName, _ := os.Hostname()
	master := item{
		ID:           "local",
		Hostname:     hostName,
		Name:         "主节点",
		Group:        "主节点",
		DisplayName:  "主节点",
		IsMaster:     true,
		PanelURL:     s.LocalPanel,
		RemoteIP:     s.displayHost(r),
		PanelVersion: panel.ReadSystemVersion(),
		AgentVersion: buildinfo.Version,
		CPUPercent:   hs.CPUPercent,
		MemTotal:     hs.MemTotal,
		MemUsed:      hs.MemUsed,
		OpenURL:      "/__mp/local",
	}

	list := s.reg.List()
	out := make([]item, 0, len(list)+1)
	out = append(out, master)
	for _, a := range list {
		out = append(out, item{
			ID:           a.ID,
			Hostname:     a.Hostname,
			Name:         a.Name,
			Group:        a.Group,
			DisplayName:  a.DisplayName(),
			PanelURL:     a.PanelURL,
			RemoteIP:     a.RemoteIP,
			PanelVersion: a.PanelVersion,
			AgentVersion: a.AgentVersion,
			CPUPercent:   a.CPUPercent,
			MemTotal:     a.MemTotal,
			MemUsed:      a.MemUsed,
			OpenURL:      "/__mp/go/" + a.ID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// pageData 是管理页模板数据。
type pageData struct {
	Agents        []AgentInfo
	Register      string
	Host          string
	DeviceIP      string
	Entrance      string
	LocalPanel    string
	MasterVersion string // 1Panel 版本
	NodeVersion   string // 1pm 版本
	Online        int
}

// renderNodes 渲染 /__mp/ 节点管理页。
func (s *Server) renderNodes(w http.ResponseWriter, r *http.Request) {
	host := s.AdvertiseHost(r)
	hs := hoststats.Collect()
	hostName, _ := os.Hostname()
	masterInfo := AgentInfo{
		ID:           "local",
		Hostname:     hostName,
		Name:         "主节点",
		Group:        "主节点",
		RemoteIP:     s.displayHost(r),
		PanelVersion: panel.ReadSystemVersion(),
		AgentVersion: buildinfo.Version,
		CPUPercent:   hs.CPUPercent,
		MemTotal:     hs.MemTotal,
		MemUsed:      hs.MemUsed,
	}
	agents := append([]AgentInfo{masterInfo}, s.reg.List()...)
	data := pageData{
		Agents:        agents,
		Register:      s.InstallCommand(r),
		Host:          host,
		DeviceIP:      s.displayHost(r),
		Entrance:      s.Entrance,
		LocalPanel:    s.LocalPanel,
		MasterVersion: masterInfo.PanelVersion,
		NodeVersion:   buildinfo.Version,
		Online:        len(s.reg.List()),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = nodesTmpl.Execute(w, data)
}

// nodesTmpl 是 /__mp/ 管理页 HTML 模板。
var nodesTmpl = template.Must(template.New("nodes").Parse(`<!DOCTYPE html>
<html lang="zh-CN" class="light">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>节点管理 - 1Panel</title>
<script>
(function(){
  function panelTheme(){
    try{
      var raw=localStorage.getItem('GlobalState');
      if(!raw) return '';
      var st=JSON.parse(raw);
      var t=(st&&st.themeConfig&&st.themeConfig.theme)||'';
      return t==='dark'||t==='light'||t==='auto' ? t : '';
    }catch(e){ return ''; }
  }
  function resolve(mode){
    if(mode==='dark'||mode==='light') return mode;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  function apply(){
    var mode=resolve(panelTheme()||'auto');
    document.documentElement.className=mode;
    document.documentElement.style.colorScheme=mode;
  }
  apply();
  try{
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function(){
      var t=panelTheme();
      if(!t || t==='auto') apply();
    });
  }catch(e){}
  window.addEventListener('storage', function(ev){
    if(ev.key==='GlobalState') apply();
  });
})();
</script>
<style>
/* 对齐 1Panel / Element Plus 变量；html.light / html.dark 切换 */
html.light, :root{
  --el-color-primary:#005eeb;
  --el-color-primary-dark-2:#0052cc;
  --el-color-primary-light-3:#4d8ef0;
  --el-color-primary-light-5:#80aef5;
  --el-color-primary-light-7:#b3cef9;
  --el-color-primary-light-8:#cce0fb;
  --el-color-primary-light-9:#e6f0fd;
  --el-color-success:#67c23a;
  --el-color-danger:#f56c6c;
  --el-bg-color:#ffffff;
  --el-bg-color-page:#f0f2f5;
  --el-bg-color-overlay:#ffffff;
  --el-text-color-primary:#303133;
  --el-text-color-regular:#606266;
  --el-text-color-secondary:#909399;
  --el-text-color-placeholder:#a8abb2;
  --el-border-color:#dcdfe6;
  --el-border-color-light:#e4e7ed;
  --el-border-color-lighter:#ebeef5;
  --el-fill-color:#f0f2f5;
  --el-fill-color-light:#f5f7fa;
  --el-fill-color-blank:#ffffff;
  --el-mask-color:rgba(255,255,255,.9);
  --mp-shadow:0 1px 4px rgba(0,21,41,.08);
  --mp-radius:4px;
  --mp-success-bg:#f0f9eb;
  --mp-success-border:#e1f3d8;
  --mp-success-text:#67c23a;
  --mp-danger-bg:#fef0f0;
  --mp-danger-border:#fde2e2;
  --mp-danger-text:#f56c6c;
}
html.dark{
  --el-color-primary:#3d8eff;
  --el-color-primary-dark-2:#3d8eff;
  --el-color-primary-light-3:#3364ad;
  --el-color-primary-light-5:#372e46;
  --el-color-primary-light-7:#2d4a7a;
  --el-color-primary-light-8:#2a4066;
  --el-color-primary-light-9:#2e313d;
  --el-color-success:#3fb950;
  --el-color-danger:#e2324f;
  --el-bg-color:#2e313d;
  --el-bg-color-page:#242633;
  --el-bg-color-overlay:#2e313d;
  --el-text-color-primary:#c0c2cf;
  --el-text-color-regular:#c0c2cf;
  --el-text-color-secondary:#9597a4;
  --el-text-color-placeholder:#787b88;
  --el-border-color:#434552;
  --el-border-color-light:#434552;
  --el-border-color-lighter:#434552;
  --el-fill-color:#242633;
  --el-fill-color-light:#2e313d;
  --el-fill-color-blank:#2e313d;
  --el-mask-color:rgba(36,38,51,.9);
  --mp-shadow:0 1px 4px rgba(0,0,0,.35);
  --mp-success-bg:#1f3a2a;
  --mp-success-border:#2d5a3d;
  --mp-success-text:#3fb950;
  --mp-danger-bg:#3a2428;
  --mp-danger-border:#5a3038;
  --mp-danger-text:#e9657b;
}
*{box-sizing:border-box}
body{
  margin:0;
  min-height:100vh;
  font-family:Helvetica Neue,Helvetica,PingFang SC,Hiragino Sans GB,Microsoft YaHei,Arial,sans-serif;
  background:var(--el-bg-color-page);
  color:var(--el-text-color-primary);
  transition:background-color .2s,color .2s;
}
.topbar{
  height:56px;
  background:var(--el-bg-color);
  border-bottom:1px solid var(--el-border-color-light);
  display:flex;align-items:center;justify-content:space-between;
  padding:0 20px;
  box-shadow:var(--mp-shadow);
}
.brand{display:flex;align-items:center;gap:10px;font-weight:600;font-size:16px;color:var(--el-text-color-primary)}
.brand svg{color:var(--el-color-primary)}
.device{
  display:flex;align-items:center;gap:8px;
  color:var(--el-text-color-regular);font-size:13px;
  background:var(--el-fill-color-light);border:1px solid var(--el-border-color-light);
  border-radius:20px;padding:6px 12px;
}
.device strong{color:var(--el-color-primary);font-weight:600}
.wrap{max-width:1080px;margin:0 auto;padding:20px}
.stats{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-bottom:16px}
.stat{
  background:var(--el-bg-color);border:1px solid var(--el-border-color-light);border-radius:var(--mp-radius);
  padding:16px 18px;box-shadow:var(--mp-shadow);
}
.stat .label{color:var(--el-text-color-secondary);font-size:13px;margin-bottom:8px}
.stat .value{font-size:22px;font-weight:600;color:var(--el-text-color-primary)}
.card{
  background:var(--el-bg-color);border:1px solid var(--el-border-color-light);border-radius:var(--mp-radius);
  box-shadow:var(--mp-shadow);margin-bottom:16px;overflow:hidden;
}
.card-hd{
  padding:14px 18px;border-bottom:1px solid var(--el-border-color-lighter);
  display:flex;align-items:center;justify-content:space-between;gap:12px;
}
.card-hd h2{margin:0;font-size:15px;font-weight:600;color:var(--el-text-color-primary)}
.card-bd{padding:18px}
.section-title{margin:18px 0 10px;font-size:14px;font-weight:600;color:var(--el-text-color-primary)}
.section-title:first-of-type{margin-top:4px}
.field{display:flex;flex-direction:column;gap:4px;min-width:160px;flex:1}
.field > span{font-size:12px;color:var(--el-text-color-secondary)}
.field input,.mp-input{
  padding:8px 10px;border:1px solid var(--el-border-color);border-radius:var(--mp-radius);
  background:var(--el-fill-color-blank);color:var(--el-text-color-primary);font:inherit;
  outline:none;transition:border-color .15s;
}
.field input:focus,.mp-input:focus{border-color:var(--el-color-primary)}
.field input::placeholder{color:var(--el-text-color-placeholder)}
.meta-row{display:flex;flex-wrap:wrap;gap:12px;margin-bottom:12px}
.cmd{display:flex;gap:10px;align-items:stretch}
.cmd code{
  flex:1;background:var(--el-fill-color-light);border:1px solid var(--el-border-color-light);border-radius:var(--mp-radius);
  padding:10px 12px;font-size:13px;color:var(--el-text-color-regular);overflow:auto;white-space:nowrap;
  font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;
}
.btn{
  border:1px solid transparent;border-radius:var(--mp-radius);
  background:var(--el-color-primary);color:#fff;
  padding:8px 16px;font-size:13px;font-weight:500;cursor:pointer;
  text-decoration:none;display:inline-flex;align-items:center;justify-content:center;gap:6px;
  line-height:1.4;white-space:nowrap;transition:background .15s,border-color .15s,color .15s;
}
.btn:hover{background:var(--el-color-primary-dark-2)}
.btn.plain{
  background:var(--el-bg-color);color:var(--el-text-color-regular);border-color:var(--el-border-color);
}
.btn.plain:hover{color:var(--el-color-primary);border-color:var(--el-color-primary-light-5);background:var(--el-color-primary-light-9)}
.btn.primary-panel{background:var(--el-color-primary)}
.btn.primary-panel:hover{background:var(--el-color-primary-dark-2)}
.btn:disabled{opacity:.6;cursor:not-allowed}
.meta{margin:10px 0 0;color:var(--el-text-color-secondary);font-size:12px}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:12px 10px;border-bottom:1px solid var(--el-border-color-lighter);font-size:13px;vertical-align:middle;color:var(--el-text-color-regular)}
th{color:var(--el-text-color-secondary);font-weight:500;background:var(--el-fill-color-light)}
tr:hover td{background:var(--el-fill-color-light)}
tr:last-child td{border-bottom:0}
tr.group-row td{padding:10px 18px;background:var(--el-fill-color)!important;font-weight:600;color:var(--el-text-color-regular)}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--el-color-success);margin-right:6px}
.tag{
  display:inline-flex;align-items:center;padding:2px 8px;border-radius:4px;
  background:var(--mp-success-bg);color:var(--mp-success-text);font-size:12px;border:1px solid var(--mp-success-border);
}
.empty{color:var(--el-text-color-secondary);padding:24px 0;text-align:center;font-size:13px}
.actions{display:flex;flex-wrap:wrap;gap:10px}
.muted{color:var(--el-text-color-secondary)}
.toast{
  position:fixed;right:20px;bottom:20px;background:var(--mp-success-bg);color:var(--mp-success-text);
  border:1px solid var(--mp-success-border);padding:10px 14px;border-radius:var(--mp-radius);
  opacity:0;transform:translateY(8px);transition:.2s;font-size:13px;z-index:99;
}
.toast.show{opacity:1;transform:translateY(0)}
.toast.err{background:var(--mp-danger-bg);color:var(--mp-danger-text);border-color:var(--mp-danger-border)}
@media (max-width:800px){
  .stats{grid-template-columns:1fr}
  .cmd{flex-direction:column}
}
</style>
</head>
<body>
<header class="topbar">
  <div class="brand">
    <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <path fill="currentColor" d="M4 5h16a1 1 0 0 1 1 1v4H3V6a1 1 0 0 1 1-1zm-1 7h18v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6zm3 2v2h2v-2H6zm4 0v2h2v-2h-2z"/>
    </svg>
    <span>节点管理</span>
  </div>
  <div class="device" title="宣告主机（PublicHost 或当前访问 Host）">
    <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="M12 2a10 10 0 1 0 .001 20.001A10 10 0 0 0 12 2zm0 2a8 8 0 0 1 7.75 6H4.25A8 8 0 0 1 12 4zm0 16a8 8 0 0 1-7.75-6h15.5A8 8 0 0 1 12 20z"/></svg>
    <strong>{{if .DeviceIP}}{{.DeviceIP}}{{else}}-{{end}}</strong>
  </div>
</header>

<div class="wrap">
  <div class="stats">
    <div class="stat">
      <div class="label">在线 Agent</div>
      <div class="value" id="statOnline">{{.Online}}</div>
    </div>
    <div class="stat">
      <div class="label">注册地址</div>
      <div class="value" style="font-size:14px;word-break:break-all">{{.Host}}</div>
    </div>
    <div class="stat">
      <div class="label">安全入口</div>
      <div class="value" style="font-size:16px">{{if .Entrance}}/{{.Entrance}}{{else}}-{{end}}</div>
    </div>
  </div>

  <div class="card">
    <div class="card-hd">
      <h2>子节点安装命令</h2>
      <button class="btn plain" type="button" id="btnRotate" onclick="rotateToken()">轮换 Token</button>
    </div>
    <div class="card-bd">
      <div class="meta-row">
        <label class="field">
          <span>节点名称</span>
          <input id="nodeName" class="mp-input" type="text" maxlength="64" placeholder="如：机房A-web1">
        </label>
        <label class="field">
          <span>节点分组</span>
          <input id="nodeGroup" class="mp-input" type="text" maxlength="64" placeholder="如：生产 / 测试">
        </label>
      </div>
      <div class="cmd">
        <code id="reg">{{.Register}}</code>
        <button class="btn" type="button" id="btnCopy" onclick="copyReg()">复制命令</button>
      </div>
      <p class="meta">填写名称/分组后复制：命令会写入安装脚本，Agent 上线后按分组显示。复制时重新签发 HMAC（约 5 分钟有效）。轮换 Token 后需重新安装。{{if .Entrance}}安全入口 {{.Entrance}}{{end}}</p>
    </div>
  </div>

  <div class="card">
    <div class="card-hd">
      <h2>在线节点</h2>
      <button class="btn plain" type="button" onclick="refreshAgents()">刷新</button>
    </div>
    <div class="card-bd" style="padding:0">
      <div id="agentsWrap">
      {{if .Agents}}
      <table>
        <thead>
          <tr>
            <th style="padding-left:18px">状态</th>
            <th>名称</th>
            <th>IP</th>
            <th>Agent</th>
            <th>1Panel</th>
            <th>CPU</th>
            <th>内存</th>
            <th style="width:120px"></th>
          </tr>
        </thead>
        <tbody id="agentsBody">
        {{range .Agents}}
          <tr data-agent-id="{{.ID}}">
            <td style="padding-left:18px"><span class="tag"><span class="dot"></span>在线</span></td>
            <td>{{.DisplayName}}</td>
            <td class="muted">{{if .RemoteIP}}{{.RemoteIP}}{{else}}-{{end}}</td>
            <td>{{if .AgentVersion}}{{.AgentVersion}}{{else}}-{{end}}</td>
            <td>{{if .PanelVersion}}{{.PanelVersion}}{{else}}-{{end}}</td>
            <td class="col-cpu">{{printf "%.1f%%" .CPUPercent}}</td>
            <td class="col-mem">-</td>
            <td>{{if eq .ID "local"}}<a class="btn primary-panel" href="/__mp/local">进入面板</a>{{else}}<a class="btn primary-panel" href="/__mp/go/{{.ID}}">进入面板</a>{{end}}</td>
          </tr>
        {{end}}
        </tbody>
      </table>
      {{else}}
      <p class="empty" id="agentsEmpty">暂无在线 Agent，请先在子节点执行上方注册命令</p>
      {{end}}
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-hd"><h2>节点管理</h2></div>
    <div class="card-bd">
      <p class="meta" style="margin:0 0 14px">
        当前节点 <strong>主节点</strong>
        · 1pm <strong id="nodeVer">{{if .NodeVersion}}{{.NodeVersion}}{{else}}-{{end}}</strong>
        · 1Panel <strong>{{if .MasterVersion}}{{.MasterVersion}}{{else}}-{{end}}</strong>
        · 上游 {{.LocalPanel}}
        · <a class="btn primary-panel" href="/__mp/local" style="margin-left:6px">切换回主节点 1Panel</a>
      </p>

      <h3 class="section-title">1panel-Agent 管理面板</h3>
      <div class="actions">
        <button class="btn plain" type="button" id="btnUpdateMaster" onclick="updateMaster()">更新主节点 1pm</button>
        <button class="btn plain" type="button" id="btnForceUpdate" onclick="forceUpdate()">更新子节点 1pm</button>
      </div>

      <h3 class="section-title">1Panel 更新</h3>
      <div class="actions">
        <button class="btn plain" type="button" id="btnUpgradePanelMaster" onclick="upgradePanelMaster()">更新主节点 1Panel</button>
        <button class="btn plain" type="button" id="btnUpgradePanel" onclick="upgradePanel()">更新子节点 1Panel</button>
      </div>

      <h3 class="section-title">1Panel SSL</h3>
      <div class="actions">
        <button class="btn plain" type="button" id="btnSSLOn" onclick="panelSSL(true)">开启主节点 SSL</button>
        <button class="btn plain" type="button" id="btnSSLOff" onclick="panelSSL(false)">关闭主节点 SSL</button>
      </div>
      <p class="meta" style="margin:14px 0 0">1pm：先更新主节点再更新子节点（主节点沿用安装时的 Release 源，已最新则跳过）。1Panel：调用官方升级 API（已最新则跳过）。主节点 SSL：加密公网入口与 Agent 隧道（wss）；子节点本机面板始终 Bind 127.0.0.1 且禁用 SSL，不对外、不二次套 TLS。</p>
    </div>
  </div>
</div>

<div class="toast" id="toast">复制成功</div>
<script>
function showToast(text, err){
  const el=document.getElementById('toast');
  el.textContent=text;
  el.classList.toggle('err', !!err);
  el.classList.add('show');
  setTimeout(()=>{el.classList.remove('show'); el.classList.remove('err');},1500);
}
function fmtBytes(n){
  n=Number(n)||0;
  if(n<=0) return '-';
  const u=['B','KB','MB','GB','TB'];
  let i=0;
  while(n>=1024 && i<u.length-1){n/=1024;i++;}
  return (i===0?n:n.toFixed(1))+u[i];
}
function fmtCPU(v){
  if(v==null || isNaN(v)) return '-';
  return Number(v).toFixed(1)+'%';
}
// HTTP 非安全上下文 / 异步后丢失用户手势时，clipboard API 会失败；用 textarea 回退。
function fallbackCopy(text){
  const ta=document.createElement('textarea');
  ta.value=text;
  ta.setAttribute('readonly','');
  ta.style.cssText='position:fixed;left:-9999px;top:0';
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  ta.setSelectionRange(0, ta.value.length);
  let ok=false;
  try{ ok=document.execCommand('copy'); }catch(e){}
  document.body.removeChild(ta);
  return ok;
}
function installQuery(){
  const name=(document.getElementById('nodeName')||{}).value||'';
  const group=(document.getElementById('nodeGroup')||{}).value||'';
  const q=[];
  if(name.trim()) q.push('name='+encodeURIComponent(name.trim()));
  if(group.trim()) q.push('group='+encodeURIComponent(group.trim()));
  return q.length?('?'+q.join('&')):'';
}
function applyInstallCmd(data){
  if(!data || !data.install) throw new Error('empty command');
  document.getElementById('reg').textContent=data.install;
  return data.install;
}
function fetchInstallCmd(){
  return fetch('/__mp/api/install-command'+installQuery(),{credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    const ct=r.headers.get('content-type')||'';
    if(ct.indexOf('json')<0) throw new Error('接口返回非 JSON');
    return r.json();
  }).then(applyInstallCmd);
}
// 同步拉取：必须在 click 手势内完成，否则 HTTP 下剪贴板会被浏览器拒绝。
function fetchInstallCmdSync(){
  const xhr=new XMLHttpRequest();
  xhr.open('GET','/__mp/api/install-command'+installQuery(),false);
  xhr.withCredentials=true;
  xhr.send(null);
  if(xhr.status!==200) throw new Error('HTTP '+xhr.status);
  let data;
  try{ data=JSON.parse(xhr.responseText); }catch(e){ throw new Error('接口返回非 JSON'); }
  return applyInstallCmd(data);
}
function copyReg(){
  const btn=document.getElementById('btnCopy');
  btn.disabled=true;
  try{
    const text=fetchInstallCmdSync();
    if(!fallbackCopy(text)){
      const el=document.getElementById('reg');
      const range=document.createRange();
      range.selectNodeContents(el);
      const sel=window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      showToast('请按 Ctrl+C / ⌘C 复制', true);
    }else{
      showToast('复制成功');
    }
  }catch(e){
    showToast('复制失败: '+(e&&e.message?e.message:e), true);
  }
  btn.disabled=false;
}
function rotateToken(){
  if(!confirm('轮换后旧安装命令与已注册 Agent 立即失效，需重新 curl|bash。继续？')) return;
  const btn=document.getElementById('btnRotate');
  btn.disabled=true; btn.textContent='轮换中…';
  fetch('/__mp/api/rotate-token',{method:'POST',credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
  }).then(data=>{
    if(data.install) document.getElementById('reg').textContent=data.install;
    showToast('Token 已轮换');
  }).catch(e=>{
    alert('轮换失败: '+e.message);
  }).finally(()=>{
    btn.disabled=false; btn.textContent='轮换 Token';
  });
}
function updateMaster(){
  if(!confirm('将从 Release 下载最新 1pm，替换本机二进制并重启主节点服务。继续？')) return;
  const btn=document.getElementById('btnUpdateMaster');
  btn.disabled=true; btn.textContent='更新中…';
  fetch('/__mp/api/update-master',{method:'POST',credentials:'include'}).then(r=>{
    return r.json().then(data=>{
      if(!r.ok || data.ok===false) throw new Error(data.message||('HTTP '+r.status));
      return data;
    });
  }).then(data=>{
    showToast('主节点已更新至 '+(data.tag||'')+'，正在重启…');
    setTimeout(function(){ location.reload(); }, 2500);
  }).catch(e=>{
    alert('更新主节点 1pm 失败: '+(e&&e.message?e.message:e));
    btn.disabled=false; btn.textContent='更新主节点 1pm';
  });
}
function forceUpdate(){
  if(!confirm('将把所有在线子节点更新为当前主节点 1pm 二进制并重启服务。继续？')) return;
  const btn=document.getElementById('btnForceUpdate');
  btn.disabled=true; btn.textContent='更新中…';
  fetch('/__mp/api/force-update',{method:'POST',credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
  }).then(data=>{
    const total=data.total||0, ok=data.ok||0;
    const fails=(data.results||[]).filter(x=>!x.ok);
    if(fails.length){
      const msg=fails.map(x=>(x.name||x.id)+': '+(x.error||'fail')).join('\n');
      alert('完成 '+ok+'/'+total+'\n失败:\n'+msg);
    }else{
      showToast('已同步更新 '+ok+' 个节点');
    }
    setTimeout(refreshAgents, 2000);
  }).catch(e=>{
    alert('更新子节点 1pm 失败: '+(e&&e.message?e.message:e));
  }).finally(()=>{
    btn.disabled=false; btn.textContent='更新子节点 1pm';
  });
}
function upgradePanelMaster(){
  if(!confirm('将对本机 1Panel 触发官方升级（已最新则跳过；升级会重启面板）。继续？')) return;
  const btn=document.getElementById('btnUpgradePanelMaster');
  btn.disabled=true; btn.textContent='升级中…';
  fetch('/__mp/api/upgrade-panel-master',{method:'POST',credentials:'include'}).then(r=>{
    return r.json().then(data=>{
      if(!r.ok || data.ok===false) throw new Error(data.message||('HTTP '+r.status));
      return data;
    });
  }).then(data=>{
    if(data.skipped){
      showToast(data.message||'主节点 1Panel 已是最新');
    }else{
      showToast('主节点 1Panel 升级已开始 '+(data.target_version||'')+'，请稍后刷新');
    }
  }).catch(e=>{
    alert('更新主节点 1Panel 失败: '+(e&&e.message?e.message:e));
  }).finally(()=>{
    btn.disabled=false; btn.textContent='更新主节点 1Panel';
  });
}
function upgradePanel(){
  if(!confirm('将让所有在线子节点登录本机 1Panel 并触发官方升级（已最新则跳过）。继续？')) return;
  const btn=document.getElementById('btnUpgradePanel');
  btn.disabled=true; btn.textContent='升级中…';
  fetch('/__mp/api/upgrade-panel',{method:'POST',credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
  }).then(data=>{
    const total=data.total||0, ok=data.ok||0;
    const lines=(data.results||[]).map(x=>{
      if(!x.ok) return (x.name||x.id)+': '+(x.error||'fail');
      if(x.skipped) return (x.name||x.id)+': 已是最新';
      return (x.name||x.id)+': → '+(x.target_version||x.message||'ok');
    });
    if((data.results||[]).some(x=>!x.ok)){
      alert('完成 '+ok+'/'+total+'\n'+lines.join('\n'));
    }else{
      showToast('子节点 1Panel 升级完成 '+ok+'/'+total);
    }
    setTimeout(refreshAgents, 5000);
  }).catch(e=>{
    alert('更新子节点 1Panel 失败: '+(e&&e.message?e.message:e));
  }).finally(()=>{
    btn.disabled=false; btn.textContent='更新子节点 1Panel';
  });
}
function panelSSL(enable){
  const tip=enable
    ? '将开启主节点面板自签 SSL，并同步所有在线子节点走 wss（master_tls）。子节点本机面板不会开 SSL。继续？'
    : '将关闭主节点面板 SSL，并同步子节点改回 ws。继续？';
  if(!confirm(tip)) return;
  const btnOn=document.getElementById('btnSSLOn');
  const btnOff=document.getElementById('btnSSLOff');
  const btn=enable?btnOn:btnOff;
  btnOn.disabled=true; btnOff.disabled=true;
  btn.textContent='处理中…';
  fetch('/__mp/api/panel-ssl',{
    method:'POST',credentials:'include',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({enable:!!enable})
  }).then(r=>{
    return r.json().then(data=>{
      if(!r.ok){
        const m=(data.master_ssl&&data.master_ssl.error)||data.message||('HTTP '+r.status);
        throw new Error(m);
      }
      return data;
    });
  }).then(data=>{
    const fails=[];
    function collect(arr,tag){
      (arr||[]).forEach(function(x){
        if(!x.ok) fails.push(tag+' '+(x.name||x.id)+': '+(x.error||'fail'));
      });
    }
    if(data.master_ssl && !data.master_ssl.ok) fails.push('主节点 SSL: '+(data.master_ssl.error||'fail'));
    collect(data.master_tls,'master_tls');
    if(fails.length){
      alert((enable?'开启':'关闭')+'部分失败:\n'+fails.join('\n'));
    }else{
      showToast(enable?'主节点 SSL 已开启':'主节点 SSL 已关闭');
    }
    setTimeout(function(){ location.reload(); }, 2000);
  }).catch(e=>{
    alert('SSL 操作失败: '+(e&&e.message?e.message:e));
    btnOn.disabled=false; btnOff.disabled=false;
    btnOn.textContent='开启主节点 SSL';
    btnOff.textContent='关闭主节点 SSL';
  });
}
function displayName(a){
  return (a && (a.display_name||a.name||a.hostname||a.id)) || '-';
}
function groupLabel(a){
  if(a && a.is_master) return '主节点';
  const g=(a && a.group)||'';
  return g.trim()?g.trim():'未分组';
}
function renderAgents(list){
  const wrap=document.getElementById('agentsWrap');
  const online=document.getElementById('statOnline');
  const agentsOnly=(list||[]).filter(a=>!a.is_master);
  if(online) online.textContent=String(agentsOnly.length);
  if(!list.length){
    wrap.innerHTML='<p class="empty" id="agentsEmpty">暂无在线节点</p>';
    return;
  }
  const sorted=list.slice().sort((x,y)=>{
    if(x.is_master && !y.is_master) return -1;
    if(!x.is_master && y.is_master) return 1;
    const gx=groupLabel(x), gy=groupLabel(y);
    if(gx!==gy) return gx.localeCompare(gy,'zh');
    return displayName(x).localeCompare(displayName(y),'zh');
  });
  let rows='';
  let lastGroup=null;
  sorted.forEach(a=>{
    const g=groupLabel(a);
    if(g!==lastGroup){
      lastGroup=g;
      rows+='<tr class="group-row"><td colspan="8">'+esc(g)+'</td></tr>';
    }
    const ip=a.remote_ip||'-';
    const av=a.agent_version||'-';
    const pv=a.panel_version||'-';
    const mem=(a.mem_total>0)?(fmtBytes(a.mem_used)+' / '+fmtBytes(a.mem_total)):'-';
    const href=a.open_url || (a.is_master?'/__mp/local':('/__mp/go/'+encodeURIComponent(a.id)));
    rows+='<tr data-agent-id="'+esc(a.id||'')+'">' +
      '<td style="padding-left:18px"><span class="tag"><span class="dot"></span>在线</span></td>'+
      '<td>'+esc(displayName(a))+'</td>'+
      '<td class="muted">'+esc(ip)+'</td>'+
      '<td>'+esc(av)+'</td>'+
      '<td>'+esc(pv)+'</td>'+
      '<td class="col-cpu">'+fmtCPU(a.cpu_percent)+'</td>'+
      '<td class="col-mem">'+mem+'</td>'+
      '<td><a class="btn primary-panel" href="'+esc(href)+'">进入面板</a></td>'+
      '</tr>';
  });
  wrap.innerHTML='<table><thead><tr>'+
    '<th style="padding-left:18px">状态</th><th>名称</th><th>IP</th><th>Agent</th><th>1Panel</th><th>CPU</th><th>内存</th><th style="width:120px"></th>'+
    '</tr></thead><tbody id="agentsBody">'+rows+'</tbody></table>';
}
function esc(s){
  return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}
function refreshAgents(){
  return fetch('/__mp/api/agents',{credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
  }).then(list=>{
    renderAgents(Array.isArray(list)?list:[]);
  }).catch(()=>{});
}
fetchInstallCmd().catch(function(){});
refreshAgents();
setInterval(refreshAgents, 5000);
setInterval(function(){ fetchInstallCmd().catch(function(){}); }, 60000);
</script>
</body>
</html>`))

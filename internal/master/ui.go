package master

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/panel"
)

// handleMP 处理 /__mp/：鉴权后分发 API、节点切换与管理页。
func (s *Server) handleMP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/__mp")

	// 切回本机必须先于鉴权：此时浏览器里是远端 psession，
	// localPanelLoggedIn 会失败；本机会话在 Master 内存里，由 handleLocal 写回。
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

// apiAgents 拉取各 Agent 最新资源快照后返回在线列表。
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
		PanelURL     string  `json:"panel_url"`
		RemoteIP     string  `json:"remote_ip"`
		PanelVersion string  `json:"panel_version"`
		AgentVersion string  `json:"agent_version"`
		CPUPercent   float64 `json:"cpu_percent"`
		MemTotal     uint64  `json:"mem_total"`
		MemUsed      uint64  `json:"mem_used"`
		OpenURL      string  `json:"open_url"`
	}
	list := s.reg.List()
	out := make([]item, 0, len(list))
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
	agents := s.reg.List()
	data := pageData{
		Agents:        agents,
		Register:      s.InstallCommand(r),
		Host:          host,
		DeviceIP:      s.displayHost(r),
		Entrance:      s.Entrance,
		LocalPanel:    s.LocalPanel,
		MasterVersion: panel.ReadSystemVersion(),
		NodeVersion:   buildinfo.Version,
		Online:        len(agents),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = nodesTmpl.Execute(w, data)
}

// nodesTmpl 是 /__mp/ 管理页 HTML 模板。
var nodesTmpl = template.Must(template.New("nodes").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>多机节点 - 1Panel</title>
<style>
:root{
  --el-color-primary:#409EFF;
  --el-color-primary-dark:#337ecc;
  --panel-primary:#005eeb;
  --bg-page:#f2f3f5;
  --bg-card:#ffffff;
  --text-primary:#303133;
  --text-regular:#606266;
  --text-secondary:#909399;
  --border:#e4e7ed;
  --success:#67c23a;
  --shadow:0 1px 4px rgba(0,21,41,.08);
  --radius:6px;
}
*{box-sizing:border-box}
body{
  margin:0;
  min-height:100vh;
  font-family:Helvetica Neue,Helvetica,PingFang SC,Hiragino Sans GB,Microsoft YaHei,Arial,sans-serif;
  background:var(--bg-page);
  color:var(--text-primary);
}
.topbar{
  height:56px;
  background:#fff;
  border-bottom:1px solid var(--border);
  display:flex;align-items:center;justify-content:space-between;
  padding:0 20px;
  box-shadow:var(--shadow);
}
.brand{display:flex;align-items:center;gap:10px;font-weight:600;font-size:16px}
.brand svg{color:var(--panel-primary)}
.device{
  display:flex;align-items:center;gap:8px;
  color:var(--text-regular);font-size:13px;
  background:#f5f7fa;border:1px solid var(--border);
  border-radius:20px;padding:6px 12px;
}
.device strong{color:var(--panel-primary);font-weight:600}
.wrap{max-width:1080px;margin:0 auto;padding:20px}
.stats{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-bottom:16px}
.stat{
  background:var(--bg-card);border:1px solid var(--border);border-radius:var(--radius);
  padding:16px 18px;box-shadow:var(--shadow);
}
.stat .label{color:var(--text-secondary);font-size:13px;margin-bottom:8px}
.stat .value{font-size:22px;font-weight:600;color:var(--text-primary)}
.stat .value.ip{font-size:18px;color:var(--panel-primary);letter-spacing:.3px}
.card{
  background:var(--bg-card);border:1px solid var(--border);border-radius:var(--radius);
  box-shadow:var(--shadow);margin-bottom:16px;overflow:hidden;
}
.card-hd{
  padding:14px 18px;border-bottom:1px solid var(--border);
  display:flex;align-items:center;justify-content:space-between;gap:12px;
}
.card-hd h2{margin:0;font-size:15px;font-weight:600}
.card-bd{padding:18px}
.cmd{display:flex;gap:10px;align-items:stretch}
.cmd code{
  flex:1;background:#f5f7fa;border:1px solid var(--border);border-radius:var(--radius);
  padding:10px 12px;font-size:13px;color:var(--text-regular);overflow:auto;white-space:nowrap;
  font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;
}
.btn{
  border:1px solid transparent;border-radius:var(--radius);
  background:var(--el-color-primary);color:#fff;
  padding:8px 16px;font-size:13px;font-weight:500;cursor:pointer;
  text-decoration:none;display:inline-flex;align-items:center;justify-content:center;gap:6px;
  line-height:1.4;white-space:nowrap;
}
.btn:hover{background:var(--el-color-primary-dark)}
.btn.plain{
  background:#fff;color:var(--text-regular);border-color:var(--border);
}
.btn.plain:hover{color:var(--el-color-primary);border-color:var(--el-color-primary-light-5,#a0cfff);background:#ecf5ff}
.btn.primary-panel{background:var(--panel-primary)}
.btn.primary-panel:hover{background:#0052cc}
.btn:disabled{opacity:.6;cursor:not-allowed}
.meta{margin:10px 0 0;color:var(--text-secondary);font-size:12px}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:12px 10px;border-bottom:1px solid var(--border);font-size:13px;vertical-align:middle}
th{color:var(--text-secondary);font-weight:500;background:#fafafa}
tr:last-child td{border-bottom:0}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--success);margin-right:6px}
.tag{
  display:inline-flex;align-items:center;padding:2px 8px;border-radius:4px;
  background:#f0f9eb;color:#67c23a;font-size:12px;border:1px solid #e1f3d8;
}
.empty{color:var(--text-secondary);padding:24px 0;text-align:center;font-size:13px}
.actions{display:flex;flex-wrap:wrap;gap:10px}
.toast{
  position:fixed;right:20px;bottom:20px;background:#f0f9eb;color:#529b2e;
  border:1px solid #e1f3d8;padding:10px 14px;border-radius:var(--radius);
  opacity:0;transform:translateY(8px);transition:.2s;font-size:13px;z-index:99;
}
.toast.show{opacity:1;transform:translateY(0)}
.toast.err{background:#fef0f0;color:#f56c6c;border-color:#fde2e2}
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
    <span>多机节点</span>
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
      <div class="meta-row" style="display:flex;flex-wrap:wrap;gap:12px;margin-bottom:12px">
        <label style="display:flex;flex-direction:column;gap:4px;min-width:160px;flex:1">
          <span class="label" style="font-size:12px;color:var(--text-secondary)">节点名称</span>
          <input id="nodeName" type="text" maxlength="64" placeholder="如：机房A-web1" style="padding:8px 10px;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:inherit;font:inherit">
        </label>
        <label style="display:flex;flex-direction:column;gap:4px;min-width:160px;flex:1">
          <span class="label" style="font-size:12px;color:var(--text-secondary)">节点分组</span>
          <input id="nodeGroup" type="text" maxlength="64" placeholder="如：生产 / 测试" style="padding:8px 10px;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:inherit;font:inherit">
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
            <td style="color:var(--text-secondary)">{{if .RemoteIP}}{{.RemoteIP}}{{else}}-{{end}}</td>
            <td>{{if .AgentVersion}}{{.AgentVersion}}{{else}}-{{end}}</td>
            <td>{{if .PanelVersion}}{{.PanelVersion}}{{else}}-{{end}}</td>
            <td class="col-cpu">-</td>
            <td class="col-mem">-</td>
            <td><a class="btn primary-panel" href="/__mp/go/{{.ID}}">进入面板</a></td>
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
    <div class="card-hd"><h2>主节点</h2></div>
    <div class="card-bd">
      <p class="meta" style="margin:0 0 14px">
        1pm <strong id="nodeVer">{{if .NodeVersion}}{{.NodeVersion}}{{else}}-{{end}}</strong>
        · 1Panel <strong>{{if .MasterVersion}}{{.MasterVersion}}{{else}}-{{end}}</strong>
        · 上游 {{.LocalPanel}}
      </p>
      <div class="actions">
        <a class="btn primary-panel" href="/__mp/local">切换回主节点 1Panel</a>
        <button class="btn plain" type="button" id="btnForceUpdate" onclick="forceUpdate()">强制更新子节点</button>
      </div>
      <p class="meta" style="margin:14px 0 0">强制更新会让所有在线子节点从本机拉取最新 1pm 二进制（同安装时 /agent.bin）并重启服务。</p>
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
    alert('强制更新失败: '+(e&&e.message?e.message:e));
  }).finally(()=>{
    btn.disabled=false; btn.textContent='强制更新子节点';
  });
}
function displayName(a){
  return (a && (a.display_name||a.name||a.hostname||a.id)) || '-';
}
function groupLabel(a){
  const g=(a && a.group)||'';
  return g.trim()?g.trim():'未分组';
}
function renderAgents(list){
  const wrap=document.getElementById('agentsWrap');
  const online=document.getElementById('statOnline');
  if(online) online.textContent=String(list.length);
  if(!list.length){
    wrap.innerHTML='<p class="empty" id="agentsEmpty">暂无在线 Agent，请先在子节点执行上方注册命令</p>';
    return;
  }
  const sorted=list.slice().sort((x,y)=>{
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
      rows+='<tr class="group-row"><td colspan="8" style="padding:10px 18px;background:rgba(0,0,0,.03);font-weight:600;color:var(--text-regular)">'+esc(g)+'</td></tr>';
    }
    const ip=a.remote_ip||'-';
    const av=a.agent_version||'-';
    const pv=a.panel_version||'-';
    const mem=(a.mem_total>0)?(fmtBytes(a.mem_used)+' / '+fmtBytes(a.mem_total)):'-';
    rows+='<tr data-agent-id="'+esc(a.id)+'">'+
      '<td style="padding-left:18px"><span class="tag"><span class="dot"></span>在线</span></td>'+
      '<td>'+esc(displayName(a))+'</td>'+
      '<td style="color:var(--text-secondary)">'+esc(ip)+'</td>'+
      '<td>'+esc(av)+'</td>'+
      '<td>'+esc(pv)+'</td>'+
      '<td class="col-cpu">'+fmtCPU(a.cpu_percent)+'</td>'+
      '<td class="col-mem">'+mem+'</td>'+
      '<td><a class="btn primary-panel" href="/__mp/go/'+encodeURIComponent(a.id)+'">进入面板</a></td>'+
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

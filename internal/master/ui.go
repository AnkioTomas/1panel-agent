package master

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"1panel-agent/internal/panel"
)

// handleMP 处理 /__mp/：鉴权后分发 API、节点切换与管理页。
func (s *Server) handleMP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/__mp")

	// 1. 统一鉴权门禁：校验失败直接拦截（API 返 401，页面请求重定向）
	if !s.ensureMPAuth(w, r, path) {
		return
	}

	// 2. 特殊无感刷新接口
	if path == "/touch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 3. API 路由分发
	switch path {
	case "/api/agents":
		s.apiAgents(w, r)
		return
	case "/api/upgrade-check":
		s.handleUpgradeCheck(w, r)
		return
	case "/api/rotate-token":
		s.handleRotateToken(w, r)
		return
	}

	// 4. UI 页面路由分发
	if path == "" || path == "/" {
		s.renderNodes(w, r)
		return
	}
	switch {
	case path == "/local":
		s.handleLocal(w, r)
	case strings.HasPrefix(path, "/go/"):
		id := strings.TrimPrefix(path, "/go/")
		id = strings.Trim(id, "/")
		s.handleSwitch(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// apiAgents 返回在线 Agent JSON 列表（含 OpenURL）。
func (s *Server) apiAgents(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID           string `json:"id"`
		Hostname     string `json:"hostname"`
		PanelURL     string `json:"panel_url"`
		RemoteIP     string `json:"remote_ip"`
		PanelVersion string `json:"panel_version"`
		OpenURL      string `json:"open_url"`
	}
	list := s.reg.List()
	out := make([]item, 0, len(list))
	for _, a := range list {
		out = append(out, item{
			ID:           a.ID,
			Hostname:     a.Hostname,
			PanelURL:     a.PanelURL,
			RemoteIP:     a.RemoteIP,
			PanelVersion: a.PanelVersion,
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
	MasterVersion string
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
      <div class="value">{{.Online}}</div>
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
      <div class="cmd">
        <code id="reg">{{.Register}}</code>
        <button class="btn" type="button" onclick="copyReg()">复制命令</button>
      </div>
      <p class="meta">安装命令使用 HMAC 签名（约 5 分钟有效）；脚本内落盘 Master/Token，systemd 只跑 agent run。轮换 Token 后需重新安装。{{if .Entrance}}安全入口 {{.Entrance}}{{end}}</p>
    </div>
  </div>

  <div class="card">
    <div class="card-hd">
      <h2>在线节点</h2>
      <div style="display:flex;gap:8px">
        <button class="btn plain" type="button" id="btnCheckUpgrade" onclick="checkUpgrade()">检查更新</button>
        <button class="btn plain" type="button" onclick="location.reload()">刷新</button>
      </div>
    </div>
    <div class="card-bd" style="padding:0">
      {{if .Agents}}
      <table>
        <thead>
          <tr>
            <th style="padding-left:18px">状态</th>
            <th>主机名</th>
            <th>IP</th>
            <th>版本</th>
            <th>更新</th>
            <th>节点 ID</th>
            <th style="width:120px"></th>
          </tr>
        </thead>
        <tbody>
        {{range .Agents}}
          <tr data-agent-id="{{.ID}}">
            <td style="padding-left:18px"><span class="tag"><span class="dot"></span>在线</span></td>
            <td>{{.Hostname}}</td>
            <td style="color:var(--text-secondary)">{{if .RemoteIP}}{{.RemoteIP}}{{else}}-{{end}}</td>
            <td class="col-ver">{{if .PanelVersion}}{{.PanelVersion}}{{else}}-{{end}}</td>
            <td class="col-upd" style="color:var(--text-secondary)">-</td>
            <td><code style="font-size:12px;color:var(--text-regular)">{{.ID}}</code></td>
            <td><a class="btn primary-panel" href="/__mp/go/{{.ID}}">进入面板</a></td>
          </tr>
        {{end}}
        </tbody>
      </table>
      {{else}}
      <p class="empty">暂无在线 Agent，请先在子节点执行上方注册命令</p>
      {{end}}
      <p class="meta" id="upgradeMsg" style="padding:12px 18px;margin:0;display:none"></p>
    </div>
  </div>

  <div class="card">
    <div class="card-hd"><h2>主节点面板</h2></div>
    <div class="card-bd">
      <p class="meta" style="margin:0 0 14px">
        上游 {{.LocalPanel}}
        · 版本 <strong id="masterVer">{{if .MasterVersion}}{{.MasterVersion}}{{else}}-{{end}}</strong>
        · 更新 <strong id="masterUpd" style="font-weight:500;color:var(--text-secondary)">-</strong>
      </p>
      <p class="meta" style="margin:0 0 14px">切换远程节点不会覆盖主节点登录态。</p>
      <div class="actions">
        <a class="btn primary-panel" href="/__mp/local">切换回主节点 1Panel</a>
        {{if .Entrance}}<a class="btn plain" href="/{{.Entrance}}">打开安全入口</a>{{end}}
        <a class="btn plain" href="/__mp/">刷新本页</a>
      </div>
    </div>
  </div>
</div>

<div class="toast" id="toast">复制成功</div>
<script>
function copyReg(){
  const t=document.getElementById('reg').innerText;
  navigator.clipboard.writeText(t).then(()=>{
    const el=document.getElementById('toast');
    el.classList.add('show');
    setTimeout(()=>el.classList.remove('show'),1200);
  });
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
    const el=document.getElementById('toast');
    el.textContent='Token 已轮换';
    el.classList.add('show');
    setTimeout(()=>{el.classList.remove('show'); el.textContent='复制成功';},1500);
  }).catch(e=>{
    alert('轮换失败: '+e.message);
  }).finally(()=>{
    btn.disabled=false; btn.textContent='轮换 Token';
  });
}
function updText(status, latest){
  if(status==='outdated') return '有更新 '+(latest||'');
  if(status==='latest') return '已是最新';
  return '-';
}
function checkUpgrade(){
  const btn=document.getElementById('btnCheckUpgrade');
  const msg=document.getElementById('upgradeMsg');
  btn.disabled=true; btn.textContent='检查中…';
  fetch('/__mp/api/upgrade-check',{credentials:'include'}).then(r=>{
    if(!r.ok) throw new Error('HTTP '+r.status);
    return r.json();
  }).then(data=>{
    const mv=document.getElementById('masterVer');
    const mu=document.getElementById('masterUpd');
    if(data.master_version && mv) mv.textContent=data.master_version;
    if(mu){
      mu.textContent=updText(data.master_status, data.latest);
      mu.style.color=data.master_status==='outdated'?'#e6a23c':(data.master_status==='latest'?'#67c23a':'');
    }
    (data.agents||[]).forEach(a=>{
      const tr=document.querySelector('tr[data-agent-id="'+a.id+'"]');
      if(!tr) return;
      const ver=tr.querySelector('.col-ver');
      const upd=tr.querySelector('.col-upd');
      if(ver && a.version) ver.textContent=a.version;
      if(upd){
        upd.textContent=updText(a.status, a.latest||data.latest);
        upd.style.color=a.status==='outdated'?'#e6a23c':(a.status==='latest'?'#67c23a':'');
      }
    });
    if(msg){
      if(data.message){ msg.style.display='block'; msg.textContent='检查备注：'+data.message; }
      else if(data.latest){ msg.style.display='block'; msg.textContent='可用版本：'+data.latest; }
      else { msg.style.display='block'; msg.textContent='未发现可用新版本'; }
    }
  }).catch(e=>{
    if(msg){ msg.style.display='block'; msg.textContent='检查失败：'+e.message; }
  }).finally(()=>{
    btn.disabled=false; btn.textContent='检查更新';
  });
}
</script>
</body>
</html>`))

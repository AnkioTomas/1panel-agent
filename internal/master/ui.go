package master

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

func (s *Server) handleMP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/__mp")
	if path == "" || path == "/" {
		s.renderNodes(w, r)
		return
	}
	switch {
	case path == "/api/agents":
		s.apiAgents(w, r)
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

func (s *Server) apiAgents(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		PanelURL string `json:"panel_url"`
		OpenURL  string `json:"open_url"`
	}
	list := s.reg.List()
	out := make([]item, 0, len(list))
	for _, a := range list {
		out = append(out, item{
			ID:       a.ID,
			Hostname: a.Hostname,
			PanelURL: a.PanelURL,
			OpenURL:  "/__mp/go/" + a.ID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type pageData struct {
	Agents     []AgentInfo
	Register   string
	Token      string
	Host       string
	Entrance   string
	LocalPanel string
}

func (s *Server) renderNodes(w http.ResponseWriter, r *http.Request) {
	host := s.AdvertiseHost(r)
	regCmd := "1pm agent register " + host + "/" + s.Token
	data := pageData{
		Agents:     s.reg.List(),
		Register:   regCmd,
		Token:      s.Token,
		Host:       host,
		Entrance:   s.Entrance,
		LocalPanel: s.LocalPanel,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = nodesTmpl.Execute(w, data)
}

var nodesTmpl = template.Must(template.New("nodes").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>1Panel 多机节点</title>
<style>
:root{--bg:#0f1419;--card:#1a2332;--line:#2d3a4d;--text:#e7ecf3;--muted:#8b9bb4;--accent:#3b82f6;--ok:#22c55e}
*{box-sizing:border-box}
body{margin:0;font-family:ui-sans-serif,system-ui,-apple-system,sans-serif;background:linear-gradient(160deg,#0f1419,#152033 50%,#0f1419);color:var(--text);min-height:100vh}
.wrap{max-width:920px;margin:0 auto;padding:2rem 1.25rem 3rem}
h1{font-size:1.5rem;margin:0 0 .35rem}
.sub{color:var(--muted);margin:0 0 1.5rem;font-size:.95rem}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:1rem 1.1rem;margin:0 0 1rem}
.card h2{font-size:1rem;margin:0 0 .75rem}
.cmd{display:flex;gap:.5rem;align-items:stretch}
.cmd code{flex:1;background:#0b1020;border:1px solid var(--line);border-radius:8px;padding:.75rem .9rem;font-size:.85rem;overflow:auto;white-space:nowrap}
button,.btn{border:0;border-radius:8px;background:var(--accent);color:#fff;padding:.7rem 1rem;font-weight:600;cursor:pointer;text-decoration:none;display:inline-flex;align-items:center}
button.secondary{background:#334155}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:.7rem .4rem;border-bottom:1px solid var(--line);font-size:.92rem}
th{color:var(--muted);font-weight:600}
.dot{display:inline-block;width:.55rem;height:.55rem;border-radius:50%;background:var(--ok);margin-right:.4rem}
.empty{color:var(--muted);padding:.5rem 0}
.toast{position:fixed;right:1rem;bottom:1rem;background:#14532d;color:#dcfce7;padding:.6rem .9rem;border-radius:8px;opacity:0;transition:.2s}
.toast.show{opacity:1}
a.link{color:#93c5fd}
</style>
</head>
<body>
<div class="wrap">
  <h1>1Panel 多机节点</h1>
  <p class="sub">Master 网关已接管面板端口；本地面板反代中。Agent 经 WebSocket 接入后可一键切换并预登录。</p>

  <div class="card">
    <h2>子节点注册命令</h2>
    <div class="cmd">
      <code id="reg">{{.Register}}</code>
      <button type="button" onclick="copyReg()">复制</button>
    </div>
    <p class="sub" style="margin:.75rem 0 0">Master: {{.Host}} · Token 已嵌入命令{{if .Entrance}} · 安全入口: {{.Entrance}}{{end}}</p>
  </div>

  <div class="card">
    <h2>在线 Agent <button class="secondary" type="button" onclick="location.reload()">刷新</button></h2>
    {{if .Agents}}
    <table>
      <thead><tr><th>状态</th><th>主机</th><th>ID</th><th>本机面板</th><th></th></tr></thead>
      <tbody>
      {{range .Agents}}
        <tr>
          <td><span class="dot"></span>在线</td>
          <td>{{.Hostname}}</td>
          <td><code>{{.ID}}</code></td>
          <td class="sub">{{.PanelURL}}</td>
          <td><a class="btn" href="/__mp/go/{{.ID}}">进入面板</a></td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{else}}
      <p class="empty">暂无在线 Agent。在子机执行上方注册命令。</p>
    {{end}}
  </div>

  <div class="card">
    <h2>本机面板</h2>
    <p class="sub">上游: {{.LocalPanel}} · 切换远程节点后点此可回到本机</p>
    <a class="btn" href="/__mp/local">切换回本机 1Panel</a>
    {{if .Entrance}}
    <a class="btn secondary" style="margin-left:.5rem" href="/{{.Entrance}}">安全入口 /{{.Entrance}}</a>
    {{end}}
    <a class="btn secondary" style="margin-left:.5rem" href="/__mp/">节点管理</a>
  </div>
</div>
<div class="toast" id="toast">已复制</div>
<script>
function copyReg(){
  const t=document.getElementById('reg').innerText;
  navigator.clipboard.writeText(t).then(()=>{
    const el=document.getElementById('toast');
    el.classList.add('show');
    setTimeout(()=>el.classList.remove('show'),1200);
  });
}
</script>
</body>
</html>`))

package master

import "html/template"

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>1Panel Nodes</title>
<style>
body{font-family:system-ui,sans-serif;margin:2rem;background:#f6f7f9;color:#222}
h1{font-size:1.25rem;margin:0 0 1rem}
ul{list-style:none;padding:0;margin:0;max-width:40rem}
li{background:#fff;border:1px solid #dde1e6;margin:0 0 .5rem;padding:.75rem 1rem;display:flex;justify-content:space-between;align-items:center}
a{color:#0b57d0;text-decoration:none;font-weight:600}
.meta{color:#667;font-size:.875rem}
.empty{color:#667}
</style>
</head>
<body>
<h1>在线 1Panel 节点</h1>
{{if .}}
<ul>
{{range .}}
<li>
  <div>
    <div><a href="/n/{{.ID}}/">{{.Hostname}}</a></div>
    <div class="meta">id={{.ID}} · {{.PanelURL}}</div>
  </div>
  <a href="/n/{{.ID}}/">打开</a>
</li>
{{end}}
</ul>
{{else}}
<p class="empty">暂无在线 Agent。使用 <code>agent register host:port/token</code> 接入。</p>
{{end}}
</body>
</html>`))

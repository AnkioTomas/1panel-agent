package master

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// clientHostKey 在反代 Director 中保存浏览器原始 Host，供 ModifyResponse 注入使用。
type clientHostKey struct{}

// sidebarHook 生成注入 1Panel HTML 的侧边栏切换脚本（含 Master IP 展示）。
func sidebarHook(masterIP, entrance string) string {
	if masterIP == "" {
		masterIP = "-"
	}
	ipJSON, _ := json.Marshal(masterIP)
	entJSON, _ := json.Marshal(strings.Trim(entrance, "/"))
	const tpl = `<script data-mp-hook="1">(function(){
// Always Master advertise IP (public_host). Never the current Agent IP.
var MASTER_IP=__MASTER_IP__;
var ENTRANCE=__ENTRANCE__;
var PATCHED=false;
var LABEL_TIMER=0;

function cookie(name){
  var m=document.cookie.match(new RegExp("(?:^|; )"+name+"=([^;]*)"));
  return m?decodeURIComponent(m[1]):"";
}
function currentID(){ return cookie("mp_node")||""; }

function clearMpNode(){
  document.cookie="mp_node=; Path=/; Max-Age=0; SameSite=Lax";
}
function masterLoginURL(){
  return ENTRANCE ? ("/"+ENTRANCE) : "/";
}
function goMasterLogin(){
  clearMpNode();
  location.replace(masterLoginURL());
}
function isLoginRoute(){
  var p=(location.pathname||"").toLowerCase();
  var h=(location.hash||"").toLowerCase();
  if(p.indexOf("/api/")===0) return false;
  if(p==="/login" || p.indexOf("/login/")===0 || /\/login$/.test(p)) return true;
  if(h.indexOf("login")>=0) return true;
  return false;
}
function enforceMasterLogin(){
  if(currentID() && isLoginRoute()) goMasterLogin();
}

function ensureStyle(){
  if(document.getElementById("mp-node-style"))return;
  var st=document.createElement("style");
  st.id="mp-node-style";
  st.textContent=
    "#mp-node-switch{display:flex;align-items:center;gap:10px;width:100%;padding:10px 14px;box-sizing:border-box;cursor:pointer;user-select:none;color:var(--el-text-color-primary,inherit);border:0;background:transparent;text-align:left;font:inherit;border-radius:4px;transition:background .15s;}"+
    "#mp-node-switch:hover{background:var(--el-fill-color-light,rgba(0,0,0,.04));}"+
    "#mp-node-switch .mp-ns-avatar{flex:0 0 28px;width:28px;height:28px;border-radius:8px;display:flex;align-items:center;justify-content:center;background:var(--el-color-primary-light-9,#e6f0fd);color:var(--el-color-primary,#005eeb);}"+
    "#mp-node-switch .mp-ns-avatar svg{display:block;}"+
    "#mp-node-switch .mp-ns-text{flex:1;min-width:0;display:flex;flex-direction:column;gap:2px;line-height:1.25;}"+
    "#mp-node-switch .mp-ns-title{font-size:14px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
    "#mp-node-switch .mp-ns-ip{font-size:12px;color:var(--el-text-color-secondary,#909399);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
    "#mp-node-switch .mp-ns-caret{flex:0 0 14px;color:var(--el-text-color-secondary,#909399);display:flex;align-items:center;justify-content:center;transition:transform .15s;}"+
    "#mp-node-switch.is-open .mp-ns-caret{transform:rotate(180deg);}"+
    ".mp-ns-native-hide{display:none!important;}"+
    "#mp-node-pop{position:fixed;z-index:4000;min-width:220px;max-width:300px;max-height:min(420px,70vh);overflow:auto;background:var(--el-bg-color-overlay,var(--el-bg-color,#fff));color:var(--el-text-color-primary,#303133);border:1px solid var(--el-border-color-light,#e4e7ed);border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.12),0 2px 6px rgba(0,0,0,.06);padding:6px;}"+
    "#mp-node-pop .mp-ns-hd{padding:8px 10px 6px;font-size:12px;font-weight:600;color:var(--el-text-color-secondary,#909399);letter-spacing:.02em;}"+
    "#mp-node-pop .mp-ns-item{display:flex;align-items:center;gap:10px;padding:8px 10px;cursor:pointer;font-size:13px;line-height:1.3;color:var(--el-text-color-regular,inherit);border-radius:6px;transition:background .12s,color .12s;}"+
    "#mp-node-pop .mp-ns-item:hover{background:var(--el-fill-color-light,rgba(0,0,0,.04));}"+
    "#mp-node-pop .mp-ns-item.is-active{color:var(--el-color-primary,#005eeb);background:var(--el-color-primary-light-9,#e6f0fd);}"+
    "#mp-node-pop .mp-ns-item.is-danger{color:var(--el-color-danger,#f56c6c);}"+
    "#mp-node-pop .mp-ns-item.is-danger:hover{background:var(--el-color-danger-light-9,#fef0f0);}"+
    "#mp-node-pop .mp-ns-ico{flex:0 0 28px;width:28px;height:28px;border-radius:8px;display:flex;align-items:center;justify-content:center;background:var(--el-fill-color-light,#f5f7fa);color:var(--el-text-color-secondary,#909399);}"+
    "#mp-node-pop .mp-ns-item.is-active .mp-ns-ico{background:var(--el-color-primary,#005eeb);color:#fff;}"+
    "#mp-node-pop .mp-ns-item.is-master .mp-ns-ico{background:var(--el-color-primary-light-9,#e6f0fd);color:var(--el-color-primary,#005eeb);}"+
    "#mp-node-pop .mp-ns-item.is-master.is-active .mp-ns-ico{background:var(--el-color-primary,#005eeb);color:#fff;}"+
    "#mp-node-pop .mp-ns-body{flex:1;min-width:0;display:flex;flex-direction:column;gap:2px;}"+
    "#mp-node-pop .mp-ns-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:500;}"+
    "#mp-node-pop .mp-ns-sub{font-size:12px;color:var(--el-text-color-secondary,#909399);font-weight:400;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
    "#mp-node-pop .mp-ns-check{flex:0 0 16px;width:16px;height:16px;opacity:0;color:var(--el-color-primary,#005eeb);}"+
    "#mp-node-pop .mp-ns-item.is-active .mp-ns-check{opacity:1;}"+
    "#mp-node-pop .mp-ns-badge{display:inline-flex;align-items:center;margin-left:6px;padding:0 5px;height:16px;border-radius:4px;font-size:10px;font-weight:600;line-height:16px;background:var(--el-color-primary-light-8,#cce0fb);color:var(--el-color-primary,#005eeb);vertical-align:middle;}"+
    "#mp-node-pop .mp-ns-item.is-active .mp-ns-badge{background:var(--el-color-primary,#005eeb);color:#fff;}"+
    "#mp-node-pop .mp-ns-sep{height:1px;margin:6px 4px;background:var(--el-border-color-lighter,#ebeef5);}"+
    "#mp-node-pop .mp-ns-group{padding:8px 10px 4px;font-size:11px;color:var(--el-text-color-secondary,#909399);font-weight:600;letter-spacing:.02em;}"+
    "#mp-node-pop .mp-ns-empty{padding:12px 10px;font-size:12px;color:var(--el-text-color-secondary,#909399);text-align:center;}"+
    "html.dark #mp-node-pop .mp-ns-item.is-danger:hover{background:rgba(226,50,79,.12);}"+
    "html.dark #mp-node-pop{box-shadow:0 8px 24px rgba(0,0,0,.45);}";
  document.head.appendChild(st);
}

function svgIco(kind){
  if(kind==="master") return '<svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M4 5h16a1 1 0 0 1 1 1v4H3V6a1 1 0 0 1 1-1zm-1 7h18v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6zm3 2v2h2v-2H6zm4 0v2h2v-2h-2z"/></svg>';
  if(kind==="node") return '<svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M12 2a3 3 0 0 1 3 3v1h3a2 2 0 0 1 2 2v3h-1a2 2 0 1 0 0 4h1v3a2 2 0 0 1-2 2h-3v1a3 3 0 1 1-6 0v-1H6a2 2 0 0 1-2-2v-3h1a2 2 0 1 0 0-4H4V8a2 2 0 0 1 2-2h3V5a3 3 0 0 1 3-3z"/></svg>';
  if(kind==="manage") return '<svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M19.14 12.94a7.5 7.5 0 0 0 .05-.94 7.5 7.5 0 0 0-.05-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.6-.22l-2.39.96a7.2 7.2 0 0 0-1.63-.94l-.36-2.54A.5.5 0 0 0 13.9 1h-3.8a.5.5 0 0 0-.49.42l-.36 2.54c-.58.23-1.12.54-1.63.94l-2.39-.96a.5.5 0 0 0-.6.22L2.71 7.48a.5.5 0 0 0 .12.64L4.86 9.7A7.5 7.5 0 0 0 4.8 12c0 .32.02.63.05.94l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32a.5.5 0 0 0 .6.22l2.39-.96c.5.4 1.05.71 1.63.94l.36 2.54a.5.5 0 0 0 .49.42h3.8a.5.5 0 0 0 .49-.42l.36-2.54c.58-.23 1.12-.54 1.63-.94l2.39.96a.5.5 0 0 0 .6-.22l1.92-3.32a.5.5 0 0 0-.12-.64l-2.02-1.58zM12 15.5A3.5 3.5 0 1 1 12 8.5a3.5 3.5 0 0 1 0 7z"/></svg>';
  if(kind==="logout") return '<svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M10 4a1 1 0 0 1 1 1v6h6.59l-1.3-1.3a1 1 0 1 1 1.42-1.4l3 3a1 1 0 0 1 0 1.4l-3 3a1 1 0 1 1-1.42-1.4L17.59 13H11v6a1 1 0 0 1-1 1H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5z"/></svg>';
  return '<svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="M9.7 16.3a1 1 0 0 1-1.4 0l-3.3-3.3a1 1 0 1 1 1.4-1.4L9 14.17l7.3-7.3a1 1 0 0 1 1.4 1.42l-8 8z"/></svg>';
}
function menuItem(attrs, ico, name, sub, active, extraClass){
  var cls="mp-ns-item"+(extraClass?(" "+extraClass):"")+(active?" is-active":"");
  var html='<div class="'+cls+'" '+attrs+'>';
  html+='<span class="mp-ns-ico">'+svgIco(ico)+'</span>';
  html+='<span class="mp-ns-body"><span class="mp-ns-name">'+name+"</span>";
  if(sub) html+='<span class="mp-ns-sub">'+sub+"</span>";
  html+="</span>";
  html+='<span class="mp-ns-check">'+svgIco("check")+"</span>";
  html+="</div>";
  return html;
}

function findNativeTrigger(){
  var side=document.querySelector(".sidebar-container");
  if(!side)return null;
  var links=side.querySelectorAll(".el-dropdown-link");
  for(var i=0;i<links.length;i++){
    if(links[i].querySelector(".ellipsis-text")) return links[i];
  }
  var ell=side.querySelector(".ellipsis-text");
  return ell?ell.closest("div"):null;
}

function findCollapseRoot(trigger){
  if(!trigger)return null;
  var side=document.querySelector(".sidebar-container");
  var n=trigger;
  while(n && n!==side){
    if(n.parentNode===side) return n;
    n=n.parentNode;
  }
  return trigger.parentNode;
}

function agentTitle(a){
  return (a && (a.display_name||a.name||a.hostname||a.id)) || "-";
}
function agentGroup(a){
  var g=(a && a.group)||"";
  return g ? g : "未分组";
}
function labelParts(agents){
  var id=currentID();
  if(!id) return {title:"主节点", ip:(MASTER_IP && MASTER_IP!=="-") ? MASTER_IP : ""};
  for(var i=0;i<(agents||[]).length;i++){
    if(agents[i].id===id){
      var a=agents[i];
      return {title:agentTitle(a), ip:(a.remote_ip||"")};
    }
  }
  return {title:id.slice(0,8), ip:""};
}

function closePop(){
  var p=document.getElementById("mp-node-pop");
  if(p) p.remove();
  var btn=document.getElementById("mp-node-switch");
  if(btn) btn.classList.remove("is-open");
  document.removeEventListener("click", onDocClick, true);
}

function onDocClick(e){
  var p=document.getElementById("mp-node-pop");
  var btn=document.getElementById("mp-node-switch");
  if(!p)return;
  if(p.contains(e.target) || (btn && btn.contains(e.target))) return;
  closePop();
}

function go(url){
  closePop();
  location.href=url;
}

function renderPop(btn, agents){
  closePop();
  btn.classList.add("is-open");
  var pop=document.createElement("div");
  pop.id="mp-node-pop";
  var id=currentID();
  var html="";
  html+='<div class="mp-ns-hd">切换节点</div>';
  var masterName='主节点<span class="mp-ns-badge">主</span>';
  var masterSub=(MASTER_IP&&MASTER_IP!=="-")?MASTER_IP:"";
  html+=menuItem('data-mp="local"', "master", masterName, masterSub, !id, "is-master");
  if(!agents || !agents.length){
    html+='<div class="mp-ns-empty">暂无在线 Agent</div>';
  } else {
    var sorted=agents.filter(function(a){ return !a.is_master; }).slice().sort(function(x,y){
      var gx=agentGroup(x), gy=agentGroup(y);
      if(gx!==gy) return gx<gy?-1:gx>gy?1:0;
      var tx=agentTitle(x), ty=agentTitle(y);
      return tx<ty?-1:tx>ty?1:0;
    });
    var last="";
    for(var i=0;i<sorted.length;i++){
      var a=sorted[i];
      var g=agentGroup(a);
      if(g!==last){
        last=g;
        html+='<div class="mp-ns-group">'+g+"</div>";
      }
      html+=menuItem('data-mp="go" data-id="'+a.id+'"', "node", agentTitle(a), a.remote_ip||"", id===a.id, "");
    }
  }
  html+='<div class="mp-ns-sep"></div>';
  html+=menuItem('data-mp="manage"', "manage", "节点管理", "安装 / 更新 / SSL", false, "");
  html+=menuItem('data-mp="logout"', "logout", "退出登录", "", false, "is-danger");
  pop.innerHTML=html;
  document.body.appendChild(pop);
  var rect=btn.getBoundingClientRect();
  pop.style.left=Math.max(8, rect.left)+"px";
  requestAnimationFrame(function(){
    var h=pop.offsetHeight||240;
    var y=rect.top - h - 6;
    if(y < 8) y=rect.bottom + 6;
    pop.style.top=y+"px";
  });
  pop.addEventListener("click", function(e){
    var el=e.target.closest("[data-mp]");
    if(!el)return;
    e.preventDefault();
    e.stopPropagation();
    var kind=el.getAttribute("data-mp");
    if(kind==="local") go("/__mp/local");
    else if(kind==="manage") go("/__mp/");
    else if(kind==="go") go("/__mp/go/"+el.getAttribute("data-id"));
    else if(kind==="logout"){
      closePop();
      fetch("/api/v2/core/auth/logout",{method:"POST",credentials:"include"}).finally(function(){ goMasterLogin(); });
    }
  });
  setTimeout(function(){ document.addEventListener("click", onDocClick, true); }, 0);
}

function loadAgents(cb){
  fetch("/__mp/api/agents",{credentials:"include"}).then(function(r){
    if(!r.ok) throw new Error("agents "+r.status);
    return r.json();
  }).then(function(list){ cb(Array.isArray(list)?list:[]); })
  .catch(function(){ cb([]); });
}

function bindSwitch(btn){
  function refreshLabel(agents){
    var p=labelParts(agents);
    var title=btn.querySelector(".mp-ns-title");
    var ip=btn.querySelector(".mp-ns-ip");
    if(title) title.textContent=p.title||"主节点";
    if(ip){
      if(p.ip){ ip.textContent=p.ip; ip.style.display=""; }
      else { ip.textContent=""; ip.style.display="none"; }
    }
  }
  refreshLabel([]);
  loadAgents(refreshLabel);
  if(btn.getAttribute("data-mp-bound")==="1") return;
  btn.setAttribute("data-mp-bound","1");
  btn.addEventListener("click", function(e){
    e.preventDefault();
    e.stopPropagation();
    if(document.getElementById("mp-node-pop")){ closePop(); return; }
    loadAgents(function(agents){
      refreshLabel(agents);
      renderPop(btn, agents);
    });
  });
}

function mountSwitch(){
  var side=document.querySelector(".sidebar-container");
  if(!side) return false;

  var trigger=findNativeTrigger();
  var host=findCollapseRoot(trigger);
  if(!host || !host.parentNode) return false;

  ensureStyle();
  host.classList.add("mp-ns-native-hide");

  var btn=document.getElementById("mp-node-switch");
  if(btn && !side.contains(btn)){
    btn.remove();
    btn=null;
  }
  if(btn){
    // Keep button parked immediately before native host.
    if(btn.nextSibling!==host) host.parentNode.insertBefore(btn, host);
    bindSwitch(btn);
    return true;
  }

  btn=document.createElement("button");
  btn.type="button";
  btn.id="mp-node-switch";
  btn.title="切换节点";
  btn.innerHTML=
    '<span class="mp-ns-avatar">'+
      '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">'+
        '<path fill="currentColor" d="M4 5h16a1 1 0 0 1 1 1v4H3V6a1 1 0 0 1 1-1zm-1 7h18v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6zm3 2v2h2v-2H6zm4 0v2h2v-2h-2z"/>'+
      '</svg>'+
    '</span>'+
    '<span class="mp-ns-text">'+
      '<span class="mp-ns-title">主节点</span>'+
      '<span class="mp-ns-ip"></span>'+
    '</span>'+
    '<span class="mp-ns-caret"><svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="M7.4 9.3a1 1 0 0 1 1.4 0L12 12.5l3.2-3.2a1 1 0 1 1 1.4 1.4l-3.9 3.9a1 1 0 0 1-1.4 0L7.4 10.7a1 1 0 0 1 0-1.4z"/></svg></span>';
  host.parentNode.insertBefore(btn, host);
  bindSwitch(btn);
  return true;
}

function tryMount(attempt){
  if(mountSwitch()){ PATCHED=true; return; }
  if(attempt>=40) return; // ~12s then give up — never spin forever
  LABEL_TIMER=setTimeout(function(){ tryMount(attempt+1); }, 300);
}

// One-shot mount retries. Do not observe sidebar mutations (that froze the page).
tryMount(0);
// Vue may recreate sidebar later; lightly re-hide native host / remount if missing.
setInterval(function(){
  var trigger=findNativeTrigger();
  if(!trigger)return;
  var host=findCollapseRoot(trigger);
  if(host) host.classList.add("mp-ns-native-hide");
  if(!document.getElementById("mp-node-switch")) mountSwitch();
}, 2000);

try{
  fetch("/__mp/touch",{credentials:"include"}).catch(function(){});
  enforceMasterLogin();
  window.addEventListener("hashchange", enforceMasterLogin);
  window.addEventListener("popstate", enforceMasterLogin);
  setInterval(enforceMasterLogin, 1000);
  var u=new URL(location.href);
  var ret=u.searchParams.get("mp_return")||sessionStorage.getItem("mp_return");
  if(u.searchParams.get("mp_return")) sessionStorage.setItem("mp_return",ret);
  if(ret && ret.indexOf("/__mp")===0){
    fetch("/api/v2/dashboard/base/os",{credentials:"include"}).then(function(r){return r.json()}).then(function(j){
      if(j && j.code===200){ sessionStorage.removeItem("mp_return"); location.replace(ret); }
    }).catch(function(){});
  }
}catch(err){}
})();</script>`
	out := strings.ReplaceAll(tpl, "__MASTER_IP__", string(ipJSON))
	return strings.ReplaceAll(out, "__ENTRANCE__", string(entJSON))
}

// injectHookHTML 向 HTML 响应注入侧边栏 Hook；已注入则原样返回。
func (s *Server) injectHookHTML(body []byte, masterIP string) []byte {
	if bytes.Contains(body, []byte(`data-mp-hook="1"`)) {
		return body
	}
	hook := []byte(sidebarHook(masterIP, s.Entrance))
	if i := bytes.LastIndex(bytes.ToLower(body), []byte("</body>")); i >= 0 {
		out := append([]byte{}, body[:i]...)
		out = append(out, hook...)
		out = append(out, body[i:]...)
		return out
	}
	return append(body, hook...)
}

// dropHopHeaders 删除重写 body 后失效的长度/编码类响应头。
func dropHopHeaders(h map[string][]string) {
	for _, k := range []string{"Content-Length", "Transfer-Encoding", "Content-Encoding"} {
		delete(h, k)
		for key := range h {
			if strings.EqualFold(key, k) {
				delete(h, key)
			}
		}
	}
}

// maybeGunzip 在 Content-Encoding 为 gzip 时解压 body，否则原样返回。
func maybeGunzip(body []byte, headers map[string][]string) []byte {
	ce := ""
	for k, vals := range headers {
		if strings.EqualFold(k, "Content-Encoding") && len(vals) > 0 {
			ce = vals[0]
			break
		}
	}
	if !strings.EqualFold(ce, "gzip") {
		return body
	}
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		return body
	}
	return out
}

// wrapLocalProxy 配置本机反代：禁用压缩并在 HTML 响应中注入 Hook。
func (s *Server) wrapLocalProxy() {
	if s.localProxy == nil {
		return
	}
	upstream := s.localProxy.Director
	s.localProxy.Director = func(r *http.Request) {
		*r = *r.WithContext(context.WithValue(r.Context(), clientHostKey{}, r.Host))
		upstream(r)
		r.Header.Del("Accept-Encoding")
	}
	s.localProxy.ModifyResponse = func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			return nil
		}
		var body []byte
		var err error
		ce := resp.Header.Get("Content-Encoding")
		switch ce {
		case "gzip":
			gr, gerr := gzip.NewReader(resp.Body)
			if gerr != nil {
				return nil
			}
			body, err = io.ReadAll(gr)
			_ = gr.Close()
			_ = resp.Body.Close()
			if err != nil {
				return nil
			}
			resp.Header.Del("Content-Encoding")
		default:
			body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return nil
			}
		}
		var req *http.Request
		if resp.Request != nil {
			if h, ok := resp.Request.Context().Value(clientHostKey{}).(string); ok && h != "" {
				req = &http.Request{Host: h}
			}
		}
		patched := s.injectHookHTML(body, s.displayHost(req))
		resp.Body = io.NopCloser(bytes.NewReader(patched))
		resp.ContentLength = int64(len(patched))
		resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
		resp.Header.Del("Transfer-Encoding")
		return nil
	}
}

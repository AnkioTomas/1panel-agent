package master

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func sidebarHook(masterIP string) string {
	if masterIP == "" {
		masterIP = "-"
	}
	ipJSON, _ := json.Marshal(masterIP)
	const tpl = `<script data-mp-hook="1">(function(){
// Always Master advertise IP (public_host). Never the current Agent IP.
var MASTER_IP=__MASTER_IP__;
var PATCHED=false;
var LABEL_TIMER=0;

function cookie(name){
  var m=document.cookie.match(new RegExp("(?:^|; )"+name+"=([^;]*)"));
  return m?decodeURIComponent(m[1]):"";
}
function currentID(){ return cookie("mp_node")||""; }

function ensureStyle(){
  if(document.getElementById("mp-node-style"))return;
  var st=document.createElement("style");
  st.id="mp-node-style";
  st.textContent=
    "#mp-node-switch{display:flex;align-items:center;gap:8px;width:100%;padding:8px 14px;box-sizing:border-box;cursor:pointer;user-select:none;color:inherit;border:0;background:transparent;text-align:left;font:inherit;}"+
    "#mp-node-switch:hover{background:rgba(0,94,235,.06);}"+
    "#mp-node-switch .mp-ns-icon{flex:0 0 18px;opacity:.9;}"+
    "#mp-node-switch .mp-ns-text{flex:1;min-width:0;display:flex;flex-direction:column;gap:2px;line-height:1.25;}"+
    "#mp-node-switch .mp-ns-title{font-size:14px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
    "#mp-node-switch .mp-ns-ip{font-size:12px;opacity:.65;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
    "#mp-node-switch .mp-ns-caret{flex:0 0 12px;opacity:.55;font-size:12px;}"+
    ".mp-ns-native-hide{display:none!important;}"+
    "#mp-node-pop{position:fixed;z-index:4000;min-width:200px;max-width:280px;max-height:360px;overflow:auto;background:var(--el-bg-color, #fff);color:var(--el-text-color-primary,#303133);border:1px solid var(--el-border-color-light,#e4e7ed);border-radius:6px;box-shadow:0 6px 16px rgba(0,0,0,.12);padding:6px 0;}"+
    "#mp-node-pop .mp-ns-item{display:flex;flex-direction:column;align-items:flex-start;gap:2px;padding:8px 14px;cursor:pointer;font-size:13px;line-height:1.25;}"+
    "#mp-node-pop .mp-ns-item:hover{background:rgba(0,94,235,.08);}"+
    "#mp-node-pop .mp-ns-item.is-active{color:var(--el-color-primary,#409EFF);font-weight:600;}"+
    "#mp-node-pop .mp-ns-item .mp-ns-sub{font-size:12px;opacity:.55;font-weight:400;}"+
    "#mp-node-pop .mp-ns-sep{height:1px;margin:6px 0;background:var(--el-border-color-lighter,#ebeef5);}"+
    "#mp-node-pop .mp-ns-empty{padding:10px 14px;font-size:12px;opacity:.6;}";
  document.head.appendChild(st);
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

function labelParts(agents){
  var id=currentID();
  if(!id) return {title:"主节点", ip:(MASTER_IP && MASTER_IP!=="-") ? MASTER_IP : ""};
  for(var i=0;i<(agents||[]).length;i++){
    if(agents[i].id===id){
      var a=agents[i];
      return {title:(a.hostname||a.id), ip:(a.remote_ip||"")};
    }
  }
  return {title:id.slice(0,8), ip:""};
}

function closePop(){
  var p=document.getElementById("mp-node-pop");
  if(p) p.remove();
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
  var pop=document.createElement("div");
  pop.id="mp-node-pop";
  var id=currentID();
  var html="";
  html+='<div class="mp-ns-item'+(id?"":" is-active")+'" data-mp="local"><span>主节点</span>'+(MASTER_IP&&MASTER_IP!=="-"?'<span class="mp-ns-sub">'+MASTER_IP+"</span>":"")+"</div>";
  if(!agents || !agents.length){
    html+='<div class="mp-ns-empty">暂无在线 Agent</div>';
  } else {
    for(var i=0;i<agents.length;i++){
      var a=agents[i];
      var active=id===a.id?" is-active":"";
      var title=(a.hostname||a.id);
      var sub=a.remote_ip||"";
      html+='<div class="mp-ns-item'+active+'" data-mp="go" data-id="'+a.id+'"><span>'+title+"</span>"+(sub?'<span class="mp-ns-sub">'+sub+"</span>":"")+"</div>";
    }
  }
  html+='<div class="mp-ns-sep"></div>';
  html+='<div class="mp-ns-item" data-mp="manage"><span>管理节点…</span></div>';
  html+='<div class="mp-ns-item" data-mp="logout"><span>退出登录</span></div>';
  pop.innerHTML=html;
  document.body.appendChild(pop);
  var rect=btn.getBoundingClientRect();
  var top=rect.top - 8;
  pop.style.left=Math.max(8, rect.left)+"px";
  // Prefer opening upward above the button (sidebar footer).
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
      fetch("/api/v2/core/auth/logout",{method:"POST",credentials:"include"}).finally(function(){ location.href="/"; });
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
    '<svg class="mp-ns-icon" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">'+
      '<path fill="currentColor" d="M4 5h16a1 1 0 0 1 1 1v4H3V6a1 1 0 0 1 1-1zm-1 7h18v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6zm3 2v2h2v-2H6zm4 0v2h2v-2h-2z"/>'+
    '</svg>'+
    '<span class="mp-ns-text">'+
      '<span class="mp-ns-title">主节点</span>'+
      '<span class="mp-ns-ip"></span>'+
    '</span>'+
    '<span class="mp-ns-caret">▾</span>';
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
	return strings.ReplaceAll(tpl, "__MASTER_IP__", string(ipJSON))
}

func (s *Server) injectHookHTML(body []byte, masterIP string) []byte {
	if bytes.Contains(body, []byte(`data-mp-hook="1"`)) {
		return body
	}
	hook := []byte(sidebarHook(masterIP))
	if i := bytes.LastIndex(bytes.ToLower(body), []byte("</body>")); i >= 0 {
		out := append([]byte{}, body[:i]...)
		out = append(out, hook...)
		out = append(out, body[i:]...)
		return out
	}
	return append(body, hook...)
}

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

// maybeGunzip returns raw body when Content-Encoding is gzip; otherwise body unchanged.
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

func (s *Server) wrapLocalProxy() {
	if s.localProxy == nil {
		return
	}
	upstream := s.localProxy.Director
	s.localProxy.Director = func(r *http.Request) {
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
		patched := s.injectHookHTML(body, s.DeviceIP())
		resp.Body = io.NopCloser(bytes.NewReader(patched))
		resp.ContentLength = int64(len(patched))
		resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
		resp.Header.Del("Transfer-Encoding")
		return nil
	}
}

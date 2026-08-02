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

func sidebarHook(deviceIP string) string {
	if deviceIP == "" {
		deviceIP = "-"
	}
	ipJSON, _ := json.Marshal(deviceIP)
	const tpl = `<script data-mp-hook="1">(function(){
var DEVICE_IP=__DEVICE_IP__;
function goMP(e){
  try{
    var t=e.target;
    if(!t)return;
    var el=t.closest?t.closest("a,.el-menu-item,.el-sub-menu__title,li,[index],#mp-menu-link"):null;
    if(!el)return;
    var idx=(el.getAttribute("index")||el.getAttribute("href")||"").toString();
    var txt=(el.textContent||"").replace(/\s+/g," ").trim();
    if(el.id==="mp-menu-link"||idx.indexOf("/__mp")===0||txt.indexOf("多机节点")>=0||txt.indexOf("MultiPanel")>=0){
      e.preventDefault();e.stopPropagation();
      location.href="/__mp/";
    }
  }catch(err){}
}
function ensureMenu(){
  var side=document.querySelector(".sidebar-container .el-menu")||document.querySelector(".el-menu");
  if(!side)return;
  // Style native HideMenu item (agent/master DB) and keep DOM fallback.
  if(!document.getElementById("mp-menu-style")){
    var st=document.createElement("style");
    st.id="mp-menu-style";
    st.textContent=
      '.el-menu-item[index="/__mp/"],#mp-menu-link.el-menu-item{height:auto!important;line-height:1.2!important;padding:10px 12px!important;margin:4px 0!important;border-radius:6px!important;}'+
      "#mp-menu-link .mp-menu-inner{display:flex;align-items:center;gap:10px;width:100%;}"+
      "#mp-menu-link .mp-menu-icon{flex:0 0 18px;opacity:.9;}"+
      "#mp-menu-link .mp-menu-text{display:flex;flex-direction:column;gap:2px;min-width:0;}"+
      "#mp-menu-link .mp-menu-title{font-size:14px;font-weight:500;}"+
      "#mp-menu-link .mp-menu-ip{font-size:12px;opacity:.65;letter-spacing:.2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"+
      '.el-menu-item[index="/__mp/"]:hover,#mp-menu-link:hover{background:rgba(0,94,235,.08)!important;}'+
      '.el-menu-item[index="/__mp/"] .mp-native-ip{display:block;font-size:12px;opacity:.65;margin-top:2px;}';
    document.head.appendChild(st);
  }
  var native=side.querySelector('.el-menu-item[index="/__mp/"]');
  if(native){
    if(!native.querySelector(".mp-native-ip") && DEVICE_IP && DEVICE_IP!=="-"){
      var ip=document.createElement("span");
      ip.className="mp-native-ip";
      ip.textContent=DEVICE_IP;
      native.appendChild(ip);
    }
    native.title="多机节点 · "+DEVICE_IP;
    return;
  }
  if(document.getElementById("mp-menu-link"))return;
  var li=document.createElement("li");
  li.id="mp-menu-link";
  li.className="el-menu-item";
  li.setAttribute("role","menuitem");
  li.setAttribute("tabindex","-1");
  li.setAttribute("index","/__mp/");
  li.title="多机节点 · "+DEVICE_IP;
  li.innerHTML='<span class="mp-menu-inner">'+
    '<svg class="mp-menu-icon" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">'+
      '<path fill="currentColor" d="M4 5h16a1 1 0 0 1 1 1v4H3V6a1 1 0 0 1 1-1zm-1 7h18v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6zm3 2v2h2v-2H6zm4 0v2h2v-2h-2z"/>'+
    '</svg>'+
    '<span class="mp-menu-text">'+
      '<span class="mp-menu-title">多机节点</span>'+
      '<span class="mp-menu-ip">'+DEVICE_IP+'</span>'+
    '</span>'+
  '</span>';
  var settings=side.querySelector('.el-menu-item[index="/settings"]') || side.querySelector('.el-sub-menu[index="/settings"]');
  if(settings&&settings.parentElement===side){ side.insertBefore(li, settings); }
  else { side.appendChild(li); }
}
document.addEventListener("click",goMP,true);
setInterval(ensureMenu,800);
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
	return strings.ReplaceAll(tpl, "__DEVICE_IP__", string(ipJSON))
}

func (s *Server) injectHookHTML(body []byte) []byte {
	if bytes.Contains(body, []byte(`data-mp-hook="1"`)) {
		return body
	}
	hook := []byte(sidebarHook(s.DeviceIP()))
	if i := bytes.LastIndex(bytes.ToLower(body), []byte("</body>")); i >= 0 {
		out := append([]byte{}, body[:i]...)
		out = append(out, hook...)
		out = append(out, body[i:]...)
		return out
	}
	return append(body, hook...)
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
		patched := s.injectHookHTML(body)
		resp.Body = io.NopCloser(bytes.NewReader(patched))
		resp.ContentLength = int64(len(patched))
		resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
		resp.Header.Del("Transfer-Encoding")
		return nil
	}
}

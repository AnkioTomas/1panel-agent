package master

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// sidebarHook forces full-page navigation for /__mp, patches sidebar, handles mp_return.
const sidebarHook = `<script data-mp-hook="1">(function(){
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
  if(document.getElementById("mp-menu-link"))return;
  var side=document.querySelector(".el-menu")||document.querySelector(".sidebar-container nav")||document.querySelector("aside .el-menu");
  if(!side)return;
  var a=document.createElement("a");
  a.id="mp-menu-link";
  a.href="/__mp/";
  a.textContent="多机节点";
  a.setAttribute("index","/__mp/");
  a.style.cssText="display:flex;align-items:center;margin:8px 12px;padding:10px 14px;border-radius:8px;background:#2563eb;color:#fff;text-decoration:none;font-size:14px;font-weight:600";
  side.appendChild(a);
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

func injectHookHTML(body []byte) []byte {
	if bytes.Contains(body, []byte(`data-mp-hook="1"`)) {
		return body
	}
	if i := bytes.LastIndex(bytes.ToLower(body), []byte("</body>")); i >= 0 {
		out := append([]byte{}, body[:i]...)
		out = append(out, []byte(sidebarHook)...)
		out = append(out, body[i:]...)
		return out
	}
	return append(body, []byte(sidebarHook)...)
}

func (s *Server) wrapLocalProxy() {
	if s.localProxy == nil {
		return
	}
	upstream := s.localProxy.Director
	s.localProxy.Director = func(r *http.Request) {
		upstream(r)
		// Avoid compressed HTML we can't patch reliably without full decode.
		r.Header.Del("Accept-Encoding")
	}
	s.localProxy.ModifyResponse = func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			return nil
		}
		// Don't inject into tiny error pages unnecessarily — still OK if we do.
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
		patched := injectHookHTML(body)
		resp.Body = io.NopCloser(bytes.NewReader(patched))
		resp.ContentLength = int64(len(patched))
		resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
		resp.Header.Del("Transfer-Encoding")
		return nil
	}
}

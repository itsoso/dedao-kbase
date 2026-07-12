package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/yann0917/dedao-gui/backend/app"
)

type enrollmentLogin interface {
	StartLogin(context.Context) error
	QRImage(context.Context) ([]byte, string, error)
	LoginStatus(context.Context) (any, error)
	Logout(context.Context) error
}

type enrollmentDiscovery interface {
	SearchOfficialAccounts(context.Context, string) ([]app.WeChatOfficialAccount, error)
	ListOfficialAccountArticles(context.Context, string, int, int) ([]app.WeChatOfficialArticle, error)
}

type enrollmentHandlerConfig struct {
	CSRFToken   string
	RemoteURL   string
	AgentID     string
	ReportError func(string, error)
}

func newEnrollmentHandler(login enrollmentLogin, discovery enrollmentDiscovery, config enrollmentHandlerConfig) (http.Handler, error) {
	csrf := strings.TrimSpace(config.CSRFToken)
	if login == nil || csrf == "" {
		return nil, fmt.Errorf("enrollment login and CSRF secret are required")
	}
	page, err := template.New("enrollment").Parse(enrollmentPageHTML)
	if err != nil {
		return nil, fmt.Errorf("parse enrollment page: %w", err)
	}
	mux := http.NewServeMux()
	guard := func(mutating bool, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if parsedHost, _, splitErr := net.SplitHostPort(r.Host); splitErr == nil {
				host = parsedHost
			}
			if !isLoopbackHost(host) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !isLoopbackHost(u.Hostname()) {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
			if mutating && r.Header.Get("X-CSRF-Token") != csrf {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("/", guard(false, requireMethod(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; connect-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'")
		_ = page.Execute(w, map[string]string{
			"CSRFToken": csrf,
			"RemoteURL": strings.TrimSpace(config.RemoteURL),
			"AgentID":   strings.TrimSpace(config.AgentID),
		})
	})))
	mux.HandleFunc("/api/local/wechat/login/start", guard(true, requireMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		if login.StartLogin(r.Context()) != nil {
			http.Error(w, "login start failed", 502)
			return
		}
		writeEnrollmentJSON(w, map[string]bool{"ok": true})
	})))
	mux.HandleFunc("/api/local/wechat/login/qr", guard(false, requireMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		data, kind, err := login.QRImage(r.Context())
		if err != nil {
			http.Error(w, "QR unavailable", 502)
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Write(data)
	})))
	mux.HandleFunc("/api/local/wechat/login/status", guard(false, requireMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		status, err := login.LoginStatus(r.Context())
		if err != nil {
			if config.ReportError != nil {
				config.ReportError("login_status", err)
			}
			http.Error(w, "status unavailable", 502)
			return
		}
		writeEnrollmentJSON(w, status)
	})))
	mux.HandleFunc("/api/local/wechat/logout", guard(true, requireMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		if login.Logout(r.Context()) != nil {
			http.Error(w, "logout failed", 502)
			return
		}
		writeEnrollmentJSON(w, map[string]bool{"ok": true})
	})))
	mux.HandleFunc("/api/local/wechat/accounts", guard(false, requireMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		if discovery == nil {
			http.Error(w, "account discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" || len([]rune(query)) > 100 {
			http.Error(w, "invalid query", http.StatusBadRequest)
			return
		}
		accounts, err := discovery.SearchOfficialAccounts(r.Context(), query)
		if err != nil {
			if config.ReportError != nil {
				config.ReportError("account_search", err)
			}
			if isEnrollmentLoginRequiredError(err) {
				http.Error(w, "login required", http.StatusUnauthorized)
				return
			}
			http.Error(w, "account search failed", http.StatusBadGateway)
			return
		}
		writeEnrollmentJSON(w, map[string]any{"accounts": accounts})
	})))
	mux.HandleFunc("/api/local/wechat/articles", guard(false, requireMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		if discovery == nil {
			http.Error(w, "article discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		fakeID := strings.TrimSpace(r.URL.Query().Get("fakeid"))
		begin, beginErr := strconv.Atoi(r.URL.Query().Get("begin"))
		count, countErr := strconv.Atoi(r.URL.Query().Get("count"))
		if fakeID == "" || beginErr != nil || begin < 0 || countErr != nil || count < 1 || count > 20 {
			http.Error(w, "invalid article query", http.StatusBadRequest)
			return
		}
		articles, err := discovery.ListOfficialAccountArticles(r.Context(), fakeID, begin, count)
		if err != nil {
			if config.ReportError != nil {
				config.ReportError("article_list", err)
			}
			if isEnrollmentLoginRequiredError(err) {
				http.Error(w, "login required", http.StatusUnauthorized)
				return
			}
			http.Error(w, "article discovery failed", http.StatusBadGateway)
			return
		}
		writeEnrollmentJSON(w, map[string]any{"articles": articles, "begin": begin, "count": count})
	})))
	return mux, nil
}

func isEnrollmentLoginRequiredError(err error) bool {
	return errors.Is(err, app.ErrWeChatCredentialsNotConfigured) || app.WeChatDiscoveryErrorCode(err) == "login_required"
}

func requireMethod(want string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != want {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func writeEnrollmentJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

const enrollmentPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>KBase 微信采集器</title>
  <style>
    :root{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#1f2328;background:#f5f6f7}*{box-sizing:border-box}body{margin:0}button,input,a{font:inherit}.shell{max-width:1120px;margin:0 auto;padding:28px}.top{display:flex;align-items:end;justify-content:space-between;border-bottom:1px solid #d8dee4;padding-bottom:18px}.top h1{margin:4px 0;font-size:30px}.muted{color:#667085}.grid{display:grid;grid-template-columns:minmax(280px,360px) minmax(0,1fr);gap:18px;padding-top:18px}.panel{background:#fff;border:1px solid #d8dee4;border-radius:8px;padding:18px}.panel h2{font-size:18px;margin:0 0 14px}.actions{display:flex;gap:8px;flex-wrap:wrap}.button{border:1px solid #c7ccd1;background:#fff;border-radius:6px;padding:9px 14px;cursor:pointer;text-decoration:none;color:inherit}.primary{background:#ff6b00;border-color:#ff6b00;color:#fff}.status{margin:12px 0;padding:10px;background:#f2f4f7;border-radius:6px}.qr{display:block;width:220px;min-height:220px;object-fit:contain;background:#f5f6f7;margin:14px auto}.search{display:flex;gap:8px}.search input{flex:1;min-width:0;border:1px solid #c7ccd1;border-radius:6px;padding:10px}.list{display:grid;gap:8px;margin-top:14px}.row{display:flex;gap:12px;align-items:start;justify-content:space-between;border-top:1px solid #edf0f2;padding:12px 0}.row strong{display:block}.row small{color:#667085;word-break:break-all}.empty{color:#667085;padding:24px 0}.online-link{display:none;margin-top:12px}@media(max-width:760px){.shell{padding:16px}.grid{grid-template-columns:1fr}.top{align-items:start;flex-direction:column}.qr{width:180px;min-height:180px}}
  </style>
</head>
<body>
<main id="source-agent-enrollment" class="shell" data-csrf="{{.CSRFToken}}" data-remote-url="{{.RemoteURL}}" data-agent-id="{{.AgentID}}">
  <header class="top"><div><p class="muted">KBase Local Agent</p><h1>微信公众号采集器</h1><span class="muted">微信会话只保存在本机 Keychain</span></div><strong>{{.AgentID}}</strong></header>
  <div class="grid">
    <section class="panel"><h2>扫码登录</h2><div id="login-status" class="status">正在检查登录状态</div><img id="login-qr" class="qr" alt="微信公众号平台登录二维码"><div class="actions"><button id="login-start" class="button primary" type="button">开始登录</button><button id="login-logout" class="button" type="button">退出登录</button></div></section>
    <section class="panel"><h2>公众号搜索</h2><form id="account-search" class="search"><input name="q" placeholder="输入公众号名称" autocomplete="off"><button class="button primary" type="submit">搜索</button></form><div id="account-results" class="list"><p class="empty">登录后搜索公众号。</p></div><a id="online-control" class="button primary online-link" target="_blank" rel="noreferrer">打开在线控制台</a><h2 style="margin-top:22px">最近文章</h2><div id="article-results" class="list"><p class="empty">选择公众号后显示文章。</p></div></section>
  </div>
</main>
<script>
(()=>{const root=document.querySelector('#source-agent-enrollment');const csrf=root.dataset.csrf;const status=document.querySelector('#login-status');const qr=document.querySelector('#login-qr');const accounts=document.querySelector('#account-results');const articles=document.querySelector('#article-results');const online=document.querySelector('#online-control');let timer;
const request=async(path,options={})=>{const headers={...(options.headers||{})};if(options.method&&options.method!=='GET')headers['X-CSRF-Token']=csrf;const response=await fetch(path,{...options,headers});if(!response.ok)throw new Error(await response.text()||('HTTP '+response.status));const type=response.headers.get('content-type')||'';return type.includes('json')?response.json():response;};
const setStatus=value=>{status.textContent=value};const poll=async()=>{try{const data=await request('/api/local/wechat/login/status');setStatus(data.state==='confirmed'?'已登录':data.state==='scanned'?'已扫码，请在手机确认':data.state==='expired'?'二维码已过期':'等待扫码');if(data.state==='confirmed'){clearInterval(timer)}}catch{setStatus('尚未登录')}};
document.querySelector('#login-start').onclick=async()=>{try{await request('/api/local/wechat/login/start',{method:'POST'});qr.src='/api/local/wechat/login/qr?t='+Date.now();setStatus('请使用微信扫码');clearInterval(timer);timer=setInterval(poll,1500)}catch(error){setStatus(error.message)}};
document.querySelector('#login-logout').onclick=async()=>{try{await request('/api/local/wechat/logout',{method:'POST'});qr.removeAttribute('src');setStatus('已退出')}catch(error){setStatus(error.message)}};
document.querySelector('#account-search').onsubmit=async event=>{event.preventDefault();const q=new FormData(event.currentTarget).get('q');accounts.textContent='搜索中';try{const data=await request('/api/local/wechat/accounts?q='+encodeURIComponent(q));accounts.replaceChildren();for(const account of data.accounts||[]){const row=document.createElement('div');row.className='row';const text=document.createElement('div');const title=document.createElement('strong');title.textContent=account.nickname||account.fakeid;const key=document.createElement('small');key.textContent=account.fakeid;text.append(title,key);const button=document.createElement('button');button.className='button';button.textContent='选择';button.onclick=()=>loadArticles(account);row.append(text,button);accounts.append(row)}if(!accounts.children.length)accounts.textContent='未找到公众号'}catch(error){accounts.textContent=error.message}};
const loadArticles=async account=>{articles.textContent='加载中';try{const data=await request('/api/local/wechat/articles?fakeid='+encodeURIComponent(account.fakeid)+'&begin=0&count=10');articles.replaceChildren();for(const article of data.articles||[]){const row=document.createElement('div');row.className='row';const text=document.createElement('div');const title=document.createElement('strong');title.textContent=article.title||article.aid;const link=document.createElement('small');link.textContent=article.link||'';text.append(title,link);row.append(text);articles.append(row)}if(!articles.children.length)articles.textContent='暂无文章';if(root.dataset.remoteUrl){const target=new URL('/wechat-source',root.dataset.remoteUrl);target.searchParams.set('source_account_key',account.fakeid);target.searchParams.set('source_account',account.nickname||account.fakeid);if(root.dataset.agentId)target.searchParams.set('agent_id',root.dataset.agentId);online.href=target.toString();online.style.display='inline-block'}}catch(error){articles.textContent=error.message}};poll();window.setInterval(poll,5000);})();
</script>
</body>
</html>`

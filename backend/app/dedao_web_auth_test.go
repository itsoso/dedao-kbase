package app

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestKBaseHTTPHandlerSanitizesDedaoAuthErrors(t *testing.T) {
	auth := &fakeDedaoWebAuth{
		qrError:    errors.New("private-cookie /srv/private/config.json"),
		checkError: errors.New("access_token=/private/token"),
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "secret-token", DedaoAuth: auth,
	})

	qr := requestJSONKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "secret-token", `{}`)
	if qr.Code != http.StatusBadGateway || !strings.Contains(qr.Body.String(), "failed to create dedao login qrcode") {
		t.Fatalf("qrcode error status=%d body=%s", qr.Code, qr.Body.String())
	}
	assertDedaoWebAuthResponseOmitsSecrets(t, qr.Body.String())

	check := requestJSONKBase(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"short-lived","qr_code_string":"qr-code"}`)
	if check.Code != http.StatusBadGateway || !strings.Contains(check.Body.String(), "failed to verify dedao login") {
		t.Fatalf("check error status=%d body=%s", check.Code, check.Body.String())
	}
	assertDedaoWebAuthResponseOmitsSecrets(t, check.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoSessionAndQRCode(t *testing.T) {
	auth := &fakeDedaoWebAuth{
		session: DedaoSession{
			LoggedIn:   true,
			ActiveUser: &DedaoSessionUser{UIDHazy: "safe-user", Name: "测试用户"},
			UserCount:  1,
		},
		qr: DedaoLoginQRCode{
			Token:        "short-lived-token",
			QRCode:       "data:image/png;base64,cXI=",
			QRCodeString: "qr-code-string",
		},
		check: DedaoLoginCheck{
			Status: 1,
			User:   &DedaoSessionUser{UIDHazy: "safe-user", Name: "测试用户"},
			Session: DedaoSession{
				LoggedIn:   true,
				ActiveUser: &DedaoSessionUser{UIDHazy: "safe-user", Name: "测试用户"},
				UserCount:  1,
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		DedaoAuth: auth,
	})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/dedao/session", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("session without bearer status = %d, want 401", unauthorized.Code)
	}

	session := requestKBase(handler, http.MethodGet, "/api/dedao/session", "secret-token")
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"logged_in":true`) || !strings.Contains(session.Body.String(), `"name":"测试用户"`) {
		t.Fatalf("session status=%d body=%s", session.Code, session.Body.String())
	}
	if !strings.Contains(session.Header().Get("Cache-Control"), "no-store") || session.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("session cache headers = %#v", session.Header())
	}
	assertDedaoWebAuthResponseOmitsSecrets(t, session.Body.String())

	qr := requestJSONKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "secret-token", `{}`)
	if qr.Code != http.StatusOK || !strings.Contains(qr.Body.String(), `"qr_code_string":"qr-code-string"`) {
		t.Fatalf("qrcode status=%d body=%s", qr.Code, qr.Body.String())
	}
	if !strings.Contains(qr.Header().Get("Cache-Control"), "no-store") || qr.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("qrcode cache headers = %#v", qr.Header())
	}
	assertDedaoWebAuthResponseOmitsSecrets(t, qr.Body.String())

	invalid := requestJSONKBase(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid check status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	checked := requestJSONKBase(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"short-lived-token","qr_code_string":"qr-code-string"}`)
	if checked.Code != http.StatusOK || auth.gotToken != "short-lived-token" || auth.gotQRCodeString != "qr-code-string" {
		t.Fatalf("check status=%d body=%s token=%q qr=%q", checked.Code, checked.Body.String(), auth.gotToken, auth.gotQRCodeString)
	}
	if !strings.Contains(checked.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("check Cache-Control = %q", checked.Header().Get("Cache-Control"))
	}
	assertDedaoWebAuthResponseOmitsSecrets(t, checked.Body.String())
}

func TestLiveDedaoAuthProviderUsesDedicatedLoginService(t *testing.T) {
	loginService := &fakeDedaoLoginService{
		token: "login-token",
		qr: &services.QrCodeResp{
			Data: struct {
				QrCode       string `json:"qrcode"`
				QrCodeString string `json:"qrCodeString"`
			}{QrCode: "qr-image", QrCodeString: "qr-string"},
		},
		check: &services.CheckLoginResp{},
	}
	provider := &liveDedaoAuthProvider{newLoginService: func() dedaoLoginService {
		loginService.factoryCalls++
		return loginService
	}}

	qr, err := provider.NewQRCode()
	if err != nil {
		t.Fatalf("NewQRCode returned error: %v", err)
	}
	if qr.Token != "login-token" || qr.QRCode != "qr-image" || qr.QRCodeString != "qr-string" {
		t.Fatalf("qr = %#v", qr)
	}
	if loginService.factoryCalls != 1 || loginService.accessTokenCalls != 1 || loginService.qrCalls != 1 {
		t.Fatalf("qrcode calls = factory %d token %d qr %d", loginService.factoryCalls, loginService.accessTokenCalls, loginService.qrCalls)
	}

	if _, err := provider.CheckLogin("login-token", "qr-string"); err != nil {
		t.Fatalf("CheckLogin returned error: %v", err)
	}
	if loginService.factoryCalls != 1 || loginService.checkCalls != 1 {
		t.Fatalf("check calls = factory %d check %d", loginService.factoryCalls, loginService.checkCalls)
	}
}

func TestLiveDedaoAuthProviderKeepsConcurrentQRCodeFlowsIsolated(t *testing.T) {
	success := func() *services.CheckLoginResp {
		result := &services.CheckLoginResp{}
		result.Data.Status = 1
		return result
	}
	loginServices := []*fakeDedaoLoginService{
		{token: "token-one", qr: dedaoTestQRCode("qr-one"), check: success(), cookie: "session=one", bootstrapCookies: []string{"csrfToken=csrf-one; Path=/"}},
		{token: "token-two", qr: dedaoTestQRCode("qr-two"), check: success(), cookie: "session=two", bootstrapCookies: []string{"csrfToken=csrf-two; Path=/"}},
	}
	factoryIndex := 0
	completed := make(map[string]string)
	var completedMu sync.Mutex
	provider := &liveDedaoAuthProvider{
		newLoginService: func() dedaoLoginService {
			service := loginServices[factoryIndex]
			factoryIndex++
			return service
		},
		loginByCookie: func(cookie string, bootstrapCookies []string) (*services.User, error) {
			completedMu.Lock()
			completed[cookie] = strings.Join(bootstrapCookies, "; ")
			completedMu.Unlock()
			return &services.User{UIDHazy: cookie}, nil
		},
	}

	qrOne, err := provider.NewQRCode()
	if err != nil {
		t.Fatal(err)
	}
	qrTwo, err := provider.NewQRCode()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, qr := range []DedaoLoginQRCode{qrOne, qrTwo} {
		qr := qr
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, checkErr := provider.CheckLogin(qr.Token, qr.QRCodeString); checkErr != nil {
				t.Errorf("check %s: %v", qr.Token, checkErr)
			}
		}()
	}
	wg.Wait()

	completedMu.Lock()
	defer completedMu.Unlock()
	if completed["session=one"] != "csrfToken=csrf-one; Path=/" || completed["session=two"] != "csrfToken=csrf-two; Path=/" {
		t.Fatalf("completed login cookies crossed flows: %#v", completed)
	}
}

func dedaoTestQRCode(qrCodeString string) *services.QrCodeResp {
	result := &services.QrCodeResp{}
	result.Data.QrCode = "image-" + qrCodeString
	result.Data.QrCodeString = qrCodeString
	return result
}

func assertDedaoWebAuthResponseOmitsSecrets(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"CookieStr", "cookie_str", "access_token", "config.json", "private-cookie"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
}

type fakeDedaoWebAuth struct {
	session         DedaoSession
	qr              DedaoLoginQRCode
	check           DedaoLoginCheck
	gotToken        string
	gotQRCodeString string
	qrError         error
	checkError      error
}

func (f *fakeDedaoWebAuth) Session() DedaoSession { return f.session }

func (f *fakeDedaoWebAuth) NewQRCode() (DedaoLoginQRCode, error) { return f.qr, f.qrError }

func (f *fakeDedaoWebAuth) CheckLogin(token, qrCodeString string) (DedaoLoginCheck, error) {
	f.gotToken = token
	f.gotQRCodeString = qrCodeString
	return f.check, f.checkError
}

type fakeDedaoLoginService struct {
	token            string
	qr               *services.QrCodeResp
	check            *services.CheckLoginResp
	factoryCalls     int
	accessTokenCalls int
	qrCalls          int
	checkCalls       int
	cookie           string
	bootstrapCookies []string
}

func (f *fakeDedaoLoginService) LoginAccessToken() (string, error) {
	f.accessTokenCalls++
	return f.token, nil
}

func (f *fakeDedaoLoginService) GetQrcode(string) (*services.QrCodeResp, error) {
	f.qrCalls++
	return f.qr, nil
}

func (f *fakeDedaoLoginService) CheckLogin(string, string) (*services.CheckLoginResp, string, error) {
	f.checkCalls++
	return f.check, f.cookie, nil
}

func (f *fakeDedaoLoginService) BootstrapCookies() []string {
	return append([]string(nil), f.bootstrapCookies...)
}

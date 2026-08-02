package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

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
	provider := liveDedaoAuthProvider{newLoginService: func() dedaoLoginService {
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
	if loginService.factoryCalls != 2 || loginService.checkCalls != 1 {
		t.Fatalf("check calls = factory %d check %d", loginService.factoryCalls, loginService.checkCalls)
	}
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
}

func (f *fakeDedaoWebAuth) Session() DedaoSession { return f.session }

func (f *fakeDedaoWebAuth) NewQRCode() (DedaoLoginQRCode, error) { return f.qr, nil }

func (f *fakeDedaoWebAuth) CheckLogin(token, qrCodeString string) (DedaoLoginCheck, error) {
	f.gotToken = token
	f.gotQRCodeString = qrCodeString
	return f.check, nil
}

type fakeDedaoLoginService struct {
	token            string
	qr               *services.QrCodeResp
	check            *services.CheckLoginResp
	factoryCalls     int
	accessTokenCalls int
	qrCalls          int
	checkCalls       int
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
	return f.check, "", nil
}

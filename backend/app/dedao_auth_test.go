package app

import (
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestLiveDedaoAuthProviderUsesDedicatedLoginService(t *testing.T) {
	loginService := &fakeDedaoLoginService{
		token: "login-token",
		qr: &services.QrCodeResp{
			Data: struct {
				QrCode       string `json:"qrcode"`
				QrCodeString string `json:"qrCodeString"`
			}{
				QrCode:       "qr-image",
				QrCodeString: "qr-string",
			},
		},
		check: &services.CheckLoginResp{
			Data: struct {
				Status int `json:"status"`
			}{
				Status: 0,
			},
		},
	}
	provider := liveDedaoAuthProvider{
		newLoginService: func() dedaoLoginService {
			loginService.factoryCalls++
			return loginService
		},
	}

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

	result, err := provider.CheckLogin("login-token", "qr-string")
	if err != nil {
		t.Fatalf("CheckLogin returned error: %v", err)
	}
	if result.Status != 0 {
		t.Fatalf("check status = %d, want 0", result.Status)
	}
	if loginService.factoryCalls != 2 || loginService.checkCalls != 1 {
		t.Fatalf("check calls = factory %d check %d", loginService.factoryCalls, loginService.checkCalls)
	}
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

func (f *fakeDedaoLoginService) GetQrcode(token string) (*services.QrCodeResp, error) {
	f.qrCalls++
	return f.qr, nil
}

func (f *fakeDedaoLoginService) CheckLogin(token string, qrCodeString string) (*services.CheckLoginResp, string, error) {
	f.checkCalls++
	return f.check, "", nil
}

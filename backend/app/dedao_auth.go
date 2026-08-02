package app

import (
	"errors"
	"sync"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

type DedaoLoginQRCode struct {
	Token        string `json:"token"`
	QRCode       string `json:"qr_code"`
	QRCodeString string `json:"qr_code_string"`
}

type DedaoLoginCheckRequest struct {
	Token        string `json:"token"`
	QRCodeString string `json:"qr_code_string"`
}

type DedaoLoginCheck struct {
	Status  int               `json:"status"`
	Expired bool              `json:"expired,omitempty"`
	User    *DedaoSessionUser `json:"user,omitempty"`
	Session DedaoSession      `json:"session"`
}

type DedaoAuthProvider interface {
	Session() DedaoSession
	NewQRCode() (DedaoLoginQRCode, error)
	CheckLogin(token, qrCodeString string) (DedaoLoginCheck, error)
}

type dedaoLoginService interface {
	LoginAccessToken() (string, error)
	GetQrcode(token string) (*services.QrCodeResp, error)
	CheckLogin(token, qrcode string) (*services.CheckLoginResp, string, error)
	BootstrapCookies() []string
}

type liveDedaoAuthProvider struct {
	newLoginService func() dedaoLoginService
	loginByCookie   func(string, []string) (*services.User, error)
	now             func() time.Time
	mu              sync.Mutex
	completionMu    sync.Mutex
	loginFlows      map[string]*dedaoLoginFlow
}

type dedaoLoginFlow struct {
	service   dedaoLoginService
	createdAt time.Time
	mu        sync.Mutex
	terminal  bool
}

const dedaoLoginFlowTTL = 10 * time.Minute

func defaultDedaoAuthProvider(provider DedaoAuthProvider) DedaoAuthProvider {
	if provider != nil {
		return provider
	}
	return &liveDedaoAuthProvider{newLoginService: func() dedaoLoginService {
		return services.NewService(&services.CookieOptions{})
	}}
}

func (p *liveDedaoAuthProvider) Session() DedaoSession {
	return CurrentDedaoSession()
}

func (p *liveDedaoAuthProvider) loginService() dedaoLoginService {
	if p.newLoginService != nil {
		return p.newLoginService()
	}
	return services.NewService(&services.CookieOptions{})
}

func (p *liveDedaoAuthProvider) NewQRCode() (DedaoLoginQRCode, error) {
	service := p.loginService()
	token, err := service.LoginAccessToken()
	if err != nil {
		return DedaoLoginQRCode{}, err
	}
	code, err := service.GetQrcode(token)
	if err != nil {
		return DedaoLoginQRCode{}, err
	}
	if code == nil {
		return DedaoLoginQRCode{}, errors.New("empty qrcode response")
	}
	if token == "" || code.Data.QrCodeString == "" {
		return DedaoLoginQRCode{}, errors.New("invalid qrcode response")
	}
	p.storeLoginFlow(token, service)
	return DedaoLoginQRCode{
		Token:        token,
		QRCode:       code.Data.QrCode,
		QRCodeString: code.Data.QrCodeString,
	}, nil
}

func (p *liveDedaoAuthProvider) CheckLogin(token, qrCodeString string) (DedaoLoginCheck, error) {
	flow, ok := p.loginFlow(token)
	if !ok {
		return DedaoLoginCheck{}, errors.New("dedao login flow not found; request a new qrcode")
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.terminal {
		return DedaoLoginCheck{}, errors.New("dedao login flow already completed")
	}
	check, cookie, err := flow.service.CheckLogin(token, qrCodeString)
	if err != nil {
		return DedaoLoginCheck{}, err
	}
	result := DedaoLoginCheck{Session: p.Session()}
	if check == nil {
		return result, nil
	}
	result.Status = check.Data.Status
	switch check.Data.Status {
	case 1:
		flow.terminal = true
		p.removeLoginFlow(token, flow)
		p.completionMu.Lock()
		user, err := p.completeLogin(cookie, flow.service.BootstrapCookies())
		if err != nil {
			p.completionMu.Unlock()
			return DedaoLoginCheck{}, err
		}
		result.User = dedaoSessionUserFromServiceUser(user)
		result.Session = p.Session()
		p.completionMu.Unlock()
	case 2:
		flow.terminal = true
		p.removeLoginFlow(token, flow)
		result.Expired = true
	}
	return result, nil
}

func (p *liveDedaoAuthProvider) completeLogin(cookie string, bootstrapCookies []string) (*services.User, error) {
	if p.loginByCookie != nil {
		return p.loginByCookie(cookie, bootstrapCookies)
	}
	return LoginByCookieWithBootstrap(cookie, bootstrapCookies)
}

func (p *liveDedaoAuthProvider) currentTime() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *liveDedaoAuthProvider) storeLoginFlow(token string, service dedaoLoginService) {
	now := p.currentTime()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loginFlows == nil {
		p.loginFlows = make(map[string]*dedaoLoginFlow)
	}
	for existingToken, flow := range p.loginFlows {
		if now.Sub(flow.createdAt) > dedaoLoginFlowTTL {
			delete(p.loginFlows, existingToken)
		}
	}
	p.loginFlows[token] = &dedaoLoginFlow{service: service, createdAt: now}
}

func (p *liveDedaoAuthProvider) loginFlow(token string) (*dedaoLoginFlow, bool) {
	now := p.currentTime()
	p.mu.Lock()
	defer p.mu.Unlock()
	flow, ok := p.loginFlows[token]
	if !ok {
		return nil, false
	}
	if now.Sub(flow.createdAt) > dedaoLoginFlowTTL {
		delete(p.loginFlows, token)
		return nil, false
	}
	return flow, true
}

func (p *liveDedaoAuthProvider) removeLoginFlow(token string, flow *dedaoLoginFlow) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loginFlows[token] == flow {
		delete(p.loginFlows, token)
	}
}

func dedaoSessionUserFromServiceUser(user *services.User) *DedaoSessionUser {
	if user == nil {
		return nil
	}
	return &DedaoSessionUser{
		UIDHazy: user.UIDHazy,
		Name:    user.Nickname,
		Avatar:  user.Avatar,
	}
}

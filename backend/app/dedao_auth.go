package app

import (
	"errors"

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
	NewQRCode() (DedaoLoginQRCode, error)
	CheckLogin(token string, qrCodeString string) (DedaoLoginCheck, error)
}

type liveDedaoAuthProvider struct{}

func defaultDedaoAuthProvider(provider DedaoAuthProvider) DedaoAuthProvider {
	if provider != nil {
		return provider
	}
	return liveDedaoAuthProvider{}
}

func (liveDedaoAuthProvider) NewQRCode() (DedaoLoginQRCode, error) {
	service := getService()
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
	return DedaoLoginQRCode{
		Token:        token,
		QRCode:       code.Data.QrCode,
		QRCodeString: code.Data.QrCodeString,
	}, nil
}

func (liveDedaoAuthProvider) CheckLogin(token string, qrCodeString string) (DedaoLoginCheck, error) {
	check, cookie, err := getService().CheckLogin(token, qrCodeString)
	if err != nil {
		return DedaoLoginCheck{}, err
	}
	result := DedaoLoginCheck{
		Session: CurrentDedaoSession(),
	}
	if check == nil {
		return result, nil
	}
	result.Status = check.Data.Status
	switch check.Data.Status {
	case 1:
		user, err := LoginByCookie(cookie)
		if err != nil {
			return DedaoLoginCheck{}, err
		}
		result.User = dedaoSessionUserFromServiceUser(user)
		result.Session = CurrentDedaoSession()
	case 2:
		result.Expired = true
	}
	return result, nil
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

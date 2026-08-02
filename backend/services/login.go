package services

import "encoding/json"

type QrCodeResp struct {
	ErrCode int    `json:"errCode"`
	ErrMsg  string `json:"errMsg"`
	Data    struct {
		QrCode       string `json:"qrcode"`
		QrCodeString string `json:"qrCodeString"`
	} `json:"data"`
}

type CheckLoginResp struct {
	ErrCode int    `json:"errCode"`
	ErrMsg  string `json:"errMsg"`
	Data    struct {
		Status int `json:"status"` // 1-扫码成功,2-过期
	} `json:"data"`
}

type loginAccessTokenError struct {
	Message string `json:"message"`
}

// LoginAccessToken get login access token
func (s *Service) LoginAccessToken() (token string, err error) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.csrfToken == "" {
		if _, err = s.getHomeInitialStateLocked(); err != nil {
			return
		}
	}
	token, err = s.reqGetLoginAccessToken(s.csrfToken)
	if err != nil {
		return
	}
	if loginAccessTokenNeedsCSRFRefresh(token) {
		s.csrfToken = ""
		if _, err = s.getHomeInitialStateLocked(); err != nil {
			return
		}
		token, err = s.reqGetLoginAccessToken(s.csrfToken)
		if err != nil {
			return
		}
	}
	return
}

func loginAccessTokenNeedsCSRFRefresh(token string) bool {
	var tokenErr loginAccessTokenError
	if err := json.Unmarshal([]byte(token), &tokenErr); err != nil {
		return false
	}
	return tokenErr.Message == "missing csrf token" || tokenErr.Message == "invalid csrf token"
}

func (s *Service) GetQrcode(token string) (resp *QrCodeResp, err error) {
	resp, err = s.reqGetQrcode(token)
	if err != nil {
		return
	}
	return
}

func (s *Service) CheckLogin(token, qrcode string) (check *CheckLoginResp, cookie string, err error) {
	check, cookie, err = s.reqCheckLogin(token, qrcode)
	if err != nil {
		return
	}
	return
}

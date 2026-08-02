package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yann0917/dedao-gui/backend/config"
	"github.com/yann0917/dedao-gui/backend/services"
)

// LoginByCookie login by cookie
func LoginByCookie(cookie string) (user *services.User, err error) {
	return LoginByCookieWithBootstrap(cookie, nil)
}

// LoginByCookieWithBootstrap completes login with cookies captured by the same
// QR login service that created and checked the code.
func LoginByCookieWithBootstrap(cookie string, bootstrapCookies []string) (user *services.User, err error) {
	var u config.Dedao
	cookieParts := make([]string, 0, 1+len(bootstrapCookies))
	if cookie = strings.TrimSpace(cookie); cookie != "" {
		cookieParts = append(cookieParts, cookie)
	}
	for _, bootstrapCookie := range bootstrapCookies {
		if bootstrapCookie = strings.TrimSpace(bootstrapCookie); bootstrapCookie != "" {
			cookieParts = append(cookieParts, bootstrapCookie)
		}
	}
	cookie = strings.Join(cookieParts, "; ")
	err = services.ParseCookies(cookie, &u.CookieOptions)
	if err != nil {
		return
	}
	// save config
	u.CookieStr = cookie
	if _, user, err = config.Instance.SetUser(&u); err != nil {
		return
	}
	if err = config.Instance.Save(); err != nil {
		return
	}
	return
}

// CheckLogin 需要开启定时器轮询是否扫码登录
func CheckLogin(token, qrCode string) (user *services.User, err error) {
	service := getService()
	check, cookie, err := service.CheckLogin(token, qrCode)
	if err != nil {
		return
	}
	if check.Data.Status == 1 {
		user, err = LoginByCookieWithBootstrap(cookie, service.BootstrapCookies())
		if err != nil {
			return
		}
		fmt.Println("扫码成功")
	} else if check.Data.Status == 2 {
		err = errors.New("登录失败，二维码已过期")
		return
	}
	return
}

func Logout() (err error) {
	err = config.Instance.DeleteConfigFile()
	return
}

func SwitchAccount(uid string) (err error) {
	if config.Instance.LoginUserCount() == 0 {
		err = errors.New("cannot found account's")
		return
	}
	err = config.Instance.SwitchUser(&config.User{UIDHazy: uid})

	if err != nil {
		return err
	}
	return
}

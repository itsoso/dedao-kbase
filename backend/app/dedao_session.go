package app

import "github.com/yann0917/dedao-gui/backend/config"

type DedaoSessionUser struct {
	UIDHazy string `json:"uid_hazy,omitempty"`
	Name    string `json:"name,omitempty"`
	Avatar  string `json:"avatar,omitempty"`
}

type DedaoSession struct {
	LoggedIn   bool              `json:"logged_in"`
	ActiveUser *DedaoSessionUser `json:"active_user,omitempty"`
	UserCount  int               `json:"user_count"`
}

func CurrentDedaoSession() DedaoSession {
	session := DedaoSession{
		UserCount: config.Instance.LoginUserCount(),
	}
	active := config.Instance.ActiveUser()
	if active == nil || active.UIDHazy == "" {
		return session
	}
	session.LoggedIn = true
	session.ActiveUser = &DedaoSessionUser{
		UIDHazy: active.UIDHazy,
		Name:    active.Name,
		Avatar:  active.Avatar,
	}
	return session
}

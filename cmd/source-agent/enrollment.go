package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type enrollmentLogin interface {
	StartLogin(context.Context) error
	QRImage(context.Context) ([]byte, string, error)
	LoginStatus(context.Context) (any, error)
	Logout(context.Context) error
}

func newEnrollmentHandler(login enrollmentLogin, csrf string) (http.Handler, error) {
	if login == nil || strings.TrimSpace(csrf) == "" {
		return nil, fmt.Errorf("enrollment login and CSRF secret are required")
	}
	mux := http.NewServeMux()
	guard := func(mutating bool, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
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
	return mux, nil
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
	_ = json.NewEncoder(w).Encode(v)
}

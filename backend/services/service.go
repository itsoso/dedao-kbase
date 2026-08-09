package services

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
	"github.com/mitchellh/mapstructure"
	"github.com/yann0917/dedao-gui/backend/utils"
)

var (
	dedaoCommURL = &url.URL{
		Scheme: "https",
		Host:   "dedao.cn",
	}
	baseURL   = "https://www.dedao.cn"
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36"
)

// Response dedao success response
type Response struct {
	H respH `json:"h"`
	C respC `json:"c"`
}

type respH struct {
	C   int    `json:"c"`
	E   string `json:"e"`
	S   int    `json:"s"`
	T   int    `json:"t"`
	Apm string `json:"apm"`
}

// respC response content
type respC []byte

func (r *respC) UnmarshalJSON(data []byte) error {
	*r = data

	return nil
}

func (r respC) String() string {
	return string(r)
}

// Service dedao service
type Service struct {
	client           *resty.Client
	loginMu          sync.Mutex
	csrfToken        string
	bootstrapCookies []string
}

type RemoteErrorKind string

const (
	RemoteErrorAuthentication RemoteErrorKind = "authentication_required"
	RemoteErrorSourceChanged  RemoteErrorKind = "source_changed"
	RemoteErrorUnavailable    RemoteErrorKind = "unavailable"
)

// RemoteError reports an upstream failure without retaining response bodies,
// request URLs, cookies, or account-specific details in persisted errors.
type RemoteError struct {
	Kind        RemoteErrorKind
	StatusCode  int
	ServiceCode int
}

func (e *RemoteError) Error() string {
	switch e.Kind {
	case RemoteErrorAuthentication:
		return "dedao authentication is required"
	case RemoteErrorSourceChanged:
		return "dedao ebook source is unavailable or changed"
	default:
		return "dedao service request failed"
	}
}

// CookieOptions dedao cookie options
type CookieOptions struct {
	GAT           string `json:"gat"`
	ISID          string `json:"isid"`
	Iget          string `json:"iget"`
	Token         string `json:"token"`
	CsrfToken     string `json:"csrfToken"`
	GuardDeviceID string `json:"_guard_device_id" mapstructure:"_guard_device_id"`
	SID           string `json:"_sid" mapstructure:"_sid"`
	AcwTc         string `json:"acw_tc" mapstructure:"acw_tc"`
	AliyungfTc    string `json:"aliyungf_tc"`
	CookieStr     string `json:"cookieStr"`
}

// NewService new service
func NewService(co *CookieOptions) *Service {
	var cookies []*http.Cookie
	if co.GAT != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "GAT",
			Value:  co.GAT,
			Domain: "." + dedaoCommURL.Host,
		})
	}

	if co.ISID != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "ISID",
			Value:  co.ISID,
			Domain: "." + dedaoCommURL.Host,
		})
	}

	if co.GuardDeviceID != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "_guard_device_id",
			Value:  co.GuardDeviceID,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.SID != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "_sid",
			Value:  co.SID,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.AcwTc != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "acw_tc",
			Value:  co.AcwTc,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.Iget != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "iget",
			Value:  co.Iget,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.Token != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "token",
			Value:  co.Token,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.CsrfToken != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "csrfToken",
			Value:  co.CsrfToken,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	if co.AliyungfTc != "" {
		cookies = append(cookies, &http.Cookie{
			Name:   "aliyungf_tc",
			Value:  co.AliyungfTc,
			Domain: "www." + dedaoCommURL.Host,
		})
	}

	client := resty.New()
	client.SetDebug(false)
	client.SetBaseURL(baseURL).
		SetCookies(cookies).
		SetHeaderVerbatim("User-Agent", UserAgent).
		SetHeaderVerbatim("Xi-DT", "web")

	if co.CsrfToken != "" {
		client.SetHeaderVerbatim("Xi-Csrf-Token", co.CsrfToken)
	}
	return &Service{client: client, csrfToken: co.CsrfToken}
}

// BootstrapCookies returns an isolated copy of cookies established while this
// service bootstrapped a QR login flow.
func (s *Service) BootstrapCookies() []string {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	return append([]string(nil), s.bootstrapCookies...)
}

func (r *Response) isSuccess() bool {
	return r.H.C == 0
}

func handleHTTPResponse(resp *resty.Response, err error) (io.ReadCloser, error) {
	if err != nil {
		return nil, err
	}

	statusCode := resp.StatusCode()
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		kind := RemoteErrorUnavailable
		switch statusCode {
		case http.StatusUnauthorized:
			kind = RemoteErrorAuthentication
		case http.StatusForbidden, http.StatusNotFound:
			kind = RemoteErrorSourceChanged
		}
		return nil, &RemoteError{Kind: kind, StatusCode: statusCode}
	}

	data := resp.Body()
	reader := bytes.NewReader(data)
	result := io.NopCloser(reader)
	return result, nil
}

func handleJSONParse(reader io.Reader, v interface{}) error {
	result := new(Response)

	err := utils.UnmarshalReader(reader, &result)
	if err != nil {
		return &RemoteError{Kind: RemoteErrorUnavailable}
	}
	// fmt.Printf("result.C:=%#v", result.C)
	if !result.isSuccess() {
		// 未登录或者登录凭证无效
		return remoteErrorForServiceResponse(result.H.C, result.H.E)
	}
	err = utils.UnmarshalJSON(result.C, v)
	if err != nil {
		return &RemoteError{Kind: RemoteErrorUnavailable, ServiceCode: result.H.C}
	}

	return nil
}

func remoteErrorForServiceResponse(code int, message string) error {
	kind := RemoteErrorUnavailable
	switch code {
	case http.StatusUnauthorized:
		kind = RemoteErrorAuthentication
	case http.StatusForbidden, http.StatusNotFound:
		kind = RemoteErrorSourceChanged
	default:
		normalized := strings.ToLower(message)
		if containsAny(normalized, "unauthorized", "authentication", "credential", "login", "token", "expired", "未登录", "登录", "认证", "凭证") {
			kind = RemoteErrorAuthentication
		} else if containsAny(normalized, "not found", "forbidden", "permission", "identity mismatch", "source changed", "不存在", "无权限", "无权", "不匹配", "已下架") {
			kind = RemoteErrorSourceChanged
		}
	}
	return &RemoteError{Kind: kind, ServiceCode: code}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

// ParseCookies parse cookie string to cookie options
func ParseCookies(cookie string, v interface{}) (err error) {
	if cookie == "" {
		return errors.New("cookie is empty")
	}
	list := strings.Split(cookie, ";")
	cookieM := make(map[string]string, len(list))
	for _, v := range list {
		item := strings.Split(v, "=")
		if len(item) > 1 {
			if item[1] != "" {
				cookieM[strings.TrimSpace(item[0])] = item[1]
			}
		}
	}
	err = mapstructure.Decode(cookieM, v)
	return
}

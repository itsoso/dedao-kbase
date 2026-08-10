package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

var service *Service

func TestMain(m *testing.M) {
	// cookie := utils.Get(baseURL)
	cookie := "aliyungf_tc=test-aliyungf; ISID=test-isid; csrfToken=test-csrf; token=test-token; _guard_device_id=test-device; _sid=test-sid; acw_tc=test-acw; GAT=test-gat; iget=test-iget"
	// cookie := ""
	co := &CookieOptions{}
	ParseCookies(cookie, &co)
	service = NewService(co)
	exitCode := m.Run()
	// 退出
	os.Exit(exitCode)
}

func TestGetLoginAccessToken(t *testing.T) {
	result, err := service.reqGetLoginAccessToken("")
	if err != nil {
		fmt.Printf("%#v \n", err)
	}
	fmt.Printf("%#v \n", result)
}

func TestGet(t *testing.T) {
	fmt.Println(dedaoCommURL.String())
}

func TestNewService(t *testing.T) {
	resp, err := service.client.R().Get("/api/pc/user/info")
	if err != nil {
		fmt.Printf("%#v \n", err)
	}
	var user User
	data := resp.Body()

	reader := bytes.NewReader(data)
	result := io.NopCloser(reader)
	handleJSONParse(result, &user)
	fmt.Println(user)
}

func TestToken(t *testing.T) {
	result, err := service.Token()
	if err != nil {
		fmt.Printf("%#v \n", err)
	}
	fmt.Printf("%#v \n", result)
}

func TestUser(t *testing.T) {
	result, err := service.User()
	if err != nil {
		fmt.Printf("%#v \n", err)
	}
	fmt.Printf("%#v \n", result)
}

func TestCourseType(t *testing.T) {
	result, err := service.CourseType()
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestCourseList(t *testing.T) {
	result, err := service.CourseList("bauhinia", "study", 1, 10)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestCourseInfo(t *testing.T) {
	ID := "OY8PNZj5EavJq1aHO9Jn1eqGDdlgw7"
	result, err := service.CourseInfo(ID)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestArticleList(t *testing.T) {
	ID := "OY8PNZj5EavJq1aHO9Jn1eqGDdlgw7"
	result, err := service.ArticleList(ID, "", 30, 30)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestAudioByAlias(t *testing.T) {
	ID := "zDWMvqA3d2k94LZ9KQ0RVjnxyapBePZ7"
	result, err := service.AudioByAlias(ID)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestArticleDetail(t *testing.T) {
	token := "KWn/CP3W2txbAhtG26cVSr0YwlF3n7LCqzYAOHpyWw3+ft2hqSH+BqlOZTnBur2gXU0ByFmUQmz0tYVxepbdpTy81Gk="
	sign := "b23a426b357d1b83"
	appID := "1632426125495894021"
	result, err := service.ArticleDetail(token, sign, appID)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestArticleInfo(t *testing.T) {
	enid := "R2Mo65zY4QZ3VnmvraKqEdNAa98jGB"
	result, err := service.ArticleInfo(enid, 1)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}
func TestEbookDetail(t *testing.T) {
	enid := "DLnMGAEG7gKLyYmkAbPaEXxD8BM4J0LMedWROrpdZn19VNzv2o5e6lqjQQ1poxqy"
	result, err := service.EbookDetail(enid)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestEbookDetailContextCancelsRequest(t *testing.T) {
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()

	contextService := NewService(&CookieOptions{})
	contextService.client.SetBaseURL(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := contextService.EbookDetailContext(ctx, "ebook-enid")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EbookDetailContext error=%v, want deadline exceeded", err)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("server request context was not canceled")
	}
}

func TestEbookRequestContextMethodsCancelInFlightRequests(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *Service) error
	}{
		{name: "detail", call: func(ctx context.Context, service *Service) error {
			_, err := service.EbookDetailContext(ctx, "ebook-enid")
			return err
		}},
		{name: "read-token", call: func(ctx context.Context, service *Service) error {
			_, err := service.EbookReadTokenContext(ctx, "ebook-enid")
			return err
		}},
		{name: "info", call: func(ctx context.Context, service *Service) error {
			_, err := service.EbookInfoContext(ctx, "read-token")
			return err
		}},
		{name: "pages", call: func(ctx context.Context, service *Service) error {
			_, err := service.EbookPagesContext(ctx, "chapter", "read-token", 0, 20, 0)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
			}))
			defer func() {
				close(release)
				server.Close()
			}()
			contextService := NewService(&CookieOptions{})
			contextService.client.SetBaseURL(server.URL)
			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() { errCh <- test.call(ctx, contextService) }()
			<-started
			cancel()
			if err := <-errCh; !errors.Is(err, context.Canceled) {
				t.Fatalf("request error=%v, want context canceled", err)
			}
		})
	}
}

func TestEbookDetailReturnsSafeTypedRemoteErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		kind       RemoteErrorKind
	}{
		{name: "http unauthorized", statusCode: http.StatusUnauthorized, body: `private token=auth-secret`, kind: RemoteErrorAuthentication},
		{name: "http no certificate", statusCode: 496, body: `private graphical verification path`, kind: RemoteErrorAuthentication},
		{name: "http forbidden", statusCode: http.StatusForbidden, body: `private account path`, kind: RemoteErrorSourceChanged},
		{name: "http missing", statusCode: http.StatusNotFound, body: `private book identity`, kind: RemoteErrorSourceChanged},
		{name: "http unavailable", statusCode: http.StatusInternalServerError, body: `private upstream trace`, kind: RemoteErrorUnavailable},
		{name: "service auth", statusCode: http.StatusOK, body: `{"h":{"c":401,"e":"login invalid token=auth-secret"},"c":{}}`, kind: RemoteErrorAuthentication},
		{name: "service expired credential", statusCode: http.StatusOK, body: `{"h":{"c":10000,"e":"credential expired private-token"},"c":{}}`, kind: RemoteErrorAuthentication},
		{name: "service identity mismatch", statusCode: http.StatusOK, body: `{"h":{"c":10001,"e":"book identity mismatch private-id"},"c":{}}`, kind: RemoteErrorSourceChanged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			contextService := NewService(&CookieOptions{})
			contextService.client.SetBaseURL(server.URL)
			_, err := contextService.EbookDetailContext(context.Background(), "ebook-enid")
			var remoteErr *RemoteError
			if !errors.As(err, &remoteErr) || remoteErr.Kind != test.kind {
				t.Fatalf("error=%#v, want RemoteError kind %q", err, test.kind)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "auth-secret") {
				t.Fatalf("safe error leaked response body: %q", err)
			}
		})
	}
}

func TestEbookReadToken(t *testing.T) {
	enid := "DLnMGAEG7gKLyYmkAbPaEXxD8BM4J0LMedWROrpdZn19VNzv2o5e6lqjQQ1poxqy"
	result, err := service.EbookReadToken(enid)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestEbookVIPInfo(t *testing.T) {
	result, err := service.EbookUserInfo()
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestTopicAll(t *testing.T) {
	result, err := service.TopicAll(0, 10)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestTopicDetail(t *testing.T) {
	id := "4qpo7LxOynVXemeY6pALW1JXrlwG6E"
	result, err := service.TopicDetail(id)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestTopicNotesList(t *testing.T) {
	id := "4qpo7LxOynVXemeY6pALW1JXrlwG6E"
	result, err := service.TopicNotesList(id, true, 0, 20)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_GetHomeInitialState(t *testing.T) {
	result, err := service.GetHomeInitialState()
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}
func TestService_SearchHot(t *testing.T) {
	result, err := service.SearchHot()
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_SunflowerLabelList(t *testing.T) {
	result, err := service.SunflowerLabelList(2)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_SunflowerLabelContent(t *testing.T) {
	enID := "Da1LVnd9dA74k9nB2G6YZrL8eqENW5Q4eVPyaDO0RxV3lzK1vJjmboMXgregjymY"
	result, err := service.SunflowerLabelContent(enID, 4, 0, 4)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_SunflowerResourceList(t *testing.T) {
	result, err := service.SunflowerResourceList()
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_AlgoFilter(t *testing.T) {
	param := AlgoFilterParam{
		ClassfcName:  "心理学",
		LabelId:      "",
		NavType:      0,
		NavigationId: "",
		Page:         0,
		PageSize:     18,
		ProductTypes: "66",
		RequestId:    "",
		SortStrategy: "HOT",
		TagsIds:      nil,
	}
	result, err := service.AlgoFilter(param)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

func TestService_AlgoProduct(t *testing.T) {
	param := AlgoFilterParam{
		ClassfcName:  "心理学",
		LabelId:      "X9vmWzAl54WYrJ78ayq1VjKbDeZRxzpvnpXEBOlvko9L026gdm3AnGNMDkG1x8JR",
		NavType:      0,
		NavigationId: "rA8XdO46oA1E4kLX6gvl3MyJxzD7dWPGA6pemYR8B52Kbqj0GnrV9ZaNOVDJBZ5a",
		Page:         0,
		PageSize:     18,
		ProductTypes: "0",
		RequestId:    "",
		SortStrategy: "HOT",
		TagsIds:      nil,
	}
	result, err := service.AlgoProduct(param)
	if err != nil {
		fmt.Printf("err:=%#v \n", err)
	}
	fmt.Printf("result:=%v \n", result)
}

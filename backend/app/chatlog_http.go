package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultChatlogHTTPBaseURL         = "http://127.0.0.1:5030"
	chatlogHTTPMaxResponseBytes int64 = 4 << 20
	chatlogHTTPMaxRows                = 500
	chatlogHTTPMaxContextWindow       = 100
	chatlogHTTPMaxQueryRunes          = 512
	chatlogHTTPMaxContentRunes        = 200000
)

var (
	ErrChatlogUnavailable    = errors.New("chatlog service unavailable")
	ErrChatlogInvalidResult  = errors.New("invalid chatlog response")
	ErrChatlogUnsafeRedirect = errors.New("chatlog redirect is not allowed")
)

type ChatlogHTTPConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

type ChatlogQuery struct {
	Time    string
	Talker  string
	Sender  string
	Keyword string
	Limit   int
	Offset  int
}

type ChatlogReferencedMessage struct {
	Seq        int64     `json:"seq,omitempty"`
	Time       time.Time `json:"time,omitempty"`
	Sender     string    `json:"sender,omitempty"`
	SenderName string    `json:"sender_name,omitempty"`
	Type       int64     `json:"type"`
	SubType    int64     `json:"sub_type"`
	Content    string    `json:"content,omitempty"`
}

type ChatlogMessage struct {
	Seq        int64                     `json:"seq"`
	MessageRef string                    `json:"message_ref"`
	Time       time.Time                 `json:"time"`
	Talker     string                    `json:"talker"`
	TalkerName string                    `json:"talker_name,omitempty"`
	IsChatRoom bool                      `json:"is_chat_room"`
	Sender     string                    `json:"sender"`
	SenderName string                    `json:"sender_name,omitempty"`
	IsSelf     bool                      `json:"is_self"`
	Type       int64                     `json:"type"`
	SubType    int64                     `json:"sub_type"`
	Content    string                    `json:"content,omitempty"`
	Referred   *ChatlogReferencedMessage `json:"referred,omitempty"`
}

type ChatlogContact struct {
	UserName string `json:"user_name"`
	Alias    string `json:"alias,omitempty"`
	Remark   string `json:"remark,omitempty"`
	NickName string `json:"nick_name,omitempty"`
	IsFriend bool   `json:"is_friend"`
}

type ChatlogContactPage struct {
	Items []ChatlogContact `json:"items"`
}

type ChatlogRoomUser struct {
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name,omitempty"`
}

type ChatlogRoom struct {
	Name     string            `json:"name"`
	Owner    string            `json:"owner,omitempty"`
	Users    []ChatlogRoomUser `json:"users,omitempty"`
	Remark   string            `json:"remark,omitempty"`
	NickName string            `json:"nick_name,omitempty"`
}

type ChatlogRoomPage struct {
	Items []ChatlogRoom `json:"items"`
}

type ChatlogSession struct {
	UserName string    `json:"user_name"`
	NOrder   int       `json:"n_order"`
	NickName string    `json:"nick_name,omitempty"`
	Content  string    `json:"content,omitempty"`
	NTime    time.Time `json:"n_time"`
}

type ChatlogSessionPage struct {
	Items []ChatlogSession `json:"items"`
}

type ChatlogContextBundle struct {
	Match    ChatlogMessage   `json:"match"`
	Messages []ChatlogMessage `json:"messages"`
}

type ChatlogReader interface {
	SearchMessages(context.Context, ChatlogQuery) ([]ChatlogMessage, error)
	ListContacts(context.Context, string, int, int) (ChatlogContactPage, error)
	ListChatRooms(context.Context, string, int, int) (ChatlogRoomPage, error)
	ListSessions(context.Context, string, int, int) (ChatlogSessionPage, error)
}

type ChatlogHTTPReader struct {
	baseURL *url.URL
	client  *http.Client
}

func NewChatlogHTTPReader(config ChatlogHTTPConfig) (*ChatlogHTTPReader, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultChatlogHTTPBaseURL
	}
	parsed, err := parseSourceAgentBaseURL(baseURL)
	if err != nil || !isExactLoopbackSourceAgentHost(parsed.Hostname()) {
		return nil, fmt.Errorf("CHATLOG_BASE_URL must be an absolute loopback HTTP(S) URL")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 || clientCopy.Timeout > 30*time.Second {
		clientCopy.Timeout = 10 * time.Second
	}
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return ErrChatlogUnsafeRedirect
	}
	return &ChatlogHTTPReader{baseURL: parsed, client: &clientCopy}, nil
}

func (c *ChatlogHTTPReader) SearchMessages(ctx context.Context, query ChatlogQuery) ([]ChatlogMessage, error) {
	if err := validateChatlogQuery(query); err != nil {
		return nil, err
	}
	values := url.Values{"format": {"json"}, "time": {strings.TrimSpace(query.Time)},
		"talker": {strings.TrimSpace(query.Talker)}, "limit": {strconv.Itoa(query.Limit)}, "offset": {strconv.Itoa(query.Offset)}}
	if value := strings.TrimSpace(query.Sender); value != "" {
		values.Set("sender", value)
	}
	if value := strings.TrimSpace(query.Keyword); value != "" {
		values.Set("keyword", value)
	}
	var raw []chatlogV0015Message
	if err := c.getJSON(ctx, "/api/v1/chatlog", values, &raw); err != nil {
		return nil, err
	}
	if len(raw) > query.Limit || len(raw) > chatlogHTTPMaxRows {
		return nil, ErrChatlogInvalidResult
	}
	messages := make([]ChatlogMessage, 0, len(raw))
	for _, item := range raw {
		message, err := normalizeChatlogV0015Message(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].Seq < messages[j].Seq })
	return messages, nil
}

func (c *ChatlogHTTPReader) SearchMessagesWithContext(ctx context.Context, query ChatlogQuery, before, after int) ([]ChatlogContextBundle, error) {
	if before < 0 || after < 0 || before > chatlogHTTPMaxContextWindow || after > chatlogHTTPMaxContextWindow {
		return nil, fmt.Errorf("context window is outside supported bounds")
	}
	matches, err := c.SearchMessages(ctx, query)
	if err != nil {
		return nil, err
	}
	bundles := make([]ChatlogContextBundle, 0, len(matches))
	for _, match := range matches {
		contextMessages, err := c.SearchMessages(ctx, ChatlogQuery{
			Time: match.Time.Format("2006-01-02"), Talker: match.Talker, Limit: chatlogHTTPMaxRows,
		})
		if err != nil {
			return nil, err
		}
		index := sort.Search(len(contextMessages), func(index int) bool { return contextMessages[index].Seq >= match.Seq })
		if index >= len(contextMessages) || contextMessages[index].Seq != match.Seq {
			return nil, ErrChatlogInvalidResult
		}
		start, end := index-before, index+after+1
		if start < 0 {
			start = 0
		}
		if end > len(contextMessages) {
			end = len(contextMessages)
		}
		bundles = append(bundles, ChatlogContextBundle{Match: match, Messages: append([]ChatlogMessage(nil), contextMessages[start:end]...)})
	}
	return bundles, nil
}

func (c *ChatlogHTTPReader) ListContacts(ctx context.Context, keyword string, limit, offset int) (ChatlogContactPage, error) {
	if err := validateChatlogListQuery(keyword, limit, offset); err != nil {
		return ChatlogContactPage{}, err
	}
	var raw struct {
		Items []struct {
			UserName string `json:"userName"`
			Alias    string `json:"alias"`
			Remark   string `json:"remark"`
			NickName string `json:"nickName"`
			IsFriend bool   `json:"isFriend"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/v1/contact", chatlogListValues(keyword, limit, offset), &raw); err != nil {
		return ChatlogContactPage{}, err
	}
	if len(raw.Items) > limit {
		return ChatlogContactPage{}, ErrChatlogInvalidResult
	}
	page := ChatlogContactPage{Items: make([]ChatlogContact, 0, len(raw.Items))}
	for _, item := range raw.Items {
		if err := validateChatlogTextFields(item.UserName, item.Alias, item.Remark, item.NickName); err != nil || strings.TrimSpace(item.UserName) == "" {
			return ChatlogContactPage{}, ErrChatlogInvalidResult
		}
		page.Items = append(page.Items, ChatlogContact(item))
	}
	return page, nil
}

func (c *ChatlogHTTPReader) ListChatRooms(ctx context.Context, keyword string, limit, offset int) (ChatlogRoomPage, error) {
	if err := validateChatlogListQuery(keyword, limit, offset); err != nil {
		return ChatlogRoomPage{}, err
	}
	var raw struct {
		Items []struct {
			Name  string `json:"name"`
			Owner string `json:"owner"`
			Users []struct {
				UserName    string `json:"userName"`
				DisplayName string `json:"displayName"`
			} `json:"users"`
			Remark   string `json:"remark"`
			NickName string `json:"nickName"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/v1/chatroom", chatlogListValues(keyword, limit, offset), &raw); err != nil {
		return ChatlogRoomPage{}, err
	}
	if len(raw.Items) > limit {
		return ChatlogRoomPage{}, ErrChatlogInvalidResult
	}
	page := ChatlogRoomPage{Items: make([]ChatlogRoom, 0, len(raw.Items))}
	for _, item := range raw.Items {
		if err := validateChatlogTextFields(item.Name, item.Owner, item.Remark, item.NickName); err != nil || strings.TrimSpace(item.Name) == "" || len(item.Users) > chatlogHTTPMaxRows {
			return ChatlogRoomPage{}, ErrChatlogInvalidResult
		}
		room := ChatlogRoom{Name: item.Name, Owner: item.Owner, Remark: item.Remark, NickName: item.NickName, Users: make([]ChatlogRoomUser, 0, len(item.Users))}
		for _, user := range item.Users {
			if err := validateChatlogTextFields(user.UserName, user.DisplayName); err != nil || strings.TrimSpace(user.UserName) == "" {
				return ChatlogRoomPage{}, ErrChatlogInvalidResult
			}
			room.Users = append(room.Users, ChatlogRoomUser{UserName: user.UserName, DisplayName: user.DisplayName})
		}
		page.Items = append(page.Items, room)
	}
	return page, nil
}

func (c *ChatlogHTTPReader) ListSessions(ctx context.Context, keyword string, limit, offset int) (ChatlogSessionPage, error) {
	if err := validateChatlogListQuery(keyword, limit, offset); err != nil {
		return ChatlogSessionPage{}, err
	}
	var raw struct {
		Items []struct {
			UserName string    `json:"userName"`
			NOrder   int       `json:"nOrder"`
			NickName string    `json:"nickName"`
			Content  string    `json:"content"`
			NTime    time.Time `json:"nTime"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/v1/session", chatlogListValues(keyword, limit, offset), &raw); err != nil {
		return ChatlogSessionPage{}, err
	}
	if len(raw.Items) > limit {
		return ChatlogSessionPage{}, ErrChatlogInvalidResult
	}
	page := ChatlogSessionPage{Items: make([]ChatlogSession, 0, len(raw.Items))}
	for _, item := range raw.Items {
		if err := validateChatlogTextFields(item.UserName, item.NickName, item.Content); err != nil || strings.TrimSpace(item.UserName) == "" || item.NTime.IsZero() {
			return ChatlogSessionPage{}, ErrChatlogInvalidResult
		}
		page.Items = append(page.Items, ChatlogSession(item))
	}
	return page, nil
}

func (c *ChatlogHTTPReader) getJSON(ctx context.Context, requestPath string, values url.Values, target any) error {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + requestPath
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		if errors.Is(err, ErrChatlogUnsafeRedirect) {
			return ErrChatlogUnsafeRedirect
		}
		return ErrChatlogUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode >= 500 {
			return ErrChatlogUnavailable
		}
		return ErrChatlogInvalidResult
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, chatlogHTTPMaxResponseBytes+1))
	if err != nil {
		return ErrChatlogUnavailable
	}
	if int64(len(body)) > chatlogHTTPMaxResponseBytes {
		return ErrChatlogInvalidResult
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return ErrChatlogInvalidResult
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrChatlogInvalidResult
	}
	return nil
}

type chatlogV0015Message struct {
	Seq        int64     `json:"seq"`
	Time       time.Time `json:"time"`
	Talker     string    `json:"talker"`
	TalkerName string    `json:"talkerName"`
	IsChatRoom bool      `json:"isChatRoom"`
	Sender     string    `json:"sender"`
	SenderName string    `json:"senderName"`
	IsSelf     bool      `json:"isSelf"`
	Type       int64     `json:"type"`
	SubType    int64     `json:"subType"`
	Content    string    `json:"content"`
	Contents   struct {
		Refer *struct {
			Seq        int64     `json:"seq"`
			Time       time.Time `json:"time"`
			Sender     string    `json:"sender"`
			SenderName string    `json:"senderName"`
			Type       int64     `json:"type"`
			SubType    int64     `json:"subType"`
			Content    string    `json:"content"`
		} `json:"refer"`
	} `json:"contents"`
}

func normalizeChatlogV0015Message(raw chatlogV0015Message) (ChatlogMessage, error) {
	if raw.Seq <= 0 || raw.Time.IsZero() || strings.TrimSpace(raw.Talker) == "" ||
		validateChatlogTextFields(raw.Talker, raw.TalkerName, raw.Sender, raw.SenderName, raw.Content) != nil {
		return ChatlogMessage{}, ErrChatlogInvalidResult
	}
	message := ChatlogMessage{
		Seq: raw.Seq, MessageRef: strconv.FormatInt(raw.Seq, 10), Time: raw.Time, Talker: raw.Talker,
		TalkerName: raw.TalkerName, IsChatRoom: raw.IsChatRoom, Sender: raw.Sender, SenderName: raw.SenderName,
		IsSelf: raw.IsSelf, Type: raw.Type, SubType: raw.SubType, Content: raw.Content,
	}
	if raw.Contents.Refer != nil {
		referred := raw.Contents.Refer
		if err := validateChatlogTextFields(referred.Sender, referred.SenderName, referred.Content); err != nil {
			return ChatlogMessage{}, ErrChatlogInvalidResult
		}
		message.Referred = &ChatlogReferencedMessage{
			Seq: referred.Seq, Time: referred.Time, Sender: referred.Sender, SenderName: referred.SenderName,
			Type: referred.Type, SubType: referred.SubType, Content: referred.Content,
		}
	}
	return message, nil
}

func validateChatlogTextFields(values ...string) error {
	for _, value := range values {
		if len([]rune(value)) > chatlogHTTPMaxContentRunes || strings.ContainsRune(value, '\x00') {
			return ErrChatlogInvalidResult
		}
	}
	return nil
}

func validateChatlogQuery(query ChatlogQuery) error {
	if query.Limit <= 0 || query.Limit > chatlogHTTPMaxRows || query.Offset < 0 {
		return fmt.Errorf("chatlog limit must be between 1 and %d and offset must be non-negative", chatlogHTTPMaxRows)
	}
	if strings.TrimSpace(query.Talker) == "" {
		return fmt.Errorf("chatlog talker is required")
	}
	if err := validateChatlogTextFields(query.Time, query.Talker, query.Sender, query.Keyword); err != nil {
		return err
	}
	for _, value := range []string{query.Time, query.Talker, query.Sender, query.Keyword} {
		if len([]rune(value)) > chatlogHTTPMaxQueryRunes {
			return fmt.Errorf("chatlog query exceeds supported bounds")
		}
	}
	parts := strings.Split(strings.TrimSpace(query.Time), "~")
	if len(parts) < 1 || len(parts) > 2 {
		return fmt.Errorf("chatlog time must be YYYY-MM-DD or YYYY-MM-DD~YYYY-MM-DD")
	}
	var parsed []time.Time
	for _, part := range parts {
		value, err := time.Parse("2006-01-02", part)
		if err != nil {
			return fmt.Errorf("chatlog time must be YYYY-MM-DD or YYYY-MM-DD~YYYY-MM-DD")
		}
		parsed = append(parsed, value)
	}
	if len(parsed) == 2 && parsed[1].Before(parsed[0]) {
		return fmt.Errorf("chatlog time range is reversed")
	}
	return nil
}

func validateChatlogListQuery(keyword string, limit, offset int) error {
	if limit <= 0 || limit > chatlogHTTPMaxRows || offset < 0 || len([]rune(keyword)) > chatlogHTTPMaxQueryRunes {
		return fmt.Errorf("chatlog list query is outside supported bounds")
	}
	return nil
}

func chatlogListValues(keyword string, limit, offset int) url.Values {
	values := url.Values{"format": {"json"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		values.Set("keyword", keyword)
	}
	return values
}

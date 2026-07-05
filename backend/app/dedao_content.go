package app

import (
	"errors"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/yann0917/dedao-gui/backend/services"
)

type DedaoEbook struct {
	Enid       string `json:"enid"`
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author,omitempty"`
	Intro      string `json:"intro,omitempty"`
	Icon       string `json:"icon,omitempty"`
	Price      string `json:"price,omitempty"`
	Progress   int    `json:"progress"`
	PublishNum int    `json:"publish_num,omitempty"`
	LastRead   string `json:"last_read,omitempty"`
	IsBuy      bool   `json:"is_buy"`
	CanTrial   bool   `json:"can_trial_read,omitempty"`
}

type DedaoEbookPage struct {
	Ebooks     []DedaoEbook `json:"ebooks"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
	Total      int          `json:"total"`
	TotalPages int          `json:"total_pages"`
	IsMore     int          `json:"is_more"`
}

type DedaoCourse struct {
	Enid       string `json:"enid"`
	ID         int    `json:"id"`
	ClassID    int    `json:"class_id"`
	Title      string `json:"title"`
	Intro      string `json:"intro,omitempty"`
	Author     string `json:"author,omitempty"`
	Icon       string `json:"icon,omitempty"`
	Price      string `json:"price,omitempty"`
	Progress   int    `json:"progress"`
	PublishNum int    `json:"publish_num,omitempty"`
	CourseNum  int    `json:"course_num,omitempty"`
	LastRead   string `json:"last_read,omitempty"`
}

type DedaoCoursePage struct {
	Courses    []DedaoCourse `json:"courses"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	Total      int           `json:"total"`
	TotalPages int           `json:"total_pages"`
	IsMore     int           `json:"is_more"`
}

type DedaoTopic struct {
	TopicIDHazy string `json:"topic_id_hazy"`
	Name        string `json:"name"`
	Intro       string `json:"intro,omitempty"`
	Img         string `json:"img,omitempty"`
	Tag         int    `json:"tag,omitempty"`
	ViewCount   int    `json:"view_count,omitempty"`
	NotesCount  int    `json:"notes_count,omitempty"`
	HasNewNotes bool   `json:"has_new_notes"`
}

type DedaoTopicPage struct {
	Topics   []DedaoTopic `json:"topics"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	HasMore  bool         `json:"has_more"`
}

type DedaoTopicNote struct {
	NoteIDHazy   string   `json:"note_id_hazy"`
	AuthorName   string   `json:"author_name,omitempty"`
	Avatar       string   `json:"avatar,omitempty"`
	TimeDesc     string   `json:"time_desc,omitempty"`
	NoteTitle    string   `json:"note_title,omitempty"`
	Note         string   `json:"note"`
	Slogan       string   `json:"slogan,omitempty"`
	VInfo        string   `json:"v_info,omitempty"`
	TopicName    string   `json:"topic_name,omitempty"`
	BaseTitle    string   `json:"base_title,omitempty"`
	BaseSubTitle string   `json:"base_sub_title,omitempty"`
	BaseImg      string   `json:"base_img,omitempty"`
	Images       []string `json:"images,omitempty"`
	RepostCount  int      `json:"repost_count,omitempty"`
	CommentCount int      `json:"comment_count,omitempty"`
	LikeCount    int      `json:"like_count,omitempty"`
}

type DedaoTopicNotePage struct {
	TopicIDHazy string           `json:"topic_id_hazy"`
	Notes       []DedaoTopicNote `json:"notes"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
	HasMore     bool             `json:"has_more"`
	IsElected   bool             `json:"is_elected"`
}

type DedaoOdob struct {
	Enid          string `json:"enid"`
	ID            int    `json:"id"`
	ClassID       int    `json:"class_id,omitempty"`
	Title         string `json:"title"`
	Intro         string `json:"intro,omitempty"`
	Author        string `json:"author,omitempty"`
	Icon          string `json:"icon,omitempty"`
	Price         string `json:"price,omitempty"`
	Progress      int    `json:"progress"`
	Duration      int    `json:"duration,omitempty"`
	PublishNum    int    `json:"publish_num,omitempty"`
	LastRead      string `json:"last_read,omitempty"`
	AudioAliasID  string `json:"audio_alias_id,omitempty"`
	AudioTitle    string `json:"audio_title,omitempty"`
	AudioIcon     string `json:"audio_icon,omitempty"`
	AudioDuration int    `json:"audio_duration,omitempty"`
	AudioPlayURL  string `json:"audio_play_url,omitempty"`
	HasPlayAuth   bool   `json:"has_play_auth"`
}

type DedaoOdobPage struct {
	Odobs      []DedaoOdob `json:"odobs"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	Total      int         `json:"total"`
	TotalPages int         `json:"total_pages"`
	IsMore     int         `json:"is_more"`
}

type DedaoOdobAgency struct {
	Name           string `json:"name,omitempty"`
	Intro          string `json:"intro,omitempty"`
	MemberName     string `json:"member_name,omitempty"`
	MemberAvatar   string `json:"member_avatar,omitempty"`
	BookCount      int    `json:"book_count,omitempty"`
	UserVisitCount int    `json:"user_visit_count,omitempty"`
}

type DedaoOdobTopicSummary struct {
	Title    string `json:"title"`
	SubTitle string `json:"sub_title,omitempty"`
}

type DedaoOdobDetail struct {
	Enid           string                  `json:"enid"`
	ID             int                     `json:"id"`
	Title          string                  `json:"title"`
	Icon           string                  `json:"icon,omitempty"`
	Duration       int                     `json:"duration,omitempty"`
	AudioPrice     string                  `json:"audio_price,omitempty"`
	AudioSummary   string                  `json:"audio_summary,omitempty"`
	PublishTime    int                     `json:"publish_time,omitempty"`
	IsVIP          bool                    `json:"is_vip"`
	IsBuy          bool                    `json:"is_buy"`
	InBookrack     bool                    `json:"in_bookrack"`
	Progress       int                     `json:"progress,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	LearnCountDesc string                  `json:"learn_count_desc,omitempty"`
	Agency         DedaoOdobAgency         `json:"agency,omitempty"`
	TopicSummary   []DedaoOdobTopicSummary `json:"topic_summary,omitempty"`
}

type DedaoCourseDetailMeta struct {
	Enid           string `json:"enid"`
	ID             int    `json:"id"`
	IDStr          string `json:"id_str,omitempty"`
	Title          string `json:"title"`
	Intro          string `json:"intro,omitempty"`
	Highlight      string `json:"highlight,omitempty"`
	LecturerName   string `json:"lecturer_name,omitempty"`
	LecturerTitle  string `json:"lecturer_title,omitempty"`
	LecturerIntro  string `json:"lecturer_intro,omitempty"`
	LecturerAvatar string `json:"lecturer_avatar,omitempty"`
	Logo           string `json:"logo,omitempty"`
	IndexImg       string `json:"index_img,omitempty"`
	ArticleCount   int    `json:"article_count,omitempty"`
	LearnUserCount int    `json:"learn_user_count,omitempty"`
	PriceDesc      string `json:"price_desc,omitempty"`
	IsSubscribe    bool   `json:"is_subscribe"`
}

type DedaoCourseDetail struct {
	Course   DedaoCourseDetailMeta `json:"course"`
	Articles []DedaoArticle        `json:"articles"`
	HasMore  bool                  `json:"has_more"`
}

type DedaoArticle struct {
	Enid        string `json:"enid"`
	ID          int    `json:"id"`
	IDStr       string `json:"id_str,omitempty"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Logo        string `json:"logo,omitempty"`
	PublishTime int    `json:"publish_time,omitempty"`
	IsRead      bool   `json:"is_read"`
	IsFreeTry   bool   `json:"is_free_try"`
	OrderNum    int    `json:"order_num,omitempty"`
	HasAudio    bool   `json:"has_audio"`
	HasVideo    bool   `json:"has_video"`
}

type DedaoArticlePage struct {
	Articles     []DedaoArticle `json:"articles"`
	Count        int            `json:"count"`
	MaxID        int            `json:"max_id"`
	LoadedCount  int            `json:"loaded_count"`
	ArticleCount int            `json:"article_count,omitempty"`
	NextCursor   int            `json:"next_cursor,omitempty"`
	IsMore       bool           `json:"is_more"`
}

type DedaoArticleMarkdown struct {
	Enid     string `json:"enid"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Markdown string `json:"markdown"`
}

type DedaoEbookCatalogItem struct {
	Level     int    `json:"level"`
	Text      string `json:"text"`
	Href      string `json:"href,omitempty"`
	ChapterID string `json:"chapter_id,omitempty"`
	PlayOrder int    `json:"play_order,omitempty"`
}

type DedaoEbookDetail struct {
	Enid           string                  `json:"enid"`
	ID             int                     `json:"id"`
	Title          string                  `json:"title"`
	OperatingTitle string                  `json:"operating_title,omitempty"`
	Cover          string                  `json:"cover,omitempty"`
	Count          int                     `json:"count,omitempty"`
	Price          string                  `json:"price,omitempty"`
	AuthorInfo     string                  `json:"author_info,omitempty"`
	BookAuthor     string                  `json:"book_author,omitempty"`
	PublishTime    string                  `json:"publish_time,omitempty"`
	BookIntro      string                  `json:"book_intro,omitempty"`
	AuthorList     []string                `json:"author_list,omitempty"`
	PressName      string                  `json:"press_name,omitempty"`
	PressBrief     string                  `json:"press_brief,omitempty"`
	ClassifyName   string                  `json:"classify_name,omitempty"`
	ProductScore   string                  `json:"product_score,omitempty"`
	DoubanScore    string                  `json:"douban_score,omitempty"`
	ReadTime       int                     `json:"read_time,omitempty"`
	IsBuy          bool                    `json:"is_buy"`
	IsOnBookshelf  bool                    `json:"is_on_bookshelf"`
	CanTrialRead   bool                    `json:"can_trial_read"`
	Catalog        []DedaoEbookCatalogItem `json:"catalog"`
}

type DedaoEbookPageSVG struct {
	PageNum     int    `json:"page_num"`
	BeginOffset int64  `json:"begin_offset"`
	EndOffset   int64  `json:"end_offset"`
	IsFirst     bool   `json:"is_first"`
	IsLast      bool   `json:"is_last"`
	SVG         string `json:"svg"`
}

type DedaoEbookChapterPages struct {
	Enid      string              `json:"enid"`
	ChapterID string              `json:"chapter_id"`
	Index     int                 `json:"index"`
	Count     int                 `json:"count"`
	Offset    int                 `json:"offset"`
	IsEnd     bool                `json:"is_end"`
	Pages     []DedaoEbookPageSVG `json:"pages"`
}

type DedaoContentProvider interface {
	ListEbooks(query string, page, pageSize int) (DedaoEbookPage, error)
	SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error)
	ListCourses(query string, page, pageSize int) (DedaoCoursePage, error)
	ListCoursesByCategory(category string, query string, page, pageSize int) (DedaoCoursePage, error)
	ListTopics(page, pageSize int) (DedaoTopicPage, error)
	ListTopicNotes(topicID string, isElected bool, page, pageSize int) (DedaoTopicNotePage, error)
	ListOdobs(query string, page, pageSize int) (DedaoOdobPage, error)
	GetOdobDetail(enid string) (DedaoOdobDetail, error)
	GetCourseDetail(enid string) (DedaoCourseDetail, error)
	ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error)
	GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error)
	GetOdobArticleMarkdown(enid string) (DedaoArticleMarkdown, error)
	GetEbookDetail(enid string) (DedaoEbookDetail, error)
	AddEbookToBookshelf(enid string) (DedaoEbook, error)
	GetEbookChapterPages(enid string, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error)
}

type liveDedaoContentProvider struct{}

func defaultDedaoContentProvider(provider DedaoContentProvider) DedaoContentProvider {
	if provider != nil {
		return provider
	}
	return liveDedaoContentProvider{}
}

func (p liveDedaoContentProvider) ListEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	query = strings.TrimSpace(query)
	if query != "" {
		list, err := getService().CourseListAll(CateEbook, "study")
		if err != nil {
			return DedaoEbookPage{}, err
		}
		courses := filterDedaoCourses(list.List, query)
		return dedaoEbookPageFromAllCourses(courses, page, pageSize), nil
	}

	list, err := CourseList(CateEbook, "study", page, pageSize)
	if err != nil {
		return DedaoEbookPage{}, err
	}
	total := dedaoCourseCategoryCount(CateEbook)
	ebooks := []services.Course{}
	isMore := 0
	if list != nil {
		ebooks = list.List
		isMore = list.ISMore
	}
	return dedaoEbookPageFromPagedCourses(ebooks, page, pageSize, total, isMore), nil
}

func (p liveDedaoContentProvider) SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return DedaoEbookPage{
			Ebooks:   []DedaoEbook{},
			Page:     page,
			PageSize: pageSize,
		}, nil
	}
	result, err := getService().SearchEbooks(query, page, pageSize)
	if err != nil {
		return DedaoEbookPage{}, err
	}
	return dedaoEbookPageFromSiteSearch(result, page, pageSize), nil
}

func (p liveDedaoContentProvider) ListCourses(query string, page, pageSize int) (DedaoCoursePage, error) {
	return p.ListCoursesByCategory(CateCourse, query, page, pageSize)
}

func (p liveDedaoContentProvider) ListCoursesByCategory(category string, query string, page, pageSize int) (DedaoCoursePage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = CateCourse
	}
	query = strings.TrimSpace(query)
	if query != "" {
		list, err := getService().CourseListAll(category, "study")
		if err != nil {
			return DedaoCoursePage{}, err
		}
		courses := filterDedaoCourses(list.List, query)
		return dedaoCoursePageFromAllCourses(courses, page, pageSize), nil
	}

	list, err := CourseList(category, "study", page, pageSize)
	if err != nil {
		return DedaoCoursePage{}, err
	}
	total := dedaoCourseCategoryCount(category)
	courses := []services.Course{}
	isMore := 0
	if list != nil {
		courses = list.List
		isMore = list.ISMore
	}
	return dedaoCoursePageFromPagedCourses(courses, page, pageSize, total, isMore), nil
}

func (p liveDedaoContentProvider) ListTopics(page, pageSize int) (DedaoTopicPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	apiPage := page - 1
	if apiPage < 0 {
		apiPage = 0
	}
	list, err := TopicAll(apiPage, pageSize)
	if err != nil {
		return DedaoTopicPage{}, err
	}
	return dedaoTopicPageFromService(list, page, pageSize), nil
}

func (p liveDedaoContentProvider) ListTopicNotes(topicID string, isElected bool, page, pageSize int) (DedaoTopicNotePage, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return DedaoTopicNotePage{}, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	apiPage := page - 1
	if apiPage < 0 {
		apiPage = 0
	}
	list, err := TopicNotesList(topicID, isElected, apiPage, pageSize)
	if err != nil {
		return DedaoTopicNotePage{}, err
	}
	return dedaoTopicNotePageFromService(topicID, list, page, pageSize, isElected), nil
}

func (p liveDedaoContentProvider) ListOdobs(query string, page, pageSize int) (DedaoOdobPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	query = strings.TrimSpace(query)
	if query != "" {
		list, err := getService().CourseListAll(CateAudioBook, "study")
		if err != nil {
			return DedaoOdobPage{}, err
		}
		courses := filterDedaoCourses(list.List, query)
		return dedaoOdobPageFromAllCourses(courses, page, pageSize), nil
	}

	list, err := CourseList(CateAudioBook, "study", page, pageSize)
	if err != nil {
		return DedaoOdobPage{}, err
	}
	total := dedaoCourseCategoryCount(CateAudioBook)
	courses := []services.Course{}
	isMore := 0
	if list != nil {
		courses = list.List
		isMore = list.ISMore
	}
	return dedaoOdobPageFromPagedCourses(courses, page, pageSize, total, isMore), nil
}

func (p liveDedaoContentProvider) GetOdobDetail(enid string) (DedaoOdobDetail, error) {
	detail, err := AudioDetail(enid)
	if err != nil {
		return DedaoOdobDetail{}, err
	}
	return dedaoOdobDetailFromService(enid, detail), nil
}

func (p liveDedaoContentProvider) GetCourseDetail(enid string) (DedaoCourseDetail, error) {
	info, err := CourseInfoByEnid(enid)
	if err != nil {
		return DedaoCourseDetail{}, err
	}
	detail := DedaoCourseDetail{
		Course:   dedaoCourseDetailMetaFromInfo(info),
		Articles: dedaoArticlesFromBases(info.FlatArticleList),
		HasMore:  info.HasMoreFlatArticleList,
	}
	if len(detail.Articles) == 0 {
		page, err := p.ListCourseArticles(enid, 30, 0)
		if err != nil {
			return DedaoCourseDetail{}, err
		}
		detail.Articles = page.Articles
		detail.HasMore = page.IsMore
	}
	return detail, nil
}

func (p liveDedaoContentProvider) ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error) {
	list, err := ArticleList(enid, "", count, maxID)
	if err != nil {
		return DedaoArticlePage{}, err
	}
	articleCount := p.courseArticleCount(enid)
	if list == nil {
		return DedaoArticlePage{
			Count:        count,
			MaxID:        maxID,
			NextCursor:   maxID,
			ArticleCount: articleCount,
		}, nil
	}
	articles := dedaoArticlesFromIntros(list.List)
	return DedaoArticlePage{
		Articles:     articles,
		Count:        count,
		MaxID:        list.MaxID,
		LoadedCount:  len(articles),
		ArticleCount: articleCount,
		NextCursor:   list.MaxID,
		IsMore:       count > 0 && len(articles) >= count && list.MaxID != 0,
	}, nil
}

func (p liveDedaoContentProvider) courseArticleCount(enid string) int {
	info, err := CourseInfoByEnid(enid)
	if err != nil || info == nil {
		return 0
	}
	return info.ClassInfo.CurrentArticleCount
}

func (p liveDedaoContentProvider) GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	info, err := getService().ArticleInfo(enid, 1)
	if err != nil {
		return DedaoArticleMarkdown{}, err
	}
	detail, err := getService().ArticleDetail(info.DdArticleToken, enid, "1632426125495894021")
	if err != nil {
		return DedaoArticleMarkdown{}, err
	}
	var content []services.Content
	if err := jsoniter.UnmarshalFromString(detail.Content, &content); err != nil {
		return DedaoArticleMarkdown{}, err
	}
	title := info.ArticleTitle
	if title == "" {
		title = info.ArticleInfo.Title
	}
	return DedaoArticleMarkdown{
		Enid:     enid,
		Type:     "course",
		Title:    title,
		Markdown: ContentsToMarkdown(content),
	}, nil
}

func (p liveDedaoContentProvider) GetOdobArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	detail, err := OdobArticleDetail(enid)
	if err != nil {
		return DedaoArticleMarkdown{}, err
	}
	var content []services.Content
	if err := jsoniter.UnmarshalFromString(detail.Content, &content); err != nil {
		return DedaoArticleMarkdown{}, err
	}
	return DedaoArticleMarkdown{
		Enid:     enid,
		Type:     "odob",
		Title:    "",
		Markdown: ContentsToMarkdown(content),
	}, nil
}

func (p liveDedaoContentProvider) GetEbookDetail(enid string) (DedaoEbookDetail, error) {
	detail, err := EbookDetail(enid)
	if err != nil {
		return DedaoEbookDetail{}, err
	}
	return dedaoEbookDetailFromService(detail), nil
}

func (p liveDedaoContentProvider) AddEbookToBookshelf(enid string) (DedaoEbook, error) {
	enid = strings.TrimSpace(enid)
	if enid == "" {
		return DedaoEbook{}, errors.New("ebook enid is required")
	}
	if _, err := EbookShelfAdd([]string{enid}); err != nil {
		return DedaoEbook{}, err
	}
	detail, err := EbookDetail(enid)
	if err != nil {
		return DedaoEbook{}, err
	}
	ebook := dedaoEbookFromServiceDetail(detail)
	if ebook.Enid == "" {
		ebook.Enid = enid
	}
	ebook.IsBuy = true
	return ebook, nil
}

func (p liveDedaoContentProvider) GetEbookChapterPages(enid string, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error) {
	token, err := getService().EbookReadToken(enid)
	if err != nil {
		return DedaoEbookChapterPages{}, err
	}
	pageList, err := getService().EbookReaderPages(chapterID, token.Token, index, count, offset)
	if err != nil {
		return DedaoEbookChapterPages{}, err
	}
	result := DedaoEbookChapterPages{
		Enid:      enid,
		ChapterID: chapterID,
		Index:     index,
		Count:     count,
		Offset:    offset,
	}
	if pageList == nil {
		return result, nil
	}
	result.IsEnd = pageList.IsEnd
	result.Pages = make([]DedaoEbookPageSVG, 0, len(pageList.Pages))
	for i, page := range pageList.Pages {
		result.Pages = append(result.Pages, DedaoEbookPageSVG{
			PageNum:     index + i + 1,
			BeginOffset: page.BeginOffset,
			EndOffset:   page.EndOffset,
			IsFirst:     page.IsFirst,
			IsLast:      page.IsLast,
			SVG:         DecryptAES(page.Svg),
		})
	}
	return result, nil
}

func dedaoCourseCategoryCount(category string) int {
	result, err := CourseType()
	if err != nil || result == nil {
		return 0
	}
	for _, item := range result.Data.List {
		if item.Category == category {
			return item.Count
		}
	}
	return 0
}

func filterDedaoCourses(courses []services.Course, query string) []services.Course {
	term := strings.ToLower(strings.TrimSpace(query))
	if term == "" {
		return courses
	}
	filtered := make([]services.Course, 0, len(courses))
	for _, course := range courses {
		haystack := strings.ToLower(strings.Join([]string{
			course.Title,
			course.Author,
			course.Intro,
			course.LastRead,
			course.Price,
			course.Enid,
		}, " "))
		if strings.Contains(haystack, term) {
			filtered = append(filtered, course)
		}
	}
	return filtered
}

func dedaoEbookPageFromAllCourses(courses []services.Course, page, pageSize int) DedaoEbookPage {
	total := len(courses)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return dedaoEbookPageFromPagedCourses(courses[start:end], page, pageSize, total, 0)
}

func dedaoEbookPageFromPagedCourses(courses []services.Course, page, pageSize, total, isMore int) DedaoEbookPage {
	if total < len(courses) {
		total = len(courses)
	}
	totalPages := 0
	if total > 0 && pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return DedaoEbookPage{
		Ebooks:     dedaoEbooksFromCourses(courses),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		IsMore:     isMore,
	}
}

func dedaoEbookPageFromSiteSearch(result *services.EbookSearchResult, page, pageSize int) DedaoEbookPage {
	if result == nil {
		return DedaoEbookPage{
			Ebooks:   []DedaoEbook{},
			Page:     page,
			PageSize: pageSize,
		}
	}
	if result.Page > 0 {
		page = result.Page
	}
	if result.Size > 0 {
		pageSize = result.Size
	}
	total := result.Total
	totalPages := 0
	if total > 0 && pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	ebooks := make([]DedaoEbook, 0, len(result.List))
	for _, item := range result.List {
		detail := item.Detail
		title := firstNonEmptySearchField(detail.BookName, item.Title)
		author := firstNonEmptySearchField(detail.BookAuthor, detail.Author, item.Author)
		if author == "" && len(detail.AuthorList) > 0 {
			author = detail.AuthorList[0]
		}
		ebooks = append(ebooks, DedaoEbook{
			Enid:     detail.Enid,
			ID:       detail.ID,
			Title:    stripDedaoSearchHighlights(title),
			Author:   stripDedaoSearchHighlights(author),
			Intro:    stripDedaoSearchHighlights(firstNonEmptySearchField(detail.BookIntro, item.Content)),
			Icon:     firstNonEmptySearchField(detail.Cover, item.Image, item.Extra.Image),
			Price:    firstNonEmptySearchField(detail.CurrentPrice, detail.Price, detail.OriginalPrice),
			Progress: detail.ReadProgress,
			LastRead: stripDedaoSearchHighlights(detail.ReadingTitle),
			IsBuy:    detail.IsBuy,
			CanTrial: detail.CanTrialRead,
		})
	}
	return DedaoEbookPage{
		Ebooks:     ebooks,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		IsMore:     result.IsMore,
	}
}

func dedaoEbookFromServiceDetail(detail *services.EbookDetail) DedaoEbook {
	if detail == nil {
		return DedaoEbook{}
	}
	return DedaoEbook{
		Enid:       detail.Enid,
		ID:         detail.ID,
		Title:      detail.Title,
		Author:     firstNonEmptySearchField(detail.BookAuthor, strings.Join(detail.AuthorList, " / ")),
		Intro:      firstNonEmptySearchField(detail.BookIntro, detail.AuthorInfo, detail.OperatingTitle),
		Icon:       detail.Cover,
		Price:      firstNonEmptySearchField(detail.CurrentPrice, detail.Price, detail.OriginalPrice),
		Progress:   0,
		PublishNum: detail.Count,
		IsBuy:      detail.IsBuy,
		CanTrial:   detail.CanTrialRead,
	}
}

func dedaoCoursePageFromAllCourses(courses []services.Course, page, pageSize int) DedaoCoursePage {
	total := len(courses)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return dedaoCoursePageFromPagedCourses(courses[start:end], page, pageSize, total, 0)
}

func dedaoCoursePageFromPagedCourses(courses []services.Course, page, pageSize, total, isMore int) DedaoCoursePage {
	if total < len(courses) {
		total = len(courses)
	}
	totalPages := 0
	if total > 0 && pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return DedaoCoursePage{
		Courses:    dedaoCoursesFromCourses(courses),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		IsMore:     isMore,
	}
}

func dedaoOdobPageFromAllCourses(courses []services.Course, page, pageSize int) DedaoOdobPage {
	total := len(courses)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return dedaoOdobPageFromPagedCourses(courses[start:end], page, pageSize, total, 0)
}

func dedaoOdobPageFromPagedCourses(courses []services.Course, page, pageSize, total, isMore int) DedaoOdobPage {
	if total < len(courses) {
		total = len(courses)
	}
	totalPages := 0
	if total > 0 && pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return DedaoOdobPage{
		Odobs:      dedaoOdobsFromCourses(courses),
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		IsMore:     isMore,
	}
}

func dedaoTopicPageFromService(list *services.TopicAll, page, pageSize int) DedaoTopicPage {
	result := DedaoTopicPage{
		Page:     page,
		PageSize: pageSize,
	}
	if list == nil {
		return result
	}
	result.HasMore = list.HasMore
	result.Topics = make([]DedaoTopic, 0, len(list.List))
	for _, topic := range list.List {
		result.Topics = append(result.Topics, DedaoTopic{
			TopicIDHazy: topic.TopicIDHazy,
			Name:        topic.Name,
			Intro:       topic.Intro,
			Img:         topic.Img,
			Tag:         topic.Tag,
			ViewCount:   topic.ViewCount,
			NotesCount:  topic.NotesCount,
			HasNewNotes: topic.HasNewNotes,
		})
	}
	return result
}

func dedaoTopicNotePageFromService(topicID string, list *services.NotesList, page, pageSize int, isElected bool) DedaoTopicNotePage {
	result := DedaoTopicNotePage{
		TopicIDHazy: topicID,
		Page:        page,
		PageSize:    pageSize,
		IsElected:   isElected,
	}
	if list == nil {
		return result
	}
	result.HasMore = list.HasMore
	result.Notes = make([]DedaoTopicNote, 0, len(list.NoteDetailList))
	for _, note := range list.NoteDetailList {
		first := note.FPart
		result.Notes = append(result.Notes, DedaoTopicNote{
			NoteIDHazy:   first.NoteIDHazy,
			AuthorName:   first.NickName,
			Avatar:       first.Avatar,
			TimeDesc:     first.TimeDesc,
			NoteTitle:    first.NoteTitle,
			Note:         first.Note,
			Slogan:       first.Slogan,
			VInfo:        first.VInfo,
			TopicName:    note.Topic.TopicName,
			BaseTitle:    first.BaseSource.Title,
			BaseSubTitle: first.BaseSource.SubTitle,
			BaseImg:      first.BaseSource.Img,
			Images:       dedaoTopicNoteImages(first.Images),
			RepostCount:  note.NoteCount.RepostCount,
			CommentCount: note.NoteCount.CommentCount,
			LikeCount:    note.NoteCount.LikeCount,
		})
	}
	return result
}

func dedaoTopicNoteImages(images []string) []string {
	result := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := jsoniter.UnmarshalFromString(image, &payload); err == nil && strings.TrimSpace(payload.URL) != "" {
			result = append(result, strings.TrimSpace(payload.URL))
			continue
		}
		result = append(result, image)
	}
	return result
}

func dedaoEbooksFromCourses(courses []services.Course) []DedaoEbook {
	ebooks := make([]DedaoEbook, 0, len(courses))
	for _, course := range courses {
		ebooks = append(ebooks, DedaoEbook{
			Enid:       course.Enid,
			ID:         course.ID,
			Title:      course.Title,
			Author:     course.Author,
			Intro:      course.Intro,
			Icon:       course.Icon,
			Price:      course.Price,
			Progress:   course.Progress,
			PublishNum: course.PublishNum,
			LastRead:   course.LastRead,
		})
	}
	return ebooks
}

func firstNonEmptySearchField(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stripDedaoSearchHighlights(value string) string {
	value = strings.ReplaceAll(value, "<hl>", "")
	value = strings.ReplaceAll(value, "</hl>", "")
	return strings.TrimSpace(value)
}

func dedaoOdobsFromCourses(courses []services.Course) []DedaoOdob {
	result := make([]DedaoOdob, 0, len(courses))
	for _, course := range courses {
		audio := course.AudioDetail
		result = append(result, DedaoOdob{
			Enid:          course.Enid,
			ID:            course.ID,
			ClassID:       course.ClassID,
			Title:         course.Title,
			Intro:         course.Intro,
			Author:        course.Author,
			Icon:          firstNonEmpty(course.Icon, audio.Icon, audio.AudioListIcon),
			Price:         course.Price,
			Progress:      course.Progress,
			Duration:      firstNonZero(course.Duration, audio.Duration),
			PublishNum:    course.PublishNum,
			LastRead:      course.LastRead,
			AudioAliasID:  audio.AliasID,
			AudioTitle:    firstNonEmpty(audio.Title, audio.ShareTitle),
			AudioIcon:     firstNonEmpty(audio.Icon, audio.AudioListIcon),
			AudioDuration: audio.Duration,
			AudioPlayURL:  audio.Mp3PlayURL,
			HasPlayAuth:   course.HasPlayAuth,
		})
	}
	return result
}

func dedaoCoursesFromCourses(courses []services.Course) []DedaoCourse {
	result := make([]DedaoCourse, 0, len(courses))
	for _, course := range courses {
		result = append(result, DedaoCourse{
			Enid:       course.Enid,
			ID:         course.ID,
			ClassID:    course.ClassID,
			Title:      course.Title,
			Intro:      course.Intro,
			Author:     course.Author,
			Icon:       course.Icon,
			Price:      course.Price,
			Progress:   course.Progress,
			PublishNum: course.PublishNum,
			CourseNum:  course.CourseNum,
			LastRead:   course.LastRead,
		})
	}
	return result
}

func dedaoOdobDetailFromService(enid string, detail *services.AudioInfoResp) DedaoOdobDetail {
	if detail == nil {
		return DedaoOdobDetail{Enid: enid}
	}
	audio := detail.AudioInfo
	topics := make([]DedaoOdobTopicSummary, 0, len(audio.TopicSummary))
	for _, topic := range audio.TopicSummary {
		topics = append(topics, DedaoOdobTopicSummary{
			Title:    topic.Title,
			SubTitle: topic.SubTitle,
		})
	}
	return DedaoOdobDetail{
		Enid:           enid,
		ID:             audio.ID,
		Title:          audio.Title,
		Icon:           audio.Icon,
		Duration:       audio.Duration,
		AudioPrice:     audio.AudioPrice,
		AudioSummary:   audio.AudioSummary,
		PublishTime:    audio.PublishTime,
		IsVIP:          audio.IsVip,
		IsBuy:          audio.IsBuy,
		InBookrack:     audio.InBookrack,
		Progress:       audio.Progress,
		Tags:           audio.Tag,
		LearnCountDesc: audio.LearnCountDesc,
		Agency: DedaoOdobAgency{
			Name:           audio.AgencyDetail.Name,
			Intro:          audio.AgencyDetail.Intro,
			MemberName:     audio.AgencyDetail.QcgMemberName,
			MemberAvatar:   audio.AgencyDetail.QcgMemberAvatar,
			BookCount:      audio.AgencyDetail.BookCount,
			UserVisitCount: audio.AgencyDetail.Uv,
		},
		TopicSummary: topics,
	}
}

func dedaoCourseDetailMetaFromInfo(info *services.CourseInfo) DedaoCourseDetailMeta {
	if info == nil {
		return DedaoCourseDetailMeta{}
	}
	classInfo := info.ClassInfo
	title := classInfo.Name
	if title == "" {
		title = classInfo.ShareTitle
	}
	return DedaoCourseDetailMeta{
		Enid:           classInfo.Enid,
		ID:             classInfo.ID,
		IDStr:          classInfo.IDStr,
		Title:          title,
		Intro:          classInfo.Intro,
		Highlight:      classInfo.Highlight,
		LecturerName:   classInfo.LecturerName,
		LecturerTitle:  classInfo.LecturerTitle,
		LecturerIntro:  classInfo.LecturerIntro,
		LecturerAvatar: classInfo.LecturerAvatar,
		Logo:           firstNonEmpty(classInfo.Logo, classInfo.SquareImg, classInfo.PlayerImg),
		IndexImg:       firstNonEmpty(classInfo.IndexImg, classInfo.IndexImgApplet, classInfo.OutlineImg),
		ArticleCount:   classInfo.CurrentArticleCount,
		LearnUserCount: classInfo.LearnUserCount,
		PriceDesc:      classInfo.PriceDesc,
		IsSubscribe:    classInfo.IsSubscribe == 1,
	}
}

func dedaoArticlesFromIntros(articles []services.ArticleIntro) []DedaoArticle {
	result := make([]DedaoArticle, 0, len(articles))
	for _, article := range articles {
		result = append(result, dedaoArticleFromBase(article.ArticleBase))
	}
	return result
}

func dedaoArticlesFromBases(articles []services.ArticleBase) []DedaoArticle {
	result := make([]DedaoArticle, 0, len(articles))
	for _, article := range articles {
		result = append(result, dedaoArticleFromBase(article))
	}
	return result
}

func dedaoArticleFromBase(article services.ArticleBase) DedaoArticle {
	return DedaoArticle{
		Enid:        article.Enid,
		ID:          article.ID,
		IDStr:       article.IDStr,
		Title:       article.Title,
		Summary:     article.Summary,
		Logo:        article.Logo,
		PublishTime: article.PublishTime,
		IsRead:      article.IsRead,
		IsFreeTry:   article.IsFreeTry,
		OrderNum:    article.OrderNum,
		HasAudio:    len(article.AudioAliasIds) > 0,
		HasVideo:    article.VideoStatus == 1,
	}
}

func dedaoEbookDetailFromService(detail *services.EbookDetail) DedaoEbookDetail {
	if detail == nil {
		return DedaoEbookDetail{}
	}
	return DedaoEbookDetail{
		Enid:           detail.Enid,
		ID:             detail.ID,
		Title:          detail.Title,
		OperatingTitle: detail.OperatingTitle,
		Cover:          detail.Cover,
		Count:          detail.Count,
		Price:          detail.Price,
		AuthorInfo:     detail.AuthorInfo,
		BookAuthor:     detail.BookAuthor,
		PublishTime:    detail.PublishTime,
		BookIntro:      detail.BookIntro,
		AuthorList:     detail.AuthorList,
		PressName:      detail.Press.Name,
		PressBrief:     detail.Press.Brief,
		ClassifyName:   detail.ClassifyName,
		ProductScore:   detail.ProductScore,
		DoubanScore:    detail.DoubanScore,
		ReadTime:       detail.ReadTime,
		IsBuy:          detail.IsBuy,
		IsOnBookshelf:  detail.IsOnBookshelf,
		CanTrialRead:   detail.CanTrialRead,
		Catalog:        dedaoEbookCatalogFromService(detail.CatalogList),
	}
}

func dedaoEbookCatalogFromService(items []services.Catalog) []DedaoEbookCatalogItem {
	result := make([]DedaoEbookCatalogItem, 0, len(items))
	for _, item := range items {
		result = append(result, DedaoEbookCatalogItem{
			Level:     item.Level,
			Text:      item.Text,
			Href:      item.Href,
			ChapterID: ebookChapterIDFromHref(item.Href),
			PlayOrder: item.PlayOrder,
		})
	}
	return result
}

func ebookChapterIDFromHref(href string) string {
	chapterID := strings.TrimSpace(href)
	if chapterID == "" {
		return ""
	}
	if beforeHash, _, found := strings.Cut(chapterID, "#"); found {
		chapterID = beforeHash
	}
	return strings.TrimSpace(chapterID)
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

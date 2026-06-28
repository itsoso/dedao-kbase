package app

import (
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
	Articles []DedaoArticle `json:"articles"`
	Count    int            `json:"count"`
	MaxID    int            `json:"max_id"`
	IsMore   bool           `json:"is_more"`
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
	ListCourses(query string, page, pageSize int) (DedaoCoursePage, error)
	GetCourseDetail(enid string) (DedaoCourseDetail, error)
	ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error)
	GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error)
	GetEbookDetail(enid string) (DedaoEbookDetail, error)
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

func (p liveDedaoContentProvider) ListCourses(query string, page, pageSize int) (DedaoCoursePage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	query = strings.TrimSpace(query)
	if query != "" {
		list, err := getService().CourseListAll(CateCourse, "study")
		if err != nil {
			return DedaoCoursePage{}, err
		}
		courses := filterDedaoCourses(list.List, query)
		return dedaoCoursePageFromAllCourses(courses, page, pageSize), nil
	}

	list, err := CourseList(CateCourse, "study", page, pageSize)
	if err != nil {
		return DedaoCoursePage{}, err
	}
	total := dedaoCourseCategoryCount(CateCourse)
	courses := []services.Course{}
	isMore := 0
	if list != nil {
		courses = list.List
		isMore = list.ISMore
	}
	return dedaoCoursePageFromPagedCourses(courses, page, pageSize, total, isMore), nil
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
	if list == nil {
		return DedaoArticlePage{Count: count, MaxID: maxID}, nil
	}
	return DedaoArticlePage{
		Articles: dedaoArticlesFromIntros(list.List),
		Count:    count,
		MaxID:    list.MaxID,
		IsMore:   count > 0 && len(list.List) >= count && list.MaxID != 0,
	}, nil
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

func (p liveDedaoContentProvider) GetEbookDetail(enid string) (DedaoEbookDetail, error) {
	detail, err := EbookDetail(enid)
	if err != nil {
		return DedaoEbookDetail{}, err
	}
	return dedaoEbookDetailFromService(detail), nil
}

func (p liveDedaoContentProvider) GetEbookChapterPages(enid string, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error) {
	token, err := getService().EbookReadToken(enid)
	if err != nil {
		return DedaoEbookChapterPages{}, err
	}
	pageList, err := getService().EbookPages(chapterID, token.Token, index, count, offset)
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

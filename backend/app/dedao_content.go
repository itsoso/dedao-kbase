package app

import (
	"strings"

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

type DedaoContentProvider interface {
	ListEbooks(query string, page, pageSize int) (DedaoEbookPage, error)
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

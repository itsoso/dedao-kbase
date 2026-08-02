package app

import (
	"errors"
	"strings"

	"github.com/yann0917/dedao-gui/backend/services"
)

type DedaoEbook struct {
	Enid          string `json:"enid"`
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Author        string `json:"author,omitempty"`
	Intro         string `json:"intro,omitempty"`
	Icon          string `json:"icon,omitempty"`
	Price         string `json:"price,omitempty"`
	Progress      int    `json:"progress"`
	PublishNum    int    `json:"publish_num,omitempty"`
	LastRead      string `json:"last_read,omitempty"`
	IsBuy         bool   `json:"is_buy"`
	IsOnBookshelf bool   `json:"is_on_bookshelf"`
	CanTrial      bool   `json:"can_trial_read,omitempty"`
}

type DedaoEbookPage struct {
	Ebooks     []DedaoEbook `json:"ebooks"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
	Total      int          `json:"total"`
	TotalPages int          `json:"total_pages"`
	IsMore     int          `json:"is_more"`
}

type DedaoEbookAcquisitionService interface {
	SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error)
	AddEbookToBookshelf(enid string) (DedaoEbook, error)
	EbookDetail(enid string) (*services.EbookDetail, error)
}

type liveDedaoEbookAcquisitionService struct{}

func defaultDedaoEbookAcquisitionService(service DedaoEbookAcquisitionService) DedaoEbookAcquisitionService {
	if service != nil {
		return service
	}
	return liveDedaoEbookAcquisitionService{}
}

func (liveDedaoEbookAcquisitionService) SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return DedaoEbookPage{Ebooks: []DedaoEbook{}, Page: page, PageSize: pageSize}, nil
	}
	result, err := getService().SearchEbooks(query, page, pageSize)
	if err != nil {
		return DedaoEbookPage{}, err
	}
	return dedaoEbookPageFromSiteSearch(result, page, pageSize), nil
}

func (liveDedaoEbookAcquisitionService) AddEbookToBookshelf(enid string) (DedaoEbook, error) {
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
	ebook.IsOnBookshelf = true
	return ebook, nil
}

func (liveDedaoEbookAcquisitionService) EbookDetail(enid string) (*services.EbookDetail, error) {
	return EbookDetail(enid)
}

func dedaoEbookPageFromSiteSearch(result *services.EbookSearchResult, page, pageSize int) DedaoEbookPage {
	if result == nil {
		return DedaoEbookPage{Ebooks: []DedaoEbook{}, Page: page, PageSize: pageSize}
	}
	if result.Page > 0 {
		page = result.Page
	}
	if result.Size > 0 {
		pageSize = result.Size
	}
	totalPages := 0
	if result.Total > 0 && pageSize > 0 {
		totalPages = (result.Total + pageSize - 1) / pageSize
	}
	ebooks := make([]DedaoEbook, 0, len(result.List))
	for _, item := range result.List {
		detail := item.Detail
		title := firstNonEmptyEbookField(detail.BookName, item.Title)
		author := firstNonEmptyEbookField(detail.BookAuthor, detail.Author, item.Author)
		if author == "" && len(detail.AuthorList) > 0 {
			author = detail.AuthorList[0]
		}
		ebooks = append(ebooks, DedaoEbook{
			Enid:     detail.Enid,
			ID:       detail.ID,
			Title:    stripDedaoSearchHighlights(title),
			Author:   stripDedaoSearchHighlights(author),
			Intro:    stripDedaoSearchHighlights(firstNonEmptyEbookField(detail.BookIntro, item.Content)),
			Icon:     firstNonEmptyEbookField(detail.Cover, item.Image, item.Extra.Image),
			Price:    firstNonEmptyEbookField(detail.CurrentPrice, detail.Price, detail.OriginalPrice),
			Progress: detail.ReadProgress,
			LastRead: stripDedaoSearchHighlights(detail.ReadingTitle),
			IsBuy:    detail.IsBuy,
			CanTrial: detail.CanTrialRead,
		})
	}
	return DedaoEbookPage{
		Ebooks: ebooks, Page: page, PageSize: pageSize, Total: result.Total,
		TotalPages: totalPages, IsMore: result.IsMore,
	}
}

func dedaoEbookFromServiceDetail(detail *services.EbookDetail) DedaoEbook {
	if detail == nil {
		return DedaoEbook{}
	}
	return DedaoEbook{
		Enid: detail.Enid, ID: detail.ID, Title: detail.Title,
		Author: firstNonEmptyEbookField(detail.BookAuthor, strings.Join(detail.AuthorList, " / ")),
		Intro:  firstNonEmptyEbookField(detail.BookIntro, detail.AuthorInfo, detail.OperatingTitle),
		Icon:   detail.Cover, Price: firstNonEmptyEbookField(detail.CurrentPrice, detail.Price, detail.OriginalPrice),
		PublishNum: detail.Count, IsBuy: detail.IsBuy, IsOnBookshelf: detail.IsOnBookshelf, CanTrial: detail.CanTrialRead,
	}
}

func firstNonEmptyEbookField(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stripDedaoSearchHighlights(value string) string {
	value = strings.ReplaceAll(value, "<hl>", "")
	value = strings.ReplaceAll(value, "</hl>", "")
	return strings.TrimSpace(value)
}

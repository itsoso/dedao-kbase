package app

import (
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestDedaoEbookPageFromAllCoursesReturnsEmptyPagePastEnd(t *testing.T) {
	got := dedaoEbookPageFromAllCourses([]services.Course{
		{ID: 1, Enid: "one", Title: "第一本书"},
	}, 2, 15)

	if got.Page != 2 || got.Total != 1 || got.TotalPages != 1 {
		t.Fatalf("pagination metadata = %#v", got)
	}
	if len(got.Ebooks) != 0 {
		t.Fatalf("past-end page returned ebooks = %#v, want empty page", got.Ebooks)
	}
}

func TestDedaoSiteEbookSearchMapping(t *testing.T) {
	got := dedaoEbookPageFromSiteSearch(&services.EbookSearchResult{
		Page:      2,
		Size:      5,
		Total:     11,
		IsMore:    1,
		RequestID: "request-id",
		List: []services.EbookSearchItem{
			{
				Title:   "陆蓉行为<hl>金融</hl>学讲义",
				Author:  "陆蓉",
				Content: "命中章节片段",
				Image:   "https://example.test/search-cover.jpg",
				Detail: services.EbookSearchDetail{
					ID:               32355,
					Enid:             "site-ebook-enid",
					BookName:         "陆蓉行为金融学讲义",
					BookAuthor:       "陆蓉",
					BookIntro:        "聪明的投资者都该懂的实战智慧。",
					Cover:            "https://example.test/detail-cover.jpg",
					CurrentPrice:     "41.30",
					CanTrialRead:     true,
					ReadProgress:     17,
					ReadingTitle:     "第 1 章",
					AuthorList:       []string{"陆蓉"},
					ReadingWordToken: "must-not-leak",
				},
			},
		},
	}, 2, 5)

	if got.Page != 2 || got.PageSize != 5 || got.Total != 11 || got.TotalPages != 3 || got.IsMore != 1 {
		t.Fatalf("pagination metadata = %#v", got)
	}
	if len(got.Ebooks) != 1 {
		t.Fatalf("ebooks length = %d, want 1", len(got.Ebooks))
	}
	ebook := got.Ebooks[0]
	if ebook.ID != 32355 || ebook.Enid != "site-ebook-enid" || ebook.Title != "陆蓉行为金融学讲义" {
		t.Fatalf("ebook identity = %#v", ebook)
	}
	if ebook.Author != "陆蓉" || ebook.Intro != "聪明的投资者都该懂的实战智慧。" {
		t.Fatalf("ebook author/intro = %#v", ebook)
	}
	if ebook.Icon != "https://example.test/detail-cover.jpg" || ebook.Price != "41.30" {
		t.Fatalf("ebook cover/price = %#v", ebook)
	}
	if ebook.Progress != 17 || ebook.LastRead != "第 1 章" {
		t.Fatalf("ebook reading state = %#v", ebook)
	}
}

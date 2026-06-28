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

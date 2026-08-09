package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchWithContextTaskFailureDoesNotCancelSibling(t *testing.T) {
	okStarted := make(chan struct{})
	failureServed := make(chan struct{})
	releaseOK := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			close(okStarted)
			<-releaseOK
			_, _ = w.Write([]byte("complete"))
		case "/fail":
			<-okStarted
			w.WriteHeader(http.StatusInternalServerError)
			close(failureServed)
		}
	}))
	defer server.Close()
	tasks := NewDownloadTasks()
	root := t.TempDir()
	failurePath := filepath.Join(root, "failure")
	successPath := filepath.Join(root, "success")
	tasks.Add(server.URL+"/fail", failurePath)
	tasks.Add(server.URL+"/ok", successPath)
	done := make(chan *DownloadTasks, 1)
	go func() { done <- BatchWithContext(context.Background(), tasks, 2, time.Second) }()
	select {
	case <-failureServed:
	case <-time.After(time.Second):
		close(releaseOK)
		t.Fatal("failed task was not served")
	}
	close(releaseOK)
	select {
	case completed := <-done:
		if completed.tasks[0].Err == nil {
			t.Fatal("failed task did not retain its error")
		}
		if completed.tasks[1].Err != nil {
			t.Fatalf("successful sibling was canceled: %v", completed.tasks[1].Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("batch did not complete")
	}
	data, err := os.ReadFile(successPath)
	if err != nil || string(data) != "complete" {
		t.Fatalf("successful sibling output=%q err=%v", data, err)
	}
}

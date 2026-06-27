package backend

import (
	"context"
	"encoding/json"

	"github.com/yann0917/dedao-gui/backend/app"
)

func (a *App) BookKnowledgeRoot() string {
	return app.DefaultBookKnowledgeRoot()
}

func (a *App) BookKnowledgeListBooks() ([]app.BookKnowledgeBook, error) {
	return app.DefaultBookKnowledgeStore().ListBooks()
}

func (a *App) BookKnowledgeGetBook(bookID string) (*app.BookKnowledgePackage, error) {
	return app.DefaultBookKnowledgeStore().LoadPackage(bookID)
}

func (a *App) BookKnowledgeSearch(query, bookID string, limit int) ([]app.BookKnowledgeSearchResult, error) {
	return app.DefaultBookKnowledgeStore().Search(app.BookKnowledgeSearchQuery{
		Query:  query,
		BookID: bookID,
		Limit:  limit,
	})
}

func (a *App) BookKnowledgeExport(bookID, target string) (*app.BookKnowledgeExportResult, error) {
	return app.ExportBookKnowledgePackage(app.DefaultBookKnowledgeStore(), bookID, target)
}

func (a *App) BookKnowledgeNotebookLMBridge(bookID string) (*app.BookKnowledgeNotebookLMBridge, error) {
	return app.DefaultBookKnowledgeStore().LoadNotebookLMBridge(bookID)
}

func (a *App) BookKnowledgeNotebookLMExport(bookID string) (*app.BookKnowledgeNotebookLMBridge, error) {
	return app.ExportNotebookLMBridgePackage(app.DefaultBookKnowledgeStore(), bookID)
}

func (a *App) BookKnowledgeNotebookLMSaveLink(bookID, notebookURL string) (*app.BookKnowledgeNotebookLMBridge, error) {
	return app.DefaultBookKnowledgeStore().SaveNotebookLMLink(bookID, notebookURL)
}

func (a *App) BookKnowledgeChat(bookID, mode, question, model string) (*app.BookKnowledgeChatResponse, error) {
	ctx := a.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return app.BookKnowledgeChat(ctx, app.DefaultBookKnowledgeStore(), app.BookKnowledgeChatRequest{
		BookID:   bookID,
		Mode:     mode,
		Question: question,
		Model:    model,
	})
}

func (a *App) BookKnowledgeChatHistory(bookID string, limit int) ([]app.BookKnowledgeChatHistoryItem, error) {
	return app.DefaultBookKnowledgeStore().ListChatHistory(bookID, limit)
}

func (a *App) BookKnowledgeMCPTools() []app.BookKnowledgeMCPTool {
	return app.NewBookKnowledgeMCPServer(app.DefaultBookKnowledgeStore()).Tools()
}

func (a *App) BookKnowledgeMCPCall(name string, arguments map[string]any) (string, error) {
	payload, err := json.Marshal(arguments)
	if err != nil {
		return "", err
	}
	result, err := app.NewBookKnowledgeMCPServer(app.DefaultBookKnowledgeStore()).Call(name, payload)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

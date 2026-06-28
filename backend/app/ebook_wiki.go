package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yann0917/dedao-gui/backend/utils"
)

const defaultEbookWikiRepoDir = "/Users/liqiuhua/work/personal/down-dedao"

type EbookWikiSyncConfig struct {
	RepoDir      string
	WikisCommand string
	Python       string
}

type EbookWikiInput struct {
	BookID   int
	Title    string
	HTMLPath string
}

type EbookWikiSyncResult struct {
	BookID            int    `json:"book_id"`
	KnowledgeBookID   string `json:"knowledge_book_id"`
	Title             string `json:"title"`
	HTMLPath          string `json:"html_path"`
	RepoDir           string `json:"repo_dir"`
	BookKnowledgeRoot string `json:"book_knowledge_root"`
}

type ebookWikiCommand struct {
	Dir  string
	Name string
	Args []string
}

type ebookWikiCommandRunner interface {
	Run(context.Context, ebookWikiCommand) ([]byte, error)
}

type osEbookWikiCommandRunner struct{}

func DefaultEbookWikiSyncConfig() EbookWikiSyncConfig {
	cfg := EbookWikiSyncConfig{
		RepoDir:      defaultEbookWikiRepoDir,
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}
	if value := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO")); value != "" {
		cfg.RepoDir = value
	}
	if value := strings.TrimSpace(os.Getenv("DEDAO_WIKI_COMMAND")); value != "" {
		cfg.WikisCommand = value
	}
	if value := strings.TrimSpace(os.Getenv("DEDAO_WIKI_PYTHON")); value != "" {
		cfg.Python = value
	}
	return cfg
}

func (cfg EbookWikiSyncConfig) withDefaults() EbookWikiSyncConfig {
	defaults := DefaultEbookWikiSyncConfig()
	if strings.TrimSpace(cfg.RepoDir) == "" {
		cfg.RepoDir = defaults.RepoDir
	}
	if strings.TrimSpace(cfg.WikisCommand) == "" {
		cfg.WikisCommand = defaults.WikisCommand
	}
	if strings.TrimSpace(cfg.Python) == "" {
		cfg.Python = defaults.Python
	}
	return cfg
}

func (r osEbookWikiCommandRunner) Run(ctx context.Context, cmd ebookWikiCommand) ([]byte, error) {
	execCmd := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	execCmd.Dir = cmd.Dir
	return execCmd.CombinedOutput()
}

func SyncEbookToWiki(ctx context.Context, id int, enid string) (*EbookWikiSyncResult, error) {
	return syncEbookToWikiWithConfig(ctx, id, enid, DefaultEbookWikiSyncConfig(), osEbookWikiCommandRunner{})
}

func SyncEbookToWikiStore(ctx context.Context, id int, enid string, store *BookKnowledgeStore) (*EbookWikiSyncResult, error) {
	return syncEbookToWikiWithConfigAndStore(ctx, id, enid, DefaultEbookWikiSyncConfig(), osEbookWikiCommandRunner{}, store)
}

func syncEbookToWikiWithConfig(
	ctx context.Context,
	id int,
	enid string,
	cfg EbookWikiSyncConfig,
	runner ebookWikiCommandRunner,
) (*EbookWikiSyncResult, error) {
	return syncEbookToWikiWithConfigAndStore(ctx, id, enid, cfg, runner, DefaultBookKnowledgeStore())
}

func syncEbookToWikiWithConfigAndStore(
	ctx context.Context,
	id int,
	enid string,
	cfg EbookWikiSyncConfig,
	runner ebookWikiCommandRunner,
	knowledgeStore *BookKnowledgeStore,
) (*EbookWikiSyncResult, error) {
	cfg = cfg.withDefaults()
	if knowledgeStore == nil {
		knowledgeStore = DefaultBookKnowledgeStore()
	}
	emitEbookWikiProgress(ctx, "正在下载电子书")
	download := EBookDownload{
		Ctx:          ctx,
		DownloadType: 1,
		ID:           id,
		EnID:         enid,
		OutputDir:    cfg.RepoDir,
	}
	result, err := download.DownloadWithResult()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.HTMLPath) == "" {
		return nil, fmt.Errorf("电子书 HTML 路径为空: book_id=%d", id)
	}
	if _, err = os.Stat(result.HTMLPath); err != nil {
		return nil, fmt.Errorf("电子书 HTML 文件不存在: %s: %w", result.HTMLPath, err)
	}

	input := EbookWikiInput{
		BookID:   id,
		Title:    result.Title,
		HTMLPath: result.HTMLPath,
	}
	emitEbookWikiProgress(ctx, "正在生成本地知识包")
	knowledgePackage, err := BuildBookKnowledgeFromHTMLFile(BookKnowledgeBook{
		BookID:  strconv.Itoa(id),
		DedaoID: id,
		EnID:    enid,
		Title:   result.Title,
	}, result.HTMLPath, knowledgeStore)
	if err != nil {
		return nil, err
	}

	if err = runEbookWikiPipeline(ctx, cfg, runner, input); err != nil {
		if !isEbookWikiCommandMissing(err) {
			return nil, err
		}
		emitEbookWikiProgress(ctx, "llms-wikis 不可用，已使用本地提取器")
	}
	return &EbookWikiSyncResult{
		BookID:            id,
		KnowledgeBookID:   knowledgePackage.Book.BookID,
		Title:             result.Title,
		HTMLPath:          result.HTMLPath,
		RepoDir:           cfg.RepoDir,
		BookKnowledgeRoot: knowledgeStore.Root(),
	}, nil
}

func ebookHTMLPath(outputDir, title string) (string, error) {
	return utils.FilePath(filepath.Join(outputDir, "Ebook", utils.FileName(title, "")), "html", false)
}

func ebookWikiIngestCommand(cfg EbookWikiSyncConfig, inputPath string, bookID int, title string) ebookWikiCommand {
	cfg = cfg.withDefaults()
	return ebookWikiCommand{
		Dir:  cfg.RepoDir,
		Name: cfg.WikisCommand,
		Args: []string{
			"ingest-ebook",
			"--repo", cfg.RepoDir,
			"--input", inputPath,
			"--book-id", strconv.Itoa(bookID),
			"--title", title,
		},
	}
}

func ebookWikiCompilerCommand(cfg EbookWikiSyncConfig) ebookWikiCommand {
	cfg = cfg.withDefaults()
	return ebookWikiCommand{
		Dir:  cfg.RepoDir,
		Name: cfg.Python,
		Args: []string{"pipeline/compiler.py", "--changed-only"},
	}
}

func runEbookWikiPipeline(
	ctx context.Context,
	cfg EbookWikiSyncConfig,
	runner ebookWikiCommandRunner,
	input EbookWikiInput,
) error {
	cfg = cfg.withDefaults()
	if runner == nil {
		runner = osEbookWikiCommandRunner{}
	}
	if strings.TrimSpace(input.HTMLPath) == "" {
		return fmt.Errorf("电子书 HTML 路径为空: book_id=%d", input.BookID)
	}

	emitEbookWikiProgress(ctx, "正在提取到 Wiki")
	ingestCmd := ebookWikiIngestCommand(cfg, input.HTMLPath, input.BookID, input.Title)
	if output, err := runner.Run(ctx, ingestCmd); err != nil {
		return ebookWikiCommandError("llms-wikis 提取失败", ingestCmd, output, err)
	}

	emitEbookWikiProgress(ctx, "正在编译 Wiki")
	compilerCmd := ebookWikiCompilerCommand(cfg)
	if output, err := runner.Run(ctx, compilerCmd); err != nil {
		return ebookWikiCommandError("Wiki 编译失败", compilerCmd, output, err)
	}

	emitEbookWikiProgress(ctx, "Wiki 同步完成")
	return nil
}

func ebookWikiCommandError(stage string, cmd ebookWikiCommand, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %s: %w", stage, formatEbookWikiCommand(cmd), err)
	}
	return fmt.Errorf("%s: %s: %w: %s", stage, formatEbookWikiCommand(cmd), err, message)
}

func isEbookWikiCommandMissing(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "executable file not found") ||
		strings.Contains(message, "no such file or directory")
}

func formatEbookWikiCommand(cmd ebookWikiCommand) string {
	return strings.Join(append([]string{cmd.Name}, cmd.Args...), " ")
}

func emitEbookWikiProgress(ctx context.Context, value string) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	runtime.EventsEmit(ctx, "ebookDownload", Progress{
		Total:   100,
		Current: 100,
		Pct:     100,
		Value:   value,
	})
}

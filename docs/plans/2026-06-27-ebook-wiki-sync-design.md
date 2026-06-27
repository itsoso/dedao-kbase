# Ebook Wiki Sync Design

## Goal

Add a `dedao-gui` workflow that downloads one selected Dedao ebook into the local `down-dedao` vault and then triggers wiki extraction for that ebook.

## Current State

`dedao-gui` already owns the authenticated Dedao session and can download ebooks from the shelf. The existing ebook path resolves detail metadata, gets a read token, fetches encrypted SVG pages, decrypts them, and generates HTML, PDF, or EPUB files through `backend/app/download.go` and `backend/utils/svg2html.go`.

`down-dedao` already stores ebooks under `Ebook/` and compiles wiki pages through `pipeline/compiler.py`. There is no `llms-wikis` executable checked into either repository, so `dedao-gui` should treat it as an external local command and fail visibly if it is not installed.

## Recommended Flow

1. The user clicks `下载并入 Wiki` on one ebook row.
2. `dedao-gui` downloads the ebook as HTML to `/Users/liqiuhua/work/personal/down-dedao/Ebook`.
3. The backend receives the generated HTML file path.
4. The backend runs `llms-wikis ingest-ebook --repo /Users/liqiuhua/work/personal/down-dedao --input <html> --book-id <id> --title <title>`.
5. If extraction succeeds, the backend runs `python3 pipeline/compiler.py --changed-only` inside `down-dedao`.
6. Progress and failure messages are emitted through Wails events and displayed in the existing download dialog.

## Architecture

Keep `dedao-gui` as the only Dedao-facing component. Do not reimplement login, cookies, read-token handling, or ebook decryption outside the app.

Add a small backend service around the existing ebook downloader:

- make the HTML download path observable instead of only returning `error`;
- add a wiki-sync command runner with configurable command, repo path, and Python executable defaults;
- expose one Wails method for the UI: selected ebook in, sync result out;
- reuse existing `ebookDownload` progress events so the UI does not need a second progress system.

## Defaults

- Vault repo: `/Users/liqiuhua/work/personal/down-dedao`
- Ebook output root: vault repo itself, because existing `Svg2Html` writes under `<output>/Ebook`
- Wiki command: `llms-wikis`
- Compiler command: `python3 pipeline/compiler.py --changed-only`

These should be backend defaults for the local workflow. Later they can move into settings if this needs to be portable.

The backend also supports local overrides:

- `DEDAO_WIKI_REPO`
- `DEDAO_WIKI_COMMAND`
- `DEDAO_WIKI_PYTHON`

## Error Handling

Every stage must return an error to the UI:

- Dedao download failures return the existing download error.
- Missing generated HTML file returns a clear internal error.
- Missing `llms-wikis` or non-zero exit status returns command output.
- Compiler failure returns command output.

No silent fallback should mark the sync as successful.

## Testing

Start with Go unit tests for command construction and orchestration failure behavior. Use a fake command runner so tests do not need network, Dedao credentials, or the real `llms-wikis` command.

Then run focused Go tests for `backend/app`, followed by a broader backend compile/test pass if the focused tests are green.

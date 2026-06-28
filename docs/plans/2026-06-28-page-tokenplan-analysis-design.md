# Page TokenPlan Analysis Design

## Goal

Add a TokenPlan analysis entry to the Web course and ebook reading surfaces so a learner can ask the model to analyze the currently visible study context without first converting the item into the book knowledge base.

## Scope

This slice covers one-shot page analysis only. It does not add chat history, streaming, or cross-page memory. The existing book knowledge chat remains the deeper book-grounded conversation surface.

## User Experience

Course and ebook detail pages show a compact `TokenPlan 分析` panel in the right-side context column. The panel includes a model dropdown, a prompt textarea, quick prompt buttons, a submit button, and a rendered Markdown answer. The default model is `qwen3.7-max`.

The request uses the current page state:

- Course: course metadata, article list, selected article title, and selected article Markdown.
- Ebook: ebook metadata, catalog, selected chapter title, and loaded page text/metadata extracted from SVG where possible.

## Architecture

Add a protected `POST /api/analyze-page` endpoint to `cmd/kbase-server` through `backend/app/kbase_http.go`. The handler accepts typed page context sections, builds a bounded prompt, and reuses the existing TokenPlan OpenAI-compatible client in `backend/app/book_chat.go`.

Frontend changes stay in `frontend-web`: extend `KBaseClient`, add a reusable analysis panel component, and wire it into `CourseDetailReader.vue` and `EbookDetailReader.vue`.

## Error Handling

The backend rejects empty context or empty questions with `400`, and surfaces TokenPlan errors as JSON errors. The frontend keeps errors local to the analysis panel and does not clear the reading state.

## Verification

Backend tests cover prompt construction, auth-protected HTTP access, and TokenPlan request metadata. Frontend verification uses `npm run build` to validate Vue and TypeScript integration.

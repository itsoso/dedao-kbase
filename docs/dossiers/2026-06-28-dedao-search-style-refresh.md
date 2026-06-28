# Dedao Search Style Refresh Dossier

## Status

Deployed and verified on production.

## Requirement

Reference `https://www.dedao.cn/search/ebook/result?...q=金融` and refactor the current Web UI and styling.

## Reference Notes

- Header has a 1200px centered layout, logo, search field, orange search button, and light account actions.
- Main content is centered and narrow compared with the viewport.
- Book/result modules use light gray fills, 10px radius, and almost no shadow.
- Search result rows use thin separators and orange keyword highlights.
- Metadata is muted gray; primary action color is orange.

## Gate Notes

- G1 Scope: accepted as frontend style refresh only.
- G2 Feasibility: no backend/API changes required.
- G3 Tests: passed with `npm --prefix frontend-web run build`, Playwright screenshots for `/ebook`, `/course`, `/book-knowledge`, `/course/demo-course`, `/ebook/demo-ebook`, and `git diff --check`.
- G4 Review: old blue/green primary colors were removed from `frontend-web/src` style sources; shell width and body background were checked in browser automation.
- G5 Deploy Health: static frontend bundle deployed to `/var/www/kbase.executor.life`; `/health` returned HTTP 200.
- G6 Online Verification: `/ebook` remains Basic Auth protected as expected, and deployed CSS contains `--dedao-orange` with old blue/green primary colors absent.

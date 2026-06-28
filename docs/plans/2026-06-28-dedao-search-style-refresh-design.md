# Dedao Search Style Refresh Design

## Goal

Refresh the Web UI so course, ebook, and knowledge-workbench pages feel closer to dedao.cn search/result pages while preserving the existing private KBase workflows.

## Reference Signals

The reference page uses a quiet white canvas, a centered content column, a compact header search bar, orange primary actions, shallow gray modules, and search-result rows separated by thin lines. It avoids heavy shadows and dense boxed cards. Typography is direct: 20px section titles, 16px result text, 12px muted metadata, and orange highlights for matches or primary actions.

## Visual Direction

Use a Dedao-inspired utilitarian learning interface:

- Page background: white to very light gray, not blue-gray.
- Main width: constrained and centered, with wide desktop breathing room.
- Primary color: Dedao orange for search, submit, active states, and highlights.
- Surfaces: light gray modules with 10px radius and minimal border.
- Lists: rows with bottom separators; avoid every row looking like a floating card.
- Header: logo/title plus search-like navigation, compact and horizontal.

## Scope

This slice updates the Web shell, global controls, shared cards/panels, course list, ebook list, and KBase workbench list surfaces. It does not change data fetching, TokenPlan behavior, login, or job APIs.

## Verification

Run `npm --prefix frontend-web run build`, then inspect desktop screenshots for `/ebook`, `/course`, and `/book-knowledge`. Ensure text does not overflow and the layout remains usable at the existing minimum desktop width.

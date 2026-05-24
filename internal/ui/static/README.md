# UI static assets

Phase 7a serves Tailwind and HTMX from CDNs (`cdn.tailwindcss.com`,
`unpkg.com/htmx.org`) embedded in `templates/layout.html`. Phase 7c
will replace these with vendored, minified bundles served from this
directory via `embed.FS` + `http.FileServer`.

This file exists so the embed pattern in `ui.go` has at least one
matched entry; remove or replace once real assets land.

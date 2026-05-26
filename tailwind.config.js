// Configuration for the standalone tailwindcss CLI used by `make ui-css`.
// Run `make ui-css` after editing any HTML template so the generated CSS
// in internal/ui/static/css/tailwind.css stays in sync with the classes
// actually referenced; the binary scans these globs (including class
// names embedded in inline <script> string literals) and tree-shakes
// every utility that does not appear.
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/ui/templates/**/*.html"],
  darkMode: 'class',
  theme: { extend: {} },
  plugins: [],
};

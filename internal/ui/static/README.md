# UI static assets

`css/tailwind.css` is the minified Tailwind build referenced by
`templates/layout.html`. It is generated from `css/input.css` and the
templates by the standalone tailwindcss CLI (pinned in the Makefile),
tree-shaken to the classes actually used in `templates/**/*.html`.

To regenerate after editing any template:

```sh
make ui-css
```

The Makefile downloads the right tailwindcss binary into `bin/` on first
use (gitignored) and reuses it thereafter. The generated `tailwind.css`
is checked in so production builds, CI, and offline / closed-network
deployments do not need the CLI or any network access.

# Plandillo landing page

Vercel builds and serves this English site as an Astro project with its Root Directory set to `landing` ([Vercel guide](../docs/vercel.md)). The Makefile is only for local work. There is no hosted demo: every call to action leads to the one-command Docker install in the self-host section.

The English site for `plandillo.dev` lives in this directory. It is built with [Astro](https://astro.build) and [Tailwind CSS](https://tailwindcss.com) v4 (the same version as `web/`) into a static page with no client framework. One dependency-free script (`src/scripts/app.js`) runs the board demo, the tabs, the copy button, the mobile menu and the GitHub star count. The demo plays on its own, pauses off screen or in a hidden tab, and stays on one stage with `prefers-reduced-motion`. The script also runs the theme switch. The star count is the only request to another host (`api.github.com`, cached for an hour in localStorage); if it fails, the counter stays hidden.

## Preview and build

From the repository root, run:

```sh
make -C landing serve
```

Open <http://127.0.0.1:4174/>. The Astro dev server reloads on every edit. Press Ctrl+C to stop it. Pass `PORT=4180` to select another port.

```sh
make -C landing build
```

The static site is in `landing/dist/`. Serve its contents at the domain root. Both targets need [Bun](https://bun.sh) and install dependencies from `bun.lock` on first run. Astro telemetry is turned off in the Makefile.

## Editing

Edit `src/pages/index.astro` for copy, links and the install commands, and `src/layouts/Base.astro` for metadata. Curly braces in markup are Astro expressions, so write literal ones in code samples as `&#123;` and `&#125;`. The copy button reads the `data-command` attribute of the open tab, so change it together with the `<pre>` block. The board demo copies the planner's own board and task panel: its markup and classes follow `web/src/Board.tsx`, `TaskPanel.tsx` and `Sidebar.tsx`, its colours are the planner's tokens from `web/src/styles.css` (as `app-*` in `global.css`), and its icons come from `web/src/icons.tsx` (`src/components/Icon.astro`). Change them together when the planner's look changes. The demo data is at the top of `src/scripts/app.js`. Styling is Tailwind utilities in the markup; class sets used more than once are constants at the top of `index.astro`. `src/styles/global.css` holds the fonts, the colour tokens and breakpoints in `@theme`. Each colour is a `light-dark(light, dark)` pair: the page follows the system theme, and the header switch stores the visitor's choice in localStorage (`plandillo:theme`), applied before the first paint by an inline script in `Base.astro`. Add new colours as pairs, never as a fixed hex in the markup (the design is desktop-first, so the page uses `max-xs`, `max-tab`, `max-nav` and `max-board`), and a few component classes for the board demo nodes that `app.js` builds. A checked "Done when" line in the static task card is `<Check on>` from `src/components/Check.astro`. Links to the planner carry `data-app`, which forwards `utm_*` parameters.

The CLI tab is marked as coming soon. The Go metrics and the partner and sponsor blocks are left out until there are real numbers and names. Keep product commands and README anchors in sync with the [installation guide](../docs/hosting.md#quick-start) and [MCP setup guide](../docs/agents.md). The navigation points to the [huntly-digital/plandillo repository](https://github.com/huntly-digital/plandillo).

All runtime assets are local to `public/assets/` and are copied as they are. The icons come from the supplied Plandillo vector kit. The social preview `share.jpg` is generated artwork. Bundled Inter and JetBrains Mono font files include their SIL Open Font Licenses ([Inter](public/assets/fonts/Inter-LICENSE.txt), [JetBrains Mono](public/assets/fonts/JetBrainsMono-LICENSE.txt)). The project uses the [MIT license](../LICENSE).

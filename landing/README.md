# Plandillo landing page

The landing service in the root Vercel project serves this English site. The free demo button opens `https://app.plandillo.dev/register`, and Sign in opens the planner. Campaign parameters are preserved on both links.

The English site for `plandillo.dev` lives in this directory. It uses static HTML, CSS, and a small clipboard script. The three stages of the example task work with the keyboard and with JavaScript disabled.

## Preview and build

From the repository root, run:

```sh
make -C landing serve
```

Open <http://127.0.0.1:4174/>. Press Ctrl+C to stop the server. Pass `PORT=4180` to select another port. After editing, rerun `make -C landing build` and refresh the browser.

```sh
make -C landing build
```

The static site is in `landing/dist/`. Serve its contents at the domain root. Building uses Make, cp, and rsync. The preview also uses Python 3. No package install is required.

## Editing

Edit `index.html` for copy, links, task stages, metadata, and the installation snippet. The copy button reads the displayed commands. Styles are in `style.css`. Keep product commands and README anchors in sync with the [installation guide](../docs/hosting.md#quick-start) and [MCP setup guide](../docs/agents.md). The navigation points to the [huntly-digital/plandillo repository](https://github.com/huntly-digital/plandillo).

All runtime assets are local to `assets/`. The wordmark and icons come from the supplied Plandillo vector kit. The armadillo illustration is generated artwork, with two WebP sizes for the page and a JPEG for social previews. Bundled Inter font files include their [SIL Open Font License](assets/fonts/Inter-LICENSE.txt). The project uses the [MIT license](../LICENSE).

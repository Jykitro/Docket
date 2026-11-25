# Plandillo landing page

The landing service in the root Vercel project serves this English site. The free demo button opens `https://app.plandillo.dev/register`, and Sign in opens the planner. Campaign parameters are preserved on both links.

The English site for `plandillo.dev` lives in this directory. It uses static HTML, CSS, and one dependency-free script (`app.js`): the board demo, the tabs, the copy button, the mobile menu and the GitHub star count. The demo plays on its own, pauses off screen or in a hidden tab, and stays on one stage with `prefers-reduced-motion`. The star count is the only request to another host (`api.github.com`, cached for an hour in localStorage); if it fails, the counter stays hidden.

## Preview and build

From the repository root, run:

```sh
make -C landing serve
```

Open <http://127.0.0.1:4174/>. Press Ctrl+C to stop the server. Pass `PORT=4180` to select another port. After editing, rerun `make -C landing build` and refresh the browser.

```sh
make -C landing build
```

The static site is in `landing/dist/`. Serve its contents at the domain root. Building uses Make and standard file utilities. The preview also uses Python 3. No package install is required.

## Editing

Edit `index.html` for copy, links, metadata, and the install commands. The copy button reads the `data-command` attribute of the open tab, so change it together with the `<pre>` block. The board demo data is at the top of `app.js`. Styles are in `style.css`. Links to the planner carry `data-app`, which forwards `utm_*` parameters.

The CLI tab is marked as coming soon. The Go metrics, the paid Cloud price, and the partner and sponsor blocks are left out until there are real numbers and names. Keep product commands and README anchors in sync with the [installation guide](../docs/hosting.md#quick-start) and [MCP setup guide](../docs/agents.md). The navigation points to the [huntly-digital/plandillo repository](https://github.com/huntly-digital/plandillo).

All runtime assets are local to `assets/`. The icons come from the supplied Plandillo vector kit. The social preview `share.jpg` is generated artwork. Bundled Inter and JetBrains Mono font files include their SIL Open Font Licenses ([Inter](assets/fonts/Inter-LICENSE.txt), [JetBrains Mono](assets/fonts/JetBrainsMono-LICENSE.txt)). The project uses the [MIT license](../LICENSE).

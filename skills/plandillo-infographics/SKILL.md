---
name: plandillo-infographics
description: Use when writing a Plandillo wiki page, task description or HTML document that a person reads to grasp status, readiness, a plan, a comparison, a timeline or numbers, or when the user asks for a report, dashboard, chart or diagram in Plandillo.
---

# Clear pages and visuals in Plandillo

Write for the reader who opens the page with one question: where do things stand, what comes next, what needs me. Answer it on the first screen, then let them drill down. Use the user's language for every heading, label and legend.

## Pick the format

| The reader needs | Use | Why |
|---|---|---|
| Status, a plan, decisions, a runbook: text people will edit | Markdown page (`write_page` with `body_md`, `append_to_page`) or a task `description_md` | Editable in place; `[[Page]]` links, task codes and `- [ ]` checklists stay live. |
| One chart, timeline or diagram inside such a page | A top-level `html-preview` fence in the Markdown | The visual sits next to text that stays editable. |
| A dashboard or report that is mostly visual | An HTML page (`write_page` with `body_html`) | The whole page is the document. |
| A deliverable owned by a task or page, kept with versions | `publish_html` | Each revision is kept; readers see the version number. |

Prefer Markdown. Switch to HTML only when position, length or color carries meaning: progress bars, charts, timelines, matrices, flows. Tool arguments are in `plandillo-reference`.

## Lay out the page

1. **Title** names the subject and the question: "Release 1.2: readiness", not "Notes".
2. **Status block** right under it, in a quote: overall state, date of the data, the next action with its task code, and what needs the reader. Example: `> **State:** 🟡 at risk · **As of:** 2026-11-26 · **Next:** sign Windows builds (TRK-12) · **Needs you:** pick the tap name`.
3. **Summary**: at most three bullets with the facts that changed a decision.
4. **Body**: one section per area, each with a table or a short list. Order rows by priority, then by state, never at random.
5. **Details** last: logs, raw numbers, alternatives, in `<details><summary>…</summary>` blocks or in child pages (`write_page` with `parent`) linked by `[[Title]]`.
6. **Links**: related tasks by code and pages by `[[Title]]`, not pasted copies of their content.

Keep headings to three levels and make each one say what is below it. Use numbered lists for steps in order, bullets for unordered facts, and checkboxes only for things someone will tick.

## Show readiness and progress

- Derive readiness from the record, not from a guess: ticked checklist items over all items, or tasks in a done-category status over all tasks in scope. Write it as "7 of 10 steps (70%)", with the denominator.
- Give one overall figure and a row per area, so the weak spot is visible. A 90% total that hides a 0% area misleads.
- Say what "ready" means for this page in one line (for example "merged, tested on three systems, documented").
- Pair every state with a word and a marker, and keep the legend the same across the workspace: ✅ done, 🟡 in progress, ⛔ blocked, ⬜ not started, ❗ needs a decision. A marker alone fails for screen readers and colour-blind readers.
- Put a date on every figure that changes ("as of 2026-11-26"). When the page will outlive the work, link the task instead of copying its status.

## Choose the level of detail

Write the summary for someone with one minute and the details for someone who must act. Add detail where the reader would otherwise ask: a blocker and what unblocks it, a decision with the options that lost, a failed check with the exact error and command, a number that moved a lot. Leave out steps that went as planned; a count and a link cover them. When a section grows past one screen, collapse it in `<details>` or move it to a child page and keep a one-line summary with the link.

## Markdown the board renders

GitHub-flavoured Markdown: headings, tables with column alignment (`---:` for numbers), task lists, quotes, code blocks, strikethrough, and the HTML tags `<details>`, `<summary>`, `<mark>`, `<kbd>`, `<sup>`, `<sub>`. Images show only from the board itself or as `data:image/png|jpeg|gif|webp|avif;base64` URIs. Other HTML, `style` and `class` attributes, SVG, Mermaid and remote images are removed, so use an `html-preview` fence for anything drawn.

Tables work best for three or more items compared on two or more attributes, with at most seven columns. Put the identifier first, right-align numbers, keep units in the header (`Time, h`) and keep cells short; long text belongs below the table.

```markdown
## Progress

| Area | State | Steps | Next | Task |
|---|---|---:|---|---|
| CLI core | ✅ done | 8/8 | none | PLD-149 |
| Packages | 🟡 in progress | 2/6 | Homebrew tap | PLD-151 |
| Signing | ⛔ blocked | 0/2 | Apple account | PLD-152 |
```

## Pick a visual

| Question | Visual | Avoid |
|---|---|---|
| How far along is each part? | Horizontal progress bars with "done/total" labels | Gauges, a single donut |
| How did a number change over time? | Line or column chart with a labelled time axis | Two y-axes |
| What happens when? | Timeline or simple Gantt rows with today marked | Dense calendars |
| Which is bigger? | Bars sorted by value | Pie charts with more than three slices |
| Share of a whole | One stacked bar | 3D effects |
| How do steps or systems connect? | Boxes and arrows drawn in SVG, read left to right | Crossing lines |
| Two criteria at once (impact and effort) | 2×2 matrix with items placed as labelled dots | Unlabelled points |
| State of many items | Grid of labelled chips grouped by state | Colour-only cells |

Label values directly on the marks, start bar axes at zero, and add a sentence that states the takeaway above the chart.

## Build HTML for the Plandillo sandbox

HTML pages, attachments and `html-preview` blocks run in a sandboxed frame with scripts allowed and no network:

- **Self-contained.** Inline all CSS, JavaScript and data. External scripts, stylesheets, fonts, `fetch` and remote images are blocked and fail silently. Use the system font stack. Draw charts with inline SVG or a canvas and plain JavaScript; a library must be pasted inline and counts toward the size limit (2 MiB for HTML, 200 KiB for a Markdown page).
- **No app privileges.** There is no storage, cookies, form submission, pop-ups or navigation: links inside the frame do not open. Put links in the Markdown around the block or on the page.
- **Size and theme.** The frame grows with the content up to 8192 px, then scrolls, and starts on a white background. Set your own background and text colours for light and dark (`prefers-color-scheme`), keep contrast at 4.5:1 or more, and lay out for 320–1100 px widths with wrapping grids instead of fixed widths.
- **Data first.** Keep the data in one constant at the top and render everything from it, including the totals, so an update changes only the data. Escape text that came from tasks before inserting it as HTML.
- **Readable without interaction.** The first view shows the values; hover, tabs and filters only add detail. Give each chart a title and a text alternative (`role="img"` with `aria-label`, or a small table).

```html
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Release 1.2: readiness</title>
<style>
:root{--bg:#fff;--fg:#1f2328;--mute:#59636e;--line:#d1d9e0;--ok:#1a7f37;--warn:#9a6700;--bad:#cf222e}
@media (prefers-color-scheme:dark){:root{--bg:#0d1117;--fg:#e6edf3;--mute:#9198a1;--line:#3d444d;--ok:#3fb950;--warn:#d29922;--bad:#f85149}}
body{margin:0;padding:20px;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
h1{font-size:20px;margin:0}.meta{color:var(--mute);margin:2px 0 16px}
.row{display:grid;grid-template-columns:minmax(90px,170px) 1fr auto;gap:10px;align-items:center;margin:8px 0}
.track{height:10px;background:var(--line);border-radius:5px;overflow:hidden}.fill{height:100%}
</style></head><body>
<h1>Release 1.2: readiness</h1><p class="meta" id="meta"></p><div id="rows"></div>
<script>
const asOf = "2026-11-26";
const areas = [
  {name: "CLI core", done: 8, total: 8, state: "done"},
  {name: "Packages", done: 2, total: 6, state: "in progress"},
  {name: "Signing", done: 0, total: 2, state: "blocked"},
];
const colour = {"done": "var(--ok)", "in progress": "var(--warn)", "blocked": "var(--bad)"};
const esc = s => String(s).replace(/[&<>"]/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"})[c]);
const done = areas.reduce((n, a) => n + a.done, 0), total = areas.reduce((n, a) => n + a.total, 0);
document.getElementById("meta").textContent = `${done} of ${total} steps (${Math.round(100 * done / total)}%) · as of ${asOf}`;
document.getElementById("rows").innerHTML = areas.map(a => `<div class="row"><span>${esc(a.name)}</span>
  <div class="track" role="img" aria-label="${esc(a.name)}: ${a.done} of ${a.total}, ${a.state}">
  <div class="fill" style="width:${100 * a.done / a.total}%;background:${colour[a.state]}"></div></div>
  <span>${a.done}/${a.total} · ${esc(a.state)}</span></div>`).join("");
</script></body></html>
```

## Check before you finish

- The first screen answers the page's question, with a date and the next action.
- Every number has its unit or denominator; totals match the rows.
- Each state shows a word, and markers follow the legend.
- Tasks are linked by code, pages by `[[Title]]`; nothing copied will go stale unnoticed.
- HTML has no external address, reads in dark mode and at 320 px, and shows values without hovering.
- After `write_page` or `publish_html`, read the page or document back once to confirm the version you meant is saved. When editing an existing page, keep its format and pass `expected_updated_at` or `expected_version`.

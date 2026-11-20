# Frontend layout and lineage presentation — Reference

> Maintenance reference for the shell, the Settings section, the OpenLineage pages and the lineage canvas (`frontend/src/{layouts,components/layout,pages/settings,pages/openlineage,components/lineage,lib,utils}/`). Code rules live in [frontend/AGENTS.md](../../frontend/AGENTS.md) and are not repeated here; the layout gate is [scripts/audit-ui-layout.mjs](../../frontend/scripts/audit-ui-layout.mjs).

## What it is

Three conventions combine into one content-first contract: a **skeleton** with no desktop top bar, six sidebar entries, Settings demoted to in-page navigation and one `PageHeader` per product page; an **OpenLineage information architecture** split into a directory layer (choose an object), a relation layer (explain dependencies) and an evidence layer (build trust); and a **lineage canvas** whose nodes are keyed by instance and edges by the writer that stored them.

## Decisions

| Decision | Reason |
| --- | --- |
| No desktop top bar: brand and collapse live at the top of the sidebar, user/language/theme at its bottom (Profile was removed because it had no implementation); below `lg` a 48px bar holds the drawer trigger and brand. Six top-level sidebar entries: Home, Explain SQL, Data, OpenLineage, Data Quality, Settings, with Settings as one entry plus 15 in-page destinations | A 56px bar for the brand and two controls cost more per page than it gave; 19 permanently visible rows pushed navigation ahead of content, and low-frequency configuration should not own sidebar rows |
| One `PageHeader` primitive (optional breadcrumb, eyebrow, title, description, actions slot) and one tab system; a detail page renders no second page-level H1 (the entity name is the title and the breadcrumb locates it) and no tab hosts another | Identical type scale and action placement everywhere; removes the H1 + breadcrumb + card-title repetition and the nested Table Details/History / History/Version Diff layers |
| One page scrolling container with sticky page header/tabs; only a full-height workbench may add one content-pane scroller. Ordinary pages never nest scrollers | The previous shape had card + `main` + window scroll contexts. A workbench whose content is a window of its own — the Explain SQL panes, the provider list, the agent transcript — needs a fixed header and a scrolling body; that pane is the work area, not a second page scroller, and the shell scroller must not scroll while it is open |
| List/form pages capped and centered at 1400px; detail and wide-table pages opt out with `contentWidth: "full"`; the Settings navigation is not centered but pinned to the content area's left edge, with only the reading column (860px) capped | A centered column's x depends on the scroller's content-box width, so widening the window or showing a scrollbar pushed the navigation around |
| OpenLineage product names are Jobs / Datasets / Events / Overview (task/run stay internal); directory detail opens in a drawer to keep context, run detail keeps a full page | Avoids a large backend rename; the run detail carries the evidence layer |

## Skeleton and layout mechanics

| Mechanism | Implementation |
| --- | --- |
| Single scroll container | `DefaultLayout` owns the page scroller (`<main class="relative min-h-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]">`). `relative` is part of the contract: radix-vue renders an absolutely positioned hidden native `<select>` per Select, and without a positioned ancestor it anchors to the initial containing block and inflates the document (an empty second scrollbar under long forms). `DefaultLayout` also scrolls `main` back to top when `route.path` changes (ignoring the query, so in-page state changes do not yank the view). A full-height workbench sets `h-full min-h-0` on its page root, so `main` has nothing to scroll and the workbench may own one content pane (`overflow-y-auto`) plus a fixed header; the pane must be filled by its flex parent or `main` starts scrolling again and the window gets a second scrollbar |
| Width | Only when `route.meta.contentWidth !== "full"` does the shell wrap content in `mx-auto w-full max-w-[1400px]`; full-width pages get no wrapper so the full-height workbenches keep `h-full` |
| Settings section | `SettingsLayout` pins the navigation with `md:sticky md:top-0 md:w-52 md:self-start` (`self-start` is required, or a stretched flex item has no room to stick) and caps the reading column at `max-w-[860px]`; a page whose meta sets `settingsContentWidth: "full"` opts out; `SettingsNav` lists 15 destinations |
| Sidebar | `AppSidebar` builds six top-level entries; the bottom holds `NotificationBell` and `UserMenu`; the collapsed rail uses separators where groups had titles. `AppHeader` renders below `lg` only (48px, drawer trigger plus brand); on desktop the sidebar owns brand and account controls |
| Page header | `PageHeader` offers a breadcrumb slot, an optional 11px uppercase eyebrow, the 24px `h1`, a description and an actions slot that wraps under the title on narrow screens; every product page imports it (auth, consent and redirect pages excepted) |
| Routing | Settings children hang off `/settings` (which redirects to `GeneralSettings`); the OpenLineage parent route sets `contentWidth: "full"` once so no child can drift out of step |

## OpenLineage pages

| Layer | Route → page | Notes |
| --- | --- | --- |
| Overview | `/openlineage/overview` → `OpenLineageOverviewPage` | Metric cards plus recent runs and active jobs, aggregated from existing list RPCs; no new backend RPC; cards count what the page fetched because list RPCs return no total; zero ingested events becomes an onboarding empty state |
| Directory | `/openlineage/jobs` → `OpenLineageRunsPage` (route name `OpenLineageTasks`); `/openlineage/datasets` → `OpenLineageDatasetsPage` with `OpenLineageDatasetDetailDrawer` (a dialog-form drawer that keeps the directory context) | Aggregated jobs and datasets |
| Investigation | `/openlineage/jobs/:guid` → `OpenLineageTaskDetailPage` | Job detail |
| Evidence and relation | `/openlineage/events` → `OpenLineageEventsPage`; `/events/:guid` → `OpenLineageRunDetailPage` (raw JSON, facets, Airflow link, related job/dataset); `/column-lineage/:guid+` → `OpenLineageColumnLineagePage` (the URL carries `column`; repeated segments keep object names containing `/` alive) | Evidence layer plus field relations |

The shared `OpenLineageSectionHeader` is a thin wrapper over `PageHeader`'s eyebrow. Cross-page context is real: the `from` query round-trips between job detail, run detail and column lineage (`goBack()` prefers it), and list filters are written into the URL and read back (`queryFilters()` ← `route.query`, then `router.replace({ query })`), including `lineageOnly`, `columnLineageOnly` and `search`.

## Design tokens

Colors, radii, shadows and type sizes come only from the CSS variables in `main.css` and the `tailwind.config.js` mapping. A raw palette class (`text-blue-600`, `bg-amber-50`) is a regression: borderline in light theme, broken in dark.

- The interaction accent (`--primary` plus hover/subtle/foreground variants) belongs to interactive elements only: primary buttons, links, active navigation, selected state and focus rings. It is not a decoration color.
- Semantic tokens (`--success`, `--warning`, `--info`, `--danger`, each with a subtle background) belong to status badges, alerts, sync results and destructive actions; use the `Badge` `variant` instead of composing colors by hand, and have the badge say the status in words so it does not depend on color.
- Chart status fills (`--chart-pass`/`-warn`/`-fail`/`-error`, one set per theme) are for large areas such as trend bars. The badge-sized semantic colors look muddy when poured into a chart; chart variants are retuned for area and remain distinguishable.
- Surfaces are `--background`/`--card`/`--muted`/`--border`/`--input`, with `--radius` and `shadow-e1` (card), `e2` (hover, popover) and `e3` (dialog) expressing depth. Prefer elevation to heavy borders. The dark theme raises `--card` above `--background`; without that gap card edges disappear.
- Type: body 14px, page title 24px (`PageHeader`), card title 16px from `CardTitle` (an `h2`), secondary 12px, micro 11px. Pages do not locally override the card-title size.
- Tables: 12px muted uppercase header, cells with `tabular-nums`, and the primary cell is a real link or button (keyboard reachable, focus ring) rather than a `cursor-pointer` fake row. Inputs: a visible `border-input`; focus uses `border-ring` plus `ring-2 ring-ring/25`; validation failure drives the danger border through `aria-invalid`.
- States: `EmptyState` (title, description, actions in the default slot) and `PageState` (`#skeleton` while loading, `#error` with an action on failure); an empty state always names the next step.
- Time: `formatRelativeTime` for display and `formatDateTime` inside a `title` tooltip; never a raw ISO string in a list.
- Narrow screens: below `md`, hide low-value columns and merge status/time into the primary cell while keeping the action column reachable; do not fall back to dragging the table sideways.

The gates cover what is measurable, not taste: `biome:check` rejects misordered class names, `lint` (`@intlify`) rejects unused or missing keys, and `audit:ui` rejects thick first-screen chrome, nested scrollers, document overflow, clipped or overflowing table cells, heading skips, a missing or duplicate `h1` and console errors. Pixel-level dark contrast and hover states still need a screenshot review.

## Lineage encoding

| Question | Signal | Presentation |
| --- | --- | --- |
| Instance ownership | The first GUID segment is the instance resource id; instance names come from `ListInstances`, cached in `useInstanceStore` (the lineage response does not repeat them) | 4px left border, header dot and header instance name, same instance same color; the title is the object name only, the second line carries the path between instance and object (database, plus schema when the engine has one), and the full `instance · path.object` sits in the tooltip; the MiniMap uses the same palette |
| Which writer stored the edge | `LineageRelation.metaType`: `OPENLINEAGE` is ingestion, `APPLICATION_SQL` is an application reporting the statements it runs, anything else is an analyzer reading an object's SQL definition. An object pair can carry relations from more than one writer, hence the fourth origin, `mixed` | SQL solid blue, OpenLineage dashed purple, application SQL dash-dot amber; `mixed` is a third color and dash pattern; every edge has an arrowhead so direction never depends on animation |

- The instance stripe is `border-l-4`, not an absolutely positioned block: a node must not clip its overflow, or the connection handles sitting astride the border and half their drag target get cut off (`LineageNode`). Line styles back up the colors (`LINEAGE_ORIGIN_DASH` in `lineageOrigin.ts`), so the origin survives a grayscale print or a color-blind reader.
- The legend is also the writer filter and starts collapsed (`collapsed = ref(true)`): expanded it covers canvas nodes, collapsed it still answers which color and dash mean which writer. It lists instances (name and object count) and each writer (dash sample and edge count); clicking a writer hides its edges. `mixed` is a bundle with no standalone filter semantics, so it renders as an unclickable row. The legend keeps at least one writer enabled, because an empty filter would leave the canvas with no row to click back, and switching object or Reset restores every writer, or the new object looks lineage-free.
- The palette (`--lineage-scope-1..6`, `--lineage-sql`/`-openlineage`/`-appsql`/`-mixed`) lives in `main.css`. Instance colors are assigned by sorting instance id and then external namespace with `compareScopeKeys` (code-point order, not locale order), so every browser language gets the same colors, and instances already on screen keep their color when an external dataset expands the graph.

## Node height: guess, then measure

`nodeHeight()` in `lineageGraph.ts` can only guess. How many lines the action row wraps to depends on the locale and on which actions the node offers, so a node offering all three came out taller than its 120px slot and crowded the node below. `LineageGraphPage` therefore stacks once from the guess, measures each drawn `.vue-flow__node[data-id]` with `offsetHeight` (a layout measurement, so canvas zoom cannot distort it), stores it in `renderedHeights` and restacks. `lib/lineageLayout.ts` owns the bounded convergence (`MAX_SETTLE_PASSES = 4`; one or two passes normally suffice, since positions do not feed back into heights). `measureDrawnNodes()` retries up to 10 times with `nextTick` and 25ms and returns null when the nodes are not all drawn, in which case the guessed layout stays and rendering is never blocked. Expanding a field list invalidates that node's measured height and returns it to the guess before re-measuring, while other nodes keep their cached heights. Vue Flow's own `node.dimensions` cannot be a layout input: it does not follow a card that grows in place.

## Invariants

| Rule | Breaking consequence |
| --- | --- |
| No top bar at `lg` and above; every page title goes through `PageHeader`; heading levels never skip | 48–56px lost per page; `audit:ui` fails on `h1Count !== 1` or a heading skip (`CardTitle` is an `h2` for this reason) |
| One page scroll container; no nested scroller except a single content pane inside a full-height workbench (`h-full` root, pane `min-h-0 flex-1 overflow-y-auto`); the window itself never scrolls; no hard-coded viewport reservations such as `max-h-[calc(100vh-16rem)]` | Three scroll contexts; an absolutely positioned hidden control escapes the scroller and inflates the document (a blank second scrollbar); `audit:ui` fails on `nestedScrollers !== 0` (registered routes have none) or `documentOverflow > 0` |
| The scroller spans the full remaining width while an inner wrapper caps and centers the content; `contentWidth: "full"` only for wide tables and full-height workbenches | A capped scroller parks the scrollbar mid-screen and leaves a dead gutter; the opposite over-long reading lines or a horizontal table scrollbar |
| Settings navigation stays pinned to the content area's left edge with only the reading column capped; `scrollbar-gutter: stable` stays; sticky navigation keeps `md:top-0 md:self-start` | Widening the window or a scrollbar shifts the navigation; a stretched flex item has no sticky room and the navigation drops below its title |
| Node color by instance, edge color by writer, a line style in addition to color, unhighlighted edges never below 0.85 opacity; the legend starts collapsed and instance colors sort by code point (`compareScopeKeys`) | Same-named objects collapse into one color and two writers look alike, disappearing in grayscale; an expanded legend covers nodes so they can be neither seen nor clicked, and locale-dependent sorting gives different colors per browser language |
| Node heights are guessed then measured; a measured height expires when its field list toggles; Vue Flow `dimensions` is not a layout input | Nodes overlap as the row gap is silently consumed |
| Cross-page navigation keeps `from` and list filters in the URL | The investigation trail breaks and filters are lost on return |

## Failure modes

| Scenario | Behavior |
| --- | --- |
| First measurement runs before the browser has painted, or a field list expands mid-flight | `measureDrawnNodes` retries up to 10 times (`nextTick` + 25ms) and otherwise returns null, keeping the guessed layout; a toggled field list invalidates only that node's measured height, which is re-guessed and re-measured while the other nodes keep their cached heights |
| The lineage response carries no instance information | Names come from the `useInstanceStore` cache via `ensureLoaded()`; the lineage response does not repeat them |
| Overview has no ingested events, or a list RPC has no total | Onboarding empty state; cards count what the page fetched instead of inventing a total |

## Where things live

- Shell: `DefaultLayout`, `PageHeader`, `AppSidebar`, `AppHeader`, `UserMenu`, `SettingsNav`, `SettingsLayout`; routes in `router/index.ts`.
- OpenLineage: `pages/openlineage/`, `components/openlineage/`. Lineage: `utils/lineageAsset.ts`, `lib/lineageOrigin.ts`, `lib/lineageGraph.ts`, `lib/lineageLayout.ts`, `lib/openlineageRun.ts`, `components/lineage/`, `pages/LineageGraphPage.vue` plus `composables/useLineageGraphData.ts` (graph loading) and `useLineageSelection.ts` (selection, including `markSelectedColumn`), `components/metadata/TableLineageSection.vue`.
- Tokens and theme: `assets/styles/main.css`, `store/modules/app.ts` (`theme`, `collapsedSections`, `locale`); `applyTheme` runs before the first mount. There is no feature flag: layout is driven by `route.meta.contentWidth` and `settingsContentWidth`.
- Chat and workbench panes: `composables/useChatScroll.ts` (follow the newest, stop when the reader scrolls back, keep the reading position when history is inserted above), `pages/data-quality/QualityAgentSessionPage.vue` (the work-session transcript window), `components/data-quality/QualityAgentRunState.vue` (the run-state column and its narrow-screen disclosure), `pages/data-quality/QualityAgentPage.vue` (owner conversation reusing the same follower), `components/common/AsyncMarkdown.ts` (`preloadMarkdown` warms the lazy renderer so the first transcript paint has its final height).
- Gate and tests: `scripts/audit-ui-layout.mjs` via `pnpm --dir frontend audit:ui` speaks CDP directly (no puppeteer/playwright), measures 26 routes for table top, first data row, h1 count, nested scrollers, clipped cells, horizontal overflow, heading skips and console errors, and exits 0 when all routes are within budget, 1 on a regression, 2 when it cannot run. Unit tests are `utils/lineageAsset.test.ts`, `utils/lineageTokens.test.ts` (pins the `main.css` token names so an invalid `hsl()` cannot silently erase every edge), `lib/{lineageOrigin,lineageGraph,lineageLayout,openlineageRun,lineageTrail}.test.ts`, `components/layout/AppSidebar.test.ts`, and `pages/openlineage/*.test.ts`; `test:coverage` enforces 95% lines and 85% branches for `src/lib` and `src/utils`.

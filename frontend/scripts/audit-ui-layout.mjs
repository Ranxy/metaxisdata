#!/usr/bin/env node
/**
 * Layout regression audit.
 *
 * Measures the content-first criteria from `spec/frontend_ux_redesign.md`
 * against a running dev server, so a later change cannot quietly push the page
 * chrome back up. It reports, per route:
 *
 *   - firstDataRowY / pct   how much of the viewport is spent before the first
 *                           row of real data
 *   - h1Count               exactly one page heading per route
 *   - nestedScrollers       pages must not nest their own scroll containers
 *   - consoleErrors         runtime errors seen while the route loaded
 *
 * It speaks the Chrome DevTools Protocol directly over the built-in WebSocket
 * and fetch, so it needs no browser-automation dependency.
 *
 * Usage:
 *   # terminal 1: the app
 *   pnpm --dir frontend dev
 *   # terminal 2: a browser exposing CDP (any Chrome/Chromium)
 *   google-chrome-stable --headless=new --remote-debugging-port=9222 about:blank
 *   # terminal 3:
 *   AUDIT_COOKIE="access-token=<jwt>" pnpm --dir frontend audit:ui
 *
 * Options:
 *   --base-url <url>     app under test          (default http://localhost:3000)
 *   --cdp-url <url>      browser CDP endpoint    (default http://127.0.0.1:9222)
 *   --width/--height     viewport                (default 1440x900)
 *   --json               machine-readable output
 *   --cookie <cookie>    session cookie; falls back to $AUDIT_COOKIE
 *
 * Exit codes: 0 all routes within budget, 1 a route regressed, 2 the audit
 * could not run (no browser, no session).
 */
import process from "node:process";

const DEFAULT_BASE_URL = "http://localhost:3000";
const DEFAULT_CDP_URL = "http://127.0.0.1:9222";

/**
 * Budgets are on `tableTopY`: how far down the viewport the chrome above the
 * table pushes it. The table's own header row sits ~48px further down, so
 * `firstDataRowY` is reported next to it for context but is not the budget —
 * a header row is data presentation, not chrome.
 */
const ROUTES = [
  { path: "/", name: "home" },
  { path: "/metadata", name: "metadata root", tableTopMaxY: 220 },
  {
    path: "/metadata/ms-sql-1%2Fmetaxis_demo%2Fsales",
    name: "metadata tables",
    tableTopMaxY: 220,
  },
  {
    path: "/metadata/ms-sql-1%2Fmetaxis_demo%2Fsales%2Fcustomer?metaType=4",
    name: "metadata table detail",
    tableTopMaxY: 280,
  },
  { path: "/instances", name: "instances", tableTopMaxY: 160 },
  { path: "/databases", name: "databases", tableTopMaxY: 160 },
  { path: "/manual-sql", name: "manual sql", tableTopMaxY: 160 },
  { path: "/explain-sql", name: "explain sql" },
  { path: "/openlineage/overview", name: "openlineage overview" },
  { path: "/openlineage/jobs", name: "openlineage jobs" },
  { path: "/settings/general", name: "settings general" },
  { path: "/settings/audit-logs", name: "settings audit logs", tableTopMaxY: 290 },
];

const MEASURE = `(() => {
  const main = document.querySelector('main');
  if (!main) return { missing: true, pathname: location.pathname };
  const row = main.querySelector('tbody tr');
  const table = main.querySelector('table');
  const anchor = row || table;
  const y = anchor ? Math.round(anchor.getBoundingClientRect().top) : null;
  const tableY = table ? Math.round(table.getBoundingClientRect().top) : null;
  return {
    pathname: location.pathname,
    firstDataRowY: y,
    pct: y === null ? null : Math.round((y / innerHeight) * 100),
    tableTopY: tableY,
    viewportW: innerWidth,
    viewportH: innerHeight,
    h1Count: main.querySelectorAll('h1').length,
    headingLevels: [...main.querySelectorAll('h1, h2, h3, h4, h5, h6')].map((h) =>
      Number(h.tagName.slice(1))
    ),
    nestedScrollers: [...main.querySelectorAll('*')].filter(
      (el) => el.scrollHeight > el.clientHeight + 4
    ).length,
    mainWidth: Math.round(main.getBoundingClientRect().width),
    scrollWidth: Math.round(main.scrollWidth),
    gutter: getComputedStyle(main).scrollbarGutter,
  };
})()`;

// The first data row only moves when the chrome above it settles, so sampling a
// fixed delay after load would race a slow fetch.
const ANCHOR_Y = `(() => {
  const main = document.querySelector('main');
  if (!main) return null;
  const anchor = main.querySelector('tbody tr') || main.querySelector('table');
  return anchor ? Math.round(anchor.getBoundingClientRect().top) : null;
})()`;

function parseArgs(argv) {
  const options = {
    baseUrl: process.env.AUDIT_BASE_URL || DEFAULT_BASE_URL,
    cdpUrl: process.env.AUDIT_CDP_URL || DEFAULT_CDP_URL,
    width: 1440,
    height: 900,
    json: false,
    cookie: process.env.AUDIT_COOKIE || "",
  };
  for (let i = 0; i < argv.length; i += 1) {
    const flag = argv[i];
    const value = argv[i + 1];
    if (flag === "--json") options.json = true;
    else if (flag === "--base-url") [options.baseUrl, i] = [value, i + 1];
    else if (flag === "--cdp-url") [options.cdpUrl, i] = [value, i + 1];
    else if (flag === "--cookie") [options.cookie, i] = [value, i + 1];
    else if (flag === "--width") [options.width, i] = [Number(value), i + 1];
    else if (flag === "--height") [options.height, i] = [Number(value), i + 1];
    else if (flag === "--help" || flag === "-h") options.help = true;
    else throw new Error(`unknown argument: ${flag}`);
  }
  return options;
}

const USAGE = `Usage: node scripts/audit-ui-layout.mjs [--base-url url] [--cdp-url url]
       [--width px] [--height px] [--cookie cookie] [--json]

Needs the app running and a browser exposing CDP. See the file header.`;

class Cdp {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 0;
    this.pending = new Map();
    this.listeners = new Set();
    socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (message.id !== undefined) {
        const entry = this.pending.get(message.id);
        if (!entry) return;
        this.pending.delete(message.id);
        if (message.error) entry.reject(new Error(message.error.message));
        else entry.resolve(message.result);
        return;
      }
      for (const listener of this.listeners) listener(message);
    });
  }

  send(method, params = {}, sessionId) {
    const id = (this.nextId += 1);
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.socket.send(
        JSON.stringify({ id, method, params, ...(sessionId && { sessionId }) })
      );
    });
  }

  on(listener) {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Releases the socket so the process can exit once the audit is done. */
  close() {
    this.listeners.clear();
    this.pending.clear();
    this.socket.close();
  }
}

async function connect(cdpUrl) {
  let version;
  try {
    version = await (await fetch(`${cdpUrl}/json/version`)).json();
  } catch {
    throw Object.assign(
      new Error(
        `no DevTools endpoint at ${cdpUrl}. Start a browser with --remote-debugging-port.`
      ),
      { fatal: true }
    );
  }
  const socket = new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", () =>
      reject(
        Object.assign(new Error("DevTools socket failed to open"), {
          fatal: true,
        })
      )
    );
  });
  return new Cdp(socket);
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const LOAD_TIMEOUT_MS = 10000;

/**
 * A section heading that jumps from h1 straight to h3 breaks screen-reader
 * outline navigation, so the audit treats it as a regression.
 */
function firstHeadingSkip(levels = []) {
  let previous = 0;
  for (const level of levels) {
    if (previous !== 0 && level > previous + 1) {
      return { from: previous, to: level };
    }
    previous = level;
  }
  return null;
}

function createRunner(cdp, sessionId) {
  const consoleErrors = [];

  const stopListening = cdp.on((message) => {
    if (message.sessionId !== sessionId) return;
    if (message.method === "Runtime.exceptionThrown") {
      consoleErrors.push(
        message.params.exceptionDetails?.exception?.description ??
          message.params.exceptionDetails?.text ??
          "uncaught exception"
      );
      return;
    }
    if (
      message.method === "Runtime.consoleAPICalled" &&
      message.params.type === "error"
    ) {
      consoleErrors.push(
        message.params.args
          .map((arg) => arg.value ?? arg.description ?? "")
          .join(" ")
      );
    }
  });

  async function evaluate(expression) {
    const result = await cdp.send(
      "Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true },
      sessionId
    );
    if (result.exceptionDetails) {
      throw new Error(
        result.exceptionDetails.exception?.description ?? "evaluation failed"
      );
    }
    return result.result.value;
  }

  async function waitForApp(timeoutMs = 15000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      // The authed shell renders <main>; /login uses the auth layout and has
      // none, so treat it as terminal instead of waiting out the timeout on
      // every route.
      const ready = await evaluate(
        `(!!document.querySelector('main') && !document.querySelector('main .animate-spin'))
          || location.pathname === '/login'`
      );
      if (ready) return true;
      await sleep(150);
    }
    return false;
  }

  async function waitForSettledAnchor(timeoutMs = 6000) {
    const deadline = Date.now() + timeoutMs;
    let previous;
    let stable = 0;
    while (Date.now() < deadline) {
      const y = await evaluate(ANCHOR_Y);
      if (y !== null && y === previous) {
        stable += 1;
        if (stable >= 3) return true;
      } else {
        stable = 0;
      }
      previous = y;
      await sleep(120);
    }
    return false;
  }

  async function visit(url) {
    consoleErrors.length = 0;
    // Bounded: a page that never fires a load event (a hard redirect, a
    // same-document navigation) must not hang the whole audit.
    let stop = () => {};
    const loaded = new Promise((resolve) => {
      stop = cdp.on((message) => {
        if (
          message.sessionId === sessionId &&
          message.method === "Page.loadEventFired"
        ) {
          resolve();
        }
      });
    });
    await cdp.send("Page.navigate", { url }, sessionId);
    await Promise.race([loaded, sleep(LOAD_TIMEOUT_MS)]);
    stop();
    await waitForApp();
    await waitForSettledAnchor();
  }

  return { evaluate, visit, stop: stopListening, consoleErrors };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log(USAGE);
    return 0;
  }

  const cdp = await connect(options.cdpUrl);
  const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank" });
  const { sessionId } = await cdp.send("Target.attachToTarget", {
    targetId,
    flatten: true,
  });
  await cdp.send("Page.enable", {}, sessionId);
  await cdp.send("Runtime.enable", {}, sessionId);
  await cdp.send(
    "Emulation.setDeviceMetricsOverride",
    {
      width: options.width,
      height: options.height,
      deviceScaleFactor: 1,
      mobile: false,
    },
    sessionId
  );

  const runner = createRunner(cdp, sessionId);

  if (options.cookie) {
    await runner.visit(`${options.baseUrl}/login`);
    await runner.evaluate(
      `document.cookie = ${JSON.stringify(`${options.cookie}; path=/; SameSite=Lax`)}, true`
    );
  }

  const results = [];
  let unauthenticated = false;
  for (const route of ROUTES) {
    const url = `${options.baseUrl}${route.path}`;
    await runner.visit(url);
    const measured = await runner.evaluate(MEASURE);
    const failures = [];

    if (measured.pathname === "/login") {
      unauthenticated = true;
      failures.push("redirected to /login — pass a session cookie via --cookie");
    } else if (measured.missing) {
      failures.push(`no <main> element rendered (at ${measured.pathname})`);
    } else {
      if (
        measured.viewportW !== options.width ||
        measured.viewportH !== options.height
      ) {
        failures.push(
          `viewport is ${measured.viewportW}x${measured.viewportH}, expected ${options.width}x${options.height}`
        );
      }
      if (measured.h1Count !== 1) {
        failures.push(`expected exactly 1 <h1>, found ${measured.h1Count}`);
      }
      const skip = firstHeadingSkip(measured.headingLevels);
      if (skip) {
        failures.push(
          `heading level skips from h${skip.from} to h${skip.to}`
        );
      }
      if (measured.nestedScrollers !== 0) {
        failures.push(`${measured.nestedScrollers} nested scroll container(s)`);
      }
      if (
        route.tableTopMaxY !== undefined &&
        (measured.tableTopY === null || measured.tableTopY > route.tableTopMaxY)
      ) {
        failures.push(
          `table top at y=${measured.tableTopY} (budget ${route.tableTopMaxY})`
        );
      }
      if (measured.scrollWidth > measured.mainWidth + 1) {
        failures.push(
          `horizontal overflow: ${measured.scrollWidth} > ${measured.mainWidth}`
        );
      }
    }
    if (runner.consoleErrors.length > 0) {
      failures.push(`${runner.consoleErrors.length} console error(s)`);
    }

    results.push({
      ...route,
      ...measured,
      consoleErrors: [...runner.consoleErrors],
      failures,
    });

    // Every remaining route would fail the same way; stop and report once.
    if (unauthenticated) break;
  }

  runner.stop();
  await cdp.send("Target.closeTarget", { targetId });
  cdp.close();

  const failed = results.filter((result) => result.failures.length > 0);

  if (options.json) {
    console.log(JSON.stringify({ options, results, failed: failed.length }, null, 2));
  } else {
    console.log(
      `Layout audit — ${options.baseUrl} @ ${options.width}x${options.height}\n`
    );
    for (const result of results) {
      const marker = result.failures.length > 0 ? "FAIL" : "ok  ";
      const budget =
        result.tableTopMaxY === undefined
          ? ""
          : ` / budget ${result.tableTopMaxY}`;
      const position =
        result.tableTopY === null
          ? "no data table"
          : `table top y=${result.tableTopY}${budget} · first row y=${result.firstDataRowY}`;
      console.log(`${marker} ${result.name.padEnd(24)} ${position}`);
      for (const failure of result.failures) {
        console.log(`       ↳ ${failure}`);
      }
    }
    console.log(
      `\n${results.length - failed.length}/${results.length} routes within budget`
    );
  }

  if (unauthenticated) {
    console.error(
      "\nThe audit has no session. Pass one with --cookie \"access-token=<jwt>\" or $AUDIT_COOKIE."
    );
    return 2;
  }
  return failed.length > 0 ? 1 : 0;
}

main()
  .then((code) => {
    process.exitCode = code;
  })
  .catch((error) => {
    console.error(error.fatal ? `\n${error.message}` : error);
    process.exitCode = 2;
  });

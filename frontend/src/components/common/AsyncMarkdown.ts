import { defineAsyncComponent, defineComponent, h } from "vue";
import { useI18n } from "vue-i18n";

/** A quiet placeholder while the renderer's chunk downloads. */
const MarkdownLoading = defineComponent({
  name: "MarkdownLoading",
  setup: () => () =>
    h("div", {
      class:
        "h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-foreground",
      "aria-hidden": "true",
    }),
});

/**
 * A lazy chunk can fail (an old tab after a deploy asks for a file that no
 * longer exists); the panel says so instead of staying blank forever.
 */
const MarkdownFailure = defineComponent({
  name: "MarkdownFailure",
  setup() {
    const { t } = useI18n();
    return () =>
      h("p", { class: "text-sm text-destructive" }, t("error.unknown"));
  },
});

/**
 * The markdown renderer, loaded with the first panel that shows an explanation.
 *
 * `markstream-vue` is about a megabyte of renderer plus its CSS. It used to be a
 * static dependency of the data-quality workbench and the ExplainSQL page, so
 * opening a rule workspace downloaded it even for a reader who never asked for
 * an AI explanation. Monaco is kept out of the static graph the same way
 * (`monaco-editor/lazy-editor.ts`); the stylesheet travels with this chunk
 * instead of `main.ts` so a page that never renders markdown never pays for it.
 */
async function loadMarkdownRenderer() {
  await import("markstream-vue/index.css");
  const { default: MarkdownRender } = await import("markstream-vue");
  return MarkdownRender;
}

/**
 * Downloads the renderer without rendering anything.
 *
 * A transcript opens scrolled to its newest message. That message is rendered by
 * this lazy chunk, so a pane that lands before the chunk arrives has to jump
 * again when the placeholder is replaced by real content — briefly showing the
 * wrong scroll position. Warming the chunk while the first page of messages is
 * still in flight shortens that window; the landing loop still covers a cold
 * cache. Failure is swallowed: the component has its own error state, and a
 * preload that could not run must not surface anywhere.
 */
export function preloadMarkdown(): Promise<void> {
  return loadMarkdownRenderer().then(
    () => undefined,
    () => undefined
  );
}

export const AsyncMarkdown = defineAsyncComponent({
  loader: loadMarkdownRenderer,
  delay: 200,
  timeout: 15_000,
  loadingComponent: MarkdownLoading,
  errorComponent: MarkdownFailure,
});

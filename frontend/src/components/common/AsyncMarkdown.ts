import { defineAsyncComponent } from "vue";

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
export const AsyncMarkdown = defineAsyncComponent(async () => {
  await import("markstream-vue/index.css");
  const { default: MarkdownRender } = await import("markstream-vue");
  return MarkdownRender;
});

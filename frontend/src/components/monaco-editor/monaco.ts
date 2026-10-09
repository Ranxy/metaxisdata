// Trimmed Monaco entry: the `monaco-editor` barrel pulls in every language
// definition and the JSON/CSS/HTML/TypeScript language services, while the
// per-language `register` modules go through `_.contribution`, which the barrel
// uses to pull all of them in. The ESM API is composed by hand here instead.
//
// 0.57 maps these subpaths through its own `exports` field (`./*` points into
// `esm/vs/`), so the specifiers below carry no `esm/vs` prefix.
import * as monaco from "monaco-editor/editor/editor.api";

import "monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching.js";
import "monaco-editor/editor/contrib/clipboard/browser/clipboard.js";
import "monaco-editor/editor/contrib/comment/browser/comment.js";
import "monaco-editor/editor/contrib/contextmenu/browser/contextmenu.js";
import "monaco-editor/editor/contrib/cursorUndo/browser/cursorUndo.js";
import "monaco-editor/editor/contrib/find/browser/findController.js";
import "monaco-editor/editor/contrib/folding/browser/folding.js";
import "monaco-editor/editor/contrib/hover/browser/hoverContribution.js";
import "monaco-editor/editor/contrib/indentation/browser/indentation.js";
import "monaco-editor/editor/contrib/linesOperations/browser/linesOperations.js";
import "monaco-editor/editor/contrib/multicursor/browser/multicursor.js";
import "monaco-editor/editor/contrib/wordOperations/browser/wordOperations.js";
import {
  conf as sqlConfiguration,
  language as sqlTokensProvider,
} from "monaco-editor/languages/definitions/sql/sql.js";

// Registers SQL the way its own `register.js` would, minus the lazy loader.
monaco.languages.register({
  id: "sql",
  extensions: [".sql"],
  aliases: ["SQL"],
});
monaco.languages.setLanguageConfiguration("sql", sqlConfiguration);
monaco.languages.setMonarchTokensProvider("sql", sqlTokensProvider);

export * from "monaco-editor/editor/editor.api";
export { monaco };

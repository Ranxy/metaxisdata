// Trimmed Monaco entry: the `monaco-editor` barrel pulls in every basic language
// and the JSON/CSS/HTML/TypeScript language services, while in 0.55.1
// `basic-languages/sql/sql.contribution` pulls in every editor contribution via
// `_.contribution`. The ESM API is composed by hand here instead.
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";

import "monaco-editor/esm/vs/editor/contrib/bracketMatching/browser/bracketMatching.js";
import "monaco-editor/esm/vs/editor/contrib/clipboard/browser/clipboard.js";
import "monaco-editor/esm/vs/editor/contrib/comment/browser/comment.js";
import "monaco-editor/esm/vs/editor/contrib/contextmenu/browser/contextmenu.js";
import "monaco-editor/esm/vs/editor/contrib/cursorUndo/browser/cursorUndo.js";
import "monaco-editor/esm/vs/editor/contrib/find/browser/findController.js";
import "monaco-editor/esm/vs/editor/contrib/folding/browser/folding.js";
import "monaco-editor/esm/vs/editor/contrib/hover/browser/hoverContribution.js";
import "monaco-editor/esm/vs/editor/contrib/indentation/browser/indentation.js";
import "monaco-editor/esm/vs/editor/contrib/linesOperations/browser/linesOperations.js";
import "monaco-editor/esm/vs/editor/contrib/multicursor/browser/multicursor.js";
import "monaco-editor/esm/vs/editor/contrib/wordOperations/browser/wordOperations.js";
import {
  conf as sqlConfiguration,
  language as sqlTokensProvider,
} from "monaco-editor/esm/vs/basic-languages/sql/sql.js";

// Registers SQL the way its contribution would, minus the side effects.
monaco.languages.register({
  id: "sql",
  extensions: [".sql"],
  aliases: ["SQL"],
});
monaco.languages.setLanguageConfiguration("sql", sqlConfiguration);
monaco.languages.setMonarchTokensProvider("sql", sqlTokensProvider);

export * from "monaco-editor/esm/vs/editor/editor.api";
export { monaco };

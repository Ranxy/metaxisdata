import { fileURLToPath } from "node:url";
import vueI18n from "@intlify/eslint-plugin-vue-i18n";
import vueTsEslintConfig from "@vue/eslint-config-typescript";
import pluginVue from "eslint-plugin-vue";
import globals from "globals";
import { eslintUnusedKeyIgnores } from "./scripts/check-vue-i18n.mjs";

/** The locale whose keys define the dynamic-key exemptions. */
const REFERENCE_LOCALE = fileURLToPath(
  new URL("./src/locales/en-US.json", import.meta.url)
);

export default [
  ...pluginVue.configs["flat/essential"],
  ...vueTsEslintConfig({
    extends: ["recommended"],
    supportedScriptLangs: {
      ts: true,
      tsx: true,
    },
    rootDir: import.meta.dirname,
  }),
  ...vueI18n.configs["flat/recommended"],
  {
    // ESLint 10 no longer skips what .gitignore lists, so the coverage report
    // `vitest --coverage` writes has to be named here.
    ignores: ["**/dist/**", "**/node_modules/**", "**/proto-es/**", "**/coverage/**"],
  },
  {
    rules: {
      "no-console": ["error", { allow: ["warn", "error", "debug", "assert"] }],
      "no-debugger": "error",
      "no-empty-pattern": "error",
      "vue/no-ref-as-operand": "error",
      "no-useless-escape": "error",
      "@typescript-eslint/no-empty-interface": "error",
      "@typescript-eslint/no-unused-vars": [
        "error",
        { varsIgnorePattern: "^_", argsIgnorePattern: "^_" },
      ],
      "@intlify/vue-i18n/no-unused-keys": [
        "error",
        {
          src: "./src",
          extensions: [".js", ".vue", ".ts", ".tsx"],
          // The exemptions have a single source: scripts/check-vue-i18n.mjs.
          // This rule cannot follow a key our helpers return and has no prefix
          // support, so the checked list is computed there and imported here.
          ignores: eslintUnusedKeyIgnores(REFERENCE_LOCALE),
          enableFix: false,
        },
      ],
      "@intlify/vue-i18n/no-missing-keys": "error",
      "@intlify/vue-i18n/no-raw-text": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "vue/no-mutating-props": "error",
      "vue/no-unused-components": "error",
      "vue/no-useless-template-attributes": "error",
      "vue/no-undef-components": [
        "error",
        {
          // Regex sources, not literals: eslint-plugin-vue 10.11 types these as
          // strings and compiles each with `new RegExp(pattern)`; ESLint 10
          // rejects a RegExp literal against that schema.
          ignorePatterns: [
            "^heroicons(-solid|-outline)?:",
            "^carbon:",
            "^tabler:",
            "^octicon:",
            "^router-view$",
            "^router-link$",
            "^i18n-t$",
            "^highlight-code-block$",
          ],
        },
      ],
      "vue/multi-word-component-names": "off",
    },
    settings: {
      "vue-i18n": {
        localeDir: "./src/locales/*.json",
        messageSyntaxVersion: "^11.0.0",
      },
    },
  },
  {
    files: ["tailwind.config.js"],
    rules: {
      "@typescript-eslint/no-require-imports": "off",
    },
  },
  {
    // Node CLI helper scripts (i18n checker/sorter) legitimately log to the
    // console and use Node globals.
    files: ["scripts/**/*.mjs"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "no-console": "off",
    },
  },
];
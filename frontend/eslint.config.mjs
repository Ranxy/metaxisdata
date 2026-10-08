import vueI18n from "@intlify/eslint-plugin-vue-i18n";
import vueTsEslintConfig from "@vue/eslint-config-typescript";
import pluginVue from "eslint-plugin-vue";
import globals from "globals";

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
    ignores: ["**/dist/**", "**/node_modules/**", "**/proto-es/**"],
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
          ignores: [
            // The Connect code → sentence table in src/utils/error.ts owns these;
            // the rule has no glob support, and the table's values are checked
            // against the locale schema at compile time.
            "error.aborted",
            "error.alreadyExists",
            "error.canceled",
            "error.dataLoss",
            "error.deadlineExceeded",
            "error.failedPrecondition",
            "error.internal",
            "error.invalidArgument",
            "error.notFound",
            "error.outOfRange",
            "error.permissionDenied",
            "error.resourceExhausted",
            "error.unavailable",
            "error.unimplemented",
            // Dynamically used via showSuccess / handleError / formatError.
            // This rule cannot follow a key held by our own handler, while
            // scripts/check-vue-i18n.mjs does trace it and still fails a typo.
            "databaseManagement.fetchError",
            "databaseManagement.syncError",
            "generalSettings.loadError",
            "manualSqlManagement.deleteError",
            "manualSqlManagement.fetchDatabasesError",
            "manualSqlManagement.fetchError",
            "manualSqlManagement.fetchSchemasError",
            "manualSqlManagement.idCheckError",
            "manualSqlManagement.saveError",
            "metadataBrowser.diffError",
            "metadataBrowser.fetchError",
            "metadataBrowser.historyEventFetchError",
            "metadataBrowser.historyFetchError",
            "metadataBrowser.lineageFetchError",
            "openlineage.overviewFetchError",
            "llmProvider.modelsFetched",
            "llmProvider.fetchModelsError",
            "llmProvider.fetchError",
            "llmProvider.saveError",
            "llmProvider.deleteError",
            "llmProvider.created",
            "llmProvider.updated",
            "llmProvider.deleted",
            "environmentSettings.created",
            "environmentSettings.updated",
            "environmentSettings.deleted",
            "auditLogs.fetchActorsError",
            "auditLogs.fetchError",
            "auditLogs.exportError",
            "generalSettings.saveError",
            "iam.policy.saveFailed",
            "instanceDetail.fetchInstanceError",
            "instanceDetail.syncAllError",
            "instanceDetail.syncSingleError",
            "instanceDetail.updateError",
            "instanceManagement.createError",
            "instanceManagement.fetchError",
            "instanceManagement.deleteError",
            "instanceManagement.restoreError",
            "instanceManagement.testConnectionError",
            "userManagement.createError",
            "userManagement.fetchError",
            "userManagement.deleteError",
            "userManagement.restoreError",
            "userManagement.updateError",
            "register.registerFailed",
            // Dynamically used via relationTypeKey() in src/lib/relationType.ts.
            "metadataBrowser.relationDirect",
            "metadataBrowser.relationIndirect",
            "metadataBrowser.relationJoin",
            "metadataBrowser.relationGroup",
            "metadataBrowser.relationUnion",
            "metadataBrowser.relationIntersect",
            "metadataBrowser.relationExcept",
            "metadataBrowser.relationUnknown",
            // Dynamically used via originLabelKey() in src/lib/lineageOrigin.ts, and
            // via the originHintKey the lineage table rows carry. The rule cannot
            // follow a key a helper returns.
            "lineageGraph.originSql",
            "lineageGraph.originSqlHint",
            "lineageGraph.originOpenlineage",
            "lineageGraph.originOpenlineageHint",
            "lineageGraph.originMixed",
            "lineageGraph.originMixedHint",
            // Dynamically used: the notification catalog in
            // src/lib/notificationText.ts maps a message type to these keys, and
            // the page reaches its own through handleError / showSuccess. The rule
            // cannot follow a key a helper returns, while
            // scripts/check-vue-i18n.mjs does trace the ones the helper spells out.
            "notifications.actionError",
            "notifications.deleteSuccess",
            "notifications.fetchError",
            "notifications.markAllReadSuccess",
            "notifications.openlineageFailedMessage",
            "notifications.openlineageFailedTitle",
            "notifications.openlineageInvalidEventMessage",
            "notifications.openlineageInvalidEventTitle",
            "notifications.openlineageLimitExceededMessage",
            "notifications.openlineageLimitExceededTitle",
            "notifications.openlineageNamespaceUnmappedMessage",
            "notifications.openlineageNamespaceUnmappedTitle",
            "notifications.openlineageScopeMismatchMessage",
            "notifications.openlineageScopeMismatchTitle",
            "notifications.openlineageUnknownMessage",
            "notifications.openlineageUnknownTitle",
            "notifications.schemaSyncBackgroundFailedTitle",
            "notifications.schemaSyncFailedTitle",
            "notifications.schemaSyncSucceededTitle",
            "notifications.schemaSyncSummary",
            "notifications.unknownMessage",
            "notifications.unknownTitle",
          ],
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
          ignorePatterns: [
            /^heroicons(-solid|-outline)?:/,
            /^carbon:/,
            /^tabler:/,
            /^octicon:/,
            /^router-view$/,
            /^router-link$/,
            /^i18n-t$/,
            /^highlight-code-block$/,
          ],
        },
      ],
      "vue/multi-word-component-names": "off",
    },
    settings: {
      "vue-i18n": {
        localeDir: "./src/locales/*.json",
        messageSyntaxVersion: "^9.0.0",
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
<template>
  <PageState loading />
</template>

<script setup lang="ts">
import { onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { getQualityRule } from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import { guidToRouteParams } from "@/utils/guid";

/**
 * The old per-rule URLs (`/data-quality/rules/<name>` and its edit form) land
 * here. A check now lives inside its target's contract workspace, so the
 * redirect resolves the check's target and opens the workspace with the check
 * drawer selected. A missing check falls back to the checks directory.
 */
const route = useRoute();
const router = useRouter();

onMounted(async () => {
  // The old edit URL keeps its intent: the workspace opens the composer on the
  // check instead of the read-only sheet.
  const editing = route.name === "DataQualityRuleEditRedirect";
  const param = route.params.ruleId;
  const parts = Array.isArray(param) ? param : param ? [param] : [];
  const ruleName = parts.join("/");
  try {
    const rule = await getQualityRule(ruleName);
    const target = rule.target;
    if (target?.metaGuid) {
      await router.replace({
        name: "DataQualityTarget",
        params: { targetGuid: guidToRouteParams(target.metaGuid) },
        query: {
          metaType: String(target.metaType),
          check: rule.name,
          ...(editing ? { edit: "1" } : {}),
        },
      });
      return;
    }
  } catch {
    // A stale link is not an error page: the checks directory is the fallback.
  }
  await router.replace({ name: "DataQualityChecks" });
});
</script>

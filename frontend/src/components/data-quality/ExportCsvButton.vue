<template>
  <Button
    variant="outline"
    :disabled="exporting"
    :title="t('dataQuality.exportCsvHint')"
    @click="run"
  >
    <Loader2
      v-if="exporting"
      class="mr-2 h-4 w-4 animate-spin"
    />
    <Download
      v-else
      class="mr-2 h-4 w-4"
    />
    {{ exporting ? t("dataQuality.exportingCsv") : t("dataQuality.exportCsv") }}
  </Button>
</template>

<script setup lang="ts">
import { Download, Loader2 } from "lucide-vue-next";
import { onBeforeUnmount, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import type { CsvDownload } from "@/utils/csv";
import { downloadBlob, fileTimestamp } from "@/utils/csv";
import { formatRowCount } from "@/utils/dataQuality";
import { isAborted } from "@/utils/error";

/**
 * The header's CSV export action.
 *
 * The page owns the filters and hands over `exporter`, so the file always holds
 * every row the current filter set selects, not the page on screen. The button
 * stays disabled until the stream ends, because one export may outlive the
 * default request deadline.
 *
 * Navigating away unmounts the page and with it this button, which aborts the
 * stream: the server stops walking rows, and the canceled request is silent
 * because nothing the reader did failed.
 */
const props = defineProps<{
  /** Runs the export request the page's current filters describe. */
  exporter: (signal: AbortSignal) => Promise<CsvDownload>;
  /** The file's name stem, e.g. "quality-targets"; the timestamp is appended. */
  fileName: string;
}>();

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();
const exporting = ref(false);
/** The in-flight export, so leaving the page can cancel it. */
let controller: AbortController | undefined;

async function run() {
  if (exporting.value) {
    return;
  }
  controller = new AbortController();
  exporting.value = true;
  try {
    const file = await props.exporter(controller.signal);
    downloadBlob(file.blob, `${props.fileName}-${fileTimestamp()}.csv`);
    const count = formatRowCount(file.rowCount, locale.value);
    // A file cut at the server's row ceiling is still a valid CSV, but it is not
    // the whole selection; say so instead of reporting a plain success.
    if (file.truncated) {
      notify.warning(t("dataQuality.exportCsvTruncated", { count }));
    } else {
      notify.success(t("dataQuality.exportCsvDone", { count }));
    }
  } catch (error: unknown) {
    if (!isAborted(error)) {
      handleError(error, "dataQuality.exportCsvFailed");
    }
  } finally {
    controller = undefined;
    exporting.value = false;
  }
}

onBeforeUnmount(() => {
  controller?.abort();
});
</script>

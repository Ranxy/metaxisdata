import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import type { CsvDownload } from "@/utils/csv";
import ExportCsvButton from "./ExportCsvButton.vue";

let clicked: HTMLAnchorElement[] = [];

/** jsdom implements Blob but not the object-URL half of the download path. */
function stubDownload() {
  vi.stubGlobal("URL", {
    createObjectURL: vi.fn(() => "blob:export"),
    revokeObjectURL: vi.fn(),
  });
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement
  ) {
    clicked.push(this);
  });
}

function csvFile(rowCount: bigint, truncated = false): CsvDownload {
  return {
    blob: new Blob(["a,b\n1,2"], { type: "text/csv;charset=utf-8;" }),
    rowCount,
    truncated,
  };
}

function mountButton(exporter: (signal: AbortSignal) => Promise<CsvDownload>) {
  return mount(ExportCsvButton, {
    props: { exporter, fileName: "quality-targets" },
    global: { plugins: [i18n] },
  });
}

function button(wrapper: ReturnType<typeof mountButton>) {
  return wrapper.find("button");
}

describe("ExportCsvButton", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
    vi.clearAllMocks();
    clicked = [];
    stubDownload();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("downloads the exported file with a timestamped name and reports the rows", async () => {
    const success = vi.spyOn(notify, "success");
    const wrapper = mountButton(async () => csvFile(12n));

    await button(wrapper).trigger("click");
    await flushPromises();

    expect(clicked).toHaveLength(1);
    expect(clicked[0]?.download).toMatch(/^quality-targets-\d{8}-\d{6}\.csv$/);
    expect(success).toHaveBeenCalledWith(
      i18n.global.t("dataQuality.exportCsvDone", { count: "12" })
    );
  });

  it("warns instead of claiming success when the row ceiling cut the file", async () => {
    const success = vi.spyOn(notify, "success");
    const warning = vi.spyOn(notify, "warning");
    const wrapper = mountButton(async () => csvFile(100_000n, true));

    await button(wrapper).trigger("click");
    await flushPromises();

    expect(clicked).toHaveLength(1);
    expect(success).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledWith(
      i18n.global.t("dataQuality.exportCsvTruncated", {
        count: "100,000",
      })
    );
  });

  it("stays disabled until the whole stream has been read", async () => {
    let finish: (file: CsvDownload) => void = () => {};
    const exporter = vi.fn(
      () =>
        new Promise<CsvDownload>((resolve) => {
          finish = resolve;
        })
    );
    const wrapper = mountButton(exporter);

    await button(wrapper).trigger("click");
    await flushPromises();

    expect(exporter).toHaveBeenCalledTimes(1);
    expect(button(wrapper).attributes("disabled")).toBeDefined();
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.exportingCsv"));

    finish(csvFile(1n));
    await flushPromises();

    expect(button(wrapper).attributes("disabled")).toBeUndefined();
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.exportCsv"));
  });

  it("aborts the running export when the page unmounts, silently", async () => {
    const error = vi.spyOn(notify, "error");
    let received: AbortSignal | undefined;
    const exporter = vi.fn((signal: AbortSignal) => {
      received = signal;
      // The transport rejects an aborted fetch like this; the store's abort of a
      // superseded stream arrives as a Connect cancelation.
      return new Promise<CsvDownload>((_, reject) => {
        signal.addEventListener("abort", () =>
          reject(new DOMException("aborted", "AbortError"))
        );
      });
    });
    const wrapper = mountButton(exporter);

    await button(wrapper).trigger("click");
    await flushPromises();
    expect(received?.aborted).toBe(false);

    wrapper.unmount();
    await flushPromises();

    expect(received?.aborted).toBe(true);
    // Leaving the page is not a failed export: no toast, no download.
    expect(error).not.toHaveBeenCalled();
    expect(clicked).toHaveLength(0);
  });

  it("stays silent when the server cancels the request", async () => {
    const error = vi.spyOn(notify, "error");
    const wrapper = mountButton(async () => {
      throw new ConnectError("canceled", Code.Canceled);
    });

    await button(wrapper).trigger("click");
    await flushPromises();

    expect(error).not.toHaveBeenCalled();
    expect(clicked).toHaveLength(0);
    expect(button(wrapper).attributes("disabled")).toBeUndefined();
  });

  it("reports a failed export and re-enables the button", async () => {
    const error = vi.spyOn(notify, "error");
    const wrapper = mountButton(async () => {
      throw new Error("stream broke");
    });

    await button(wrapper).trigger("click");
    await flushPromises();

    expect(error).toHaveBeenCalledWith(
      i18n.global.t("dataQuality.exportCsvFailed")
    );
    expect(clicked).toHaveLength(0);
    expect(button(wrapper).attributes("disabled")).toBeUndefined();
  });
});

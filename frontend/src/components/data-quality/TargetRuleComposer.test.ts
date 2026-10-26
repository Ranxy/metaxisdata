import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityTargetPathSchema,
  QualityTargetSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import TargetRuleComposer from "./TargetRuleComposer.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

const mocks = vi.hoisted(() => ({
  createQualityRule: vi.fn(),
  updateQualityRule: vi.fn(),
  previewQualityRule: vi.fn(),
  listAllMetadata: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  createQualityRule: mocks.createQualityRule,
  updateQualityRule: mocks.updateQualityRule,
  previewQualityRule: mocks.previewQualityRule,
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

const TABLE = "inst1;db1;public;orders";
const CREATED = "workspaces/ws/qualityRules/rule-1";
const TARGET_LABEL = "prod · shop · public · orders";

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.run",
];

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

/** Every control in the dialog lives in a portal on `document.body`. */
function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

async function openRuleTypeSelect(): Promise<HTMLElement[]> {
  const trigger = document.querySelector<HTMLElement>("#dq-composer-rule-type");
  expect(trigger).not.toBeNull();
  // Radix opens on a left-button pointerdown; a bare Event has no `button`.
  trigger!.dispatchEvent(
    new MouseEvent("pointerdown", { bubbles: true, button: 0 })
  );
  await nextTick();
  await nextTick();
  return Array.from(
    document.body.querySelectorAll<HTMLElement>('[role="option"]')
  );
}

/** A stored rule as the page hands it to the composer for editing. */
function ruleFixture(
  options: {
    title?: string;
    state?: QualityRuleState;
    owner?: string;
    tags?: string[];
    minRows?: bigint;
  } = {}
): QualityRule {
  return create(QualityRuleSchema, {
    name: CREATED,
    title: options.title ?? "Orders have rows",
    description: "The fact table should not go empty.",
    ruleType: QualityRuleType.ROW_COUNT,
    state: options.state ?? QualityRuleState.ENABLED,
    severity: QualitySeverity.ERROR,
    owner: options.owner ?? "",
    tags: options.tags ?? [],
    scheduleInterval: { seconds: 3600n, nanos: 0 },
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
      columns: [],
    }),
    config: create(QualityRuleConfigSchema, {
      config: {
        case: "rowCount",
        value: create(RowCountConfigSchema, {
          minRows: options.minRows ?? 1n,
        }),
      },
    }),
  });
}

async function mountComposer(
  options: {
    metaType?: MetaType;
    permissions?: string[];
    defaultScheduleSeconds?: number;
    defaultSeverity?: QualitySeverity;
    rule?: QualityRule;
  } = {}
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: options.permissions ?? PERMISSIONS,
  });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: { template: "<div />" } },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEdit",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(TargetRuleComposer, {
    attachTo: document.body,
    props: {
      open: true,
      targetGuid: TABLE,
      metaType: options.metaType ?? MetaType.TABLE,
      targetLabel: TARGET_LABEL,
      defaultScheduleSeconds: options.defaultScheduleSeconds,
      defaultSeverity: options.defaultSeverity,
      rule: options.rule,
    },
    global: { plugins: [i18n, pinia, router] },
  });
  await flushPromises();
  return wrapper;
}

describe("TargetRuleComposer", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listAllMetadata.mockResolvedValue([
      {
        guid: `${TABLE};email`,
        type: {
          case: "columnMetadata",
          value: { name: "email", type: "text", nullable: true },
        },
      },
    ]);
    mocks.previewQualityRule.mockResolvedValue({
      compiledSql: "SELECT count(*) FROM orders",
      referencedColumns: [],
      parameters: [],
    });
    mocks.createQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: CREATED,
        title: "Orders have rows",
        ruleType: QualityRuleType.ROW_COUNT,
        state: QualityRuleState.ENABLED,
      })
    );
  });

  it("names the target it is fixed to and links to the full editor", async () => {
    const wrapper = await mountComposer();

    // The title carries the page's label, not a target the reader must pick.
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.title", { target: TARGET_LABEL })
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.description")
    );
    // Column details reach the config form's pickers; a failed read is silent.
    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });

    const advanced = [
      ...document.body.querySelectorAll<HTMLAnchorElement>('[role="dialog"] a'),
    ].find(
      (link) =>
        link.textContent?.trim() === label("dataQuality.composer.advanced")
    );
    expect(advanced).toBeDefined();
    const href = advanced?.getAttribute("href") ?? "";
    expect(href).toContain("/data-quality/rules/new");
    expect(href).toContain("table_guid=");
    expect(href).toContain("meta_type=");
    wrapper.unmount();
  });

  it("offers only the five rule types a view can watch", async () => {
    const wrapper = await mountComposer({ metaType: MetaType.VIEW });
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.ruleTypeViewHint")
    );

    const options = await openRuleTypeSelect();
    const labels = options.map((option) => option.textContent?.trim() ?? "");
    expect(options).toHaveLength(5);
    expect(new Set(labels)).toEqual(
      new Set(
        [
          QualityRuleType.ROW_COUNT,
          QualityRuleType.ROW_COUNT_CHANGE,
          QualityRuleType.FRESHNESS,
          QualityRuleType.CUSTOM_SQL,
          QualityRuleType.SCHEMA_DRIFT,
        ].map((ruleType) => label(qualityRuleTypeKey(ruleType)))
      )
    );
    expect(labels).not.toContain(label("dataQuality.ruleType.unique"));
    expect(labels).not.toContain(label("dataQuality.ruleType.aggregate"));
    wrapper.unmount();
  });

  it("creates the rule on the fixed target and reports it to the page", async () => {
    const created = create(QualityRuleSchema, {
      name: CREATED,
      title: "Orders have rows",
      ruleType: QualityRuleType.ROW_COUNT,
      state: QualityRuleState.ENABLED,
    });
    mocks.createQualityRule.mockResolvedValue(created);
    const wrapper = await mountComposer();

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // Saving is two-step on purpose: the dry run compiles the rule first.
    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    expect(rule.title).toBe("Orders have rows");
    expect(rule.target?.metaGuid).toBe(TABLE);
    expect(rule.target?.metaType).toBe(MetaType.TABLE);
    expect(rule.state).toBe(QualityRuleState.ENABLED);
    expect(rule.config?.config.case).toBe("rowCount");

    // The page renders the created rule in place, so the dialog reports it.
    expect(wrapper.emitted("saved")).toEqual([[created]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("starts a new rule from the contract's defaults", async () => {
    const wrapper = await mountComposer({
      defaultScheduleSeconds: 6 * 3600,
      defaultSeverity: QualitySeverity.ERROR,
    });

    // Six hours is the largest whole unit the schedule control can show.
    expect(
      document.querySelector<HTMLInputElement>("#dq-composer-schedule")?.value
    ).toBe("6");

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // The severity select's label only renders once its menu opens, so the saved
    // rule is the observable proof that the contract default applied.
    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    expect(rule.severity).toBe(QualitySeverity.ERROR);
    expect(rule.scheduleInterval?.seconds).toBe(21600n);
    wrapper.unmount();
  });

  it("skips the dry run when the caller may not run rules", async () => {
    const wrapper = await mountComposer({
      permissions: ["metaxisdata.dataQuality.rules.create"],
    });

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("reports an incomplete form in place and sends nothing", async () => {
    const wrapper = await mountComposer();

    // A blank rule has no title and no row bound.
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.createQualityRule).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.validationTitle")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.validation.titleRequired")
    );
    wrapper.unmount();
  });

  it("shows the server's rejection in place and keeps the dialog open", async () => {
    mocks.createQualityRule.mockRejectedValue(
      new ConnectError('column "nope" does not exist', Code.InvalidArgument)
    );
    const wrapper = await mountComposer();

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.editor.saveFailed")
    );
    expect(document.body.textContent).toContain('column "nope" does not exist');
    // A rejected create is not a save: nothing is reported and the form stays.
    expect(wrapper.emitted("saved")).toBeUndefined();
    expect(wrapper.emitted("update:open")).toBeUndefined();
    wrapper.unmount();
  });

  it("edits the rule in place and sends only the changed fields", async () => {
    const original = ruleFixture();
    const updated = ruleFixture({ title: "Orders have rows, revised" });
    mocks.updateQualityRule.mockResolvedValue(updated);
    const wrapper = await mountComposer({ rule: original });

    // The dialog is fixed to the rule rather than to a blank create form.
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.editTitle", { title: "Orders have rows" })
    );
    expect(dialogButton(label("dataQuality.editor.saveChanges"))).toBeDefined();
    // The advanced link opens this rule in the full editor, not a new rule.
    const advanced = [
      ...document.body.querySelectorAll<HTMLAnchorElement>('[role="dialog"] a'),
    ].find(
      (link) =>
        link.textContent?.trim() === label("dataQuality.composer.advanced")
    );
    expect(advanced?.getAttribute("href")).toContain(
      "qualityRules/rule-1/edit"
    );

    setInput("#dq-composer-title", "Orders have rows, revised");
    setInput("#dq-field-minRows", "5");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    // The dry run still gates the write, exactly as it does for a new rule.
    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.name).toBe(original.name);
    expect(rule.title).toBe("Orders have rows, revised");
    expect(rule.config?.config.case).toBe("rowCount");
    expect(
      (rule.config?.config.value as { minRows?: bigint } | undefined)?.minRows
    ).toBe(5n);
    // Only what changed travels; target and state stay off the mask.
    expect(mask).toContain("title");
    expect(mask).toContain("config");
    expect(mask).not.toContain("target");
    expect(mask).not.toContain("state");

    expect(wrapper.emitted("saved")).toEqual([[updated]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("keeps the state, owner and tags a rule was saved with", async () => {
    const original = ruleFixture({
      state: QualityRuleState.PAUSED,
      owner: "users/alice",
      tags: ["nightly"],
    });
    mocks.updateQualityRule.mockResolvedValue(
      ruleFixture({ title: "Renamed", state: QualityRuleState.PAUSED })
    );
    const wrapper = await mountComposer({ rule: original });

    setInput("#dq-composer-title", "Renamed");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.state).toBe(QualityRuleState.PAUSED);
    expect(rule.owner).toBe("users/alice");
    expect(rule.tags).toEqual(["nightly"]);
    // A rename is a rename: nothing else is named by the mask.
    expect(mask).toEqual(["title"]);
    wrapper.unmount();
  });

  it("closes without a request when an edit changes nothing", async () => {
    const wrapper = await mountComposer({ rule: ruleFixture() });

    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    expect(wrapper.emitted("saved")).toBeUndefined();
    // The dialog still closes: there is nothing to save, not an error.
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });
});

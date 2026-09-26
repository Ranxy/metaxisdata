import { beforeEach, describe, expect, it, vi } from "vitest";
import { notify } from "./notify";

const mocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
}));

vi.mock("vue-sonner", () => ({
  toast: {
    success: mocks.success,
    error: mocks.error,
    warning: mocks.warning,
    info: mocks.info,
  },
}));

describe("notify", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("routes each severity to its own sonner channel", () => {
    notify.success("saved");
    notify.warning("careful");
    notify.info("for your information");

    expect(mocks.success).toHaveBeenCalledWith("saved", { duration: 5000 });
    expect(mocks.warning).toHaveBeenCalledWith("careful", { duration: 5000 });
    expect(mocks.info).toHaveBeenCalledWith("for your information", {
      duration: 5000,
    });
    expect(mocks.error).not.toHaveBeenCalled();
  });

  it("keeps an error on screen longer, because it carries the reason", () => {
    notify.error("Connection failed");

    expect(mocks.error).toHaveBeenCalledWith("Connection failed", {
      duration: 8000,
    });
  });
});

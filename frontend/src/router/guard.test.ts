import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import router from "@/router";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";

const mocks = vi.hoisted(() => ({
  getCurrentUser: vi.fn(),
  updateUser: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
}));

vi.mock("@/api/user", () => ({
  getCurrentUser: mocks.getCurrentUser,
  updateUser: mocks.updateUser,
}));

vi.mock("@/api/auth", () => ({
  login: mocks.login,
  logout: mocks.logout,
}));

describe("router auth guard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, {
        name: "users/1",
        permissions: ["metaxisdata.instances.list"],
      })
    );
  });

  it("loads the permissions before entering a page right after login", async () => {
    const authStore = useAuthStore();
    // Exactly what login() leaves behind: a user without permissions.
    authStore.user = create(UserSchema, { name: "users/1" });
    authStore.isAuthenticated = true;
    expect(authStore.permissions).toEqual([]);

    await router.push("/");

    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
    expect(authStore.permissions).toEqual(["metaxisdata.instances.list"]);

    // The permission-bearing profile is loaded once, not on every navigation.
    await router.push("/databases");
    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
  });
});

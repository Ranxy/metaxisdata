import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useUserDirectory } from "./useUserDirectory";

const mocks = vi.hoisted(() => ({
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/user", () => ({
  listAllUsers: mocks.listAllUsers,
}));

const USERS = [
  create(UserSchema, { name: "users/101", email: "alice@example.com" }),
  create(UserSchema, { name: "users/102", email: "bob@example.com" }),
];

function activate(permissions: string[]) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
}

describe("useUserDirectory", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listAllUsers.mockResolvedValue(USERS);
  });

  it("resolves owners to emails, keeping unknown names as they are", async () => {
    activate(["metaxisdata.users.list"]);
    const directory = useUserDirectory();

    await directory.load();
    await directory.load();

    expect(mocks.listAllUsers).toHaveBeenCalledTimes(1);
    expect(directory.users.value).toHaveLength(2);
    expect(directory.emailOf("users/101")).toBe("alice@example.com");
    expect(directory.emailOf("users/999")).toBe("users/999");
  });

  it("does not request the directory without users.list", async () => {
    activate([]);
    const directory = useUserDirectory();

    await directory.load();

    expect(mocks.listAllUsers).not.toHaveBeenCalled();
    expect(directory.users.value).toEqual([]);
    expect(directory.emailOf("users/101")).toBe("users/101");
  });

  it("falls back to resource names when the request fails", async () => {
    mocks.listAllUsers.mockRejectedValue(new Error("directory unavailable"));
    activate(["metaxisdata.users.list"]);
    const directory = useUserDirectory();

    await directory.load();

    expect(directory.users.value).toEqual([]);
    expect(directory.emailOf("users/101")).toBe("users/101");
  });
});

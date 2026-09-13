import { defineStore } from "pinia";
import * as authApi from "@/api/auth";
import * as userApi from "@/api/user";
import type { User } from "@/types/proto-es/v1/user_service_pb";

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  // Set when the server issued a token restricted to a forced password reset.
  requireResetPassword: boolean;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
    requireResetPassword: false,
  }),

  getters: {
    userName: (state) => state.user?.title || state.user?.email || "",
    userEmail: (state) => state.user?.email || "",
    // The effective workspace permissions, populated by GetCurrentUser. The
    // server is authoritative; this only hides UI the caller cannot use.
    permissions: (state) => state.user?.permissions ?? [],
    hasPermission: (state) => (permission: string) =>
      state.user?.permissions.includes(permission) ?? false,
  },

  actions: {
    async login(email: string, password: string) {
      this.isLoading = true;
      this.error = null;
      try {
        const response = await authApi.login(email, password);
        this.user = response.user ?? null;
        this.isAuthenticated = true;
        this.requireResetPassword = response.requireResetPassword;
        return response;
      } catch (err) {
        this.error = err instanceof Error ? err.message : "Login failed";
        throw err;
      } finally {
        this.isLoading = false;
      }
    },

    // Completes a forced password reset. The server only accepts this call from
    // the restricted token the login handed out.
    async changePassword(currentPassword: string, newPassword: string) {
      if (!this.user) {
        throw new Error("not authenticated");
      }
      await userApi.updateUser(
        { name: this.user.name, password: newPassword },
        ["password"],
        currentPassword
      );
      this.requireResetPassword = false;
    },

    async logout() {
      try {
        await authApi.logout();
      } finally {
        this.user = null;
        this.isAuthenticated = false;
        this.error = null;
        this.requireResetPassword = false;
      }
    },

    async fetchCurrentUser() {
      this.isLoading = true;
      try {
        this.user = await userApi.getCurrentUser();
        this.isAuthenticated = true;
        this.requireResetPassword = false;
      } catch {
        this.user = null;
        this.isAuthenticated = false;
      } finally {
        this.isLoading = false;
      }
    },

    clearError() {
      this.error = null;
    },
  },
});

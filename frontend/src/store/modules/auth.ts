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
  // Whether `user` came from GetCurrentUser, the only call that resolves the
  // effective permissions. The login response carries the user without them, so
  // permission-gated UI must wait for the profile to be loaded.
  permissionsLoaded: boolean;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
    requireResetPassword: false,
    permissionsLoaded: false,
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
        // The login response has no permissions; ensurePermissionsLoaded()
        // resolves them once the caller enters an authenticated page.
        this.permissionsLoaded = false;
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
        this.permissionsLoaded = false;
      }
    },

    async fetchCurrentUser() {
      this.isLoading = true;
      try {
        this.user = await userApi.getCurrentUser();
        this.isAuthenticated = true;
        this.requireResetPassword = false;
        this.permissionsLoaded = true;
      } catch {
        this.user = null;
        this.isAuthenticated = false;
        this.permissionsLoaded = false;
      } finally {
        this.isLoading = false;
      }
    },

    // Loads the permission-bearing profile at most once per session. Safe to
    // call before every navigation: a fresh session fetches, a just-finished
    // login fetches because its response had no permissions, and later
    // navigations are no-ops. A forced password reset is left alone: its
    // restricted token cannot call GetCurrentUser, and the pending user has to
    // stay in place for the password change.
    async ensurePermissionsLoaded() {
      if (this.permissionsLoaded || this.requireResetPassword) {
        return;
      }
      await this.fetchCurrentUser();
    },

    clearError() {
      this.error = null;
    },
  },
});

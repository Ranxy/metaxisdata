import { defineStore } from "pinia";
import * as authApi from "@/api/auth";
import * as userApi from "@/api/user";
import type { User } from "@/types/proto-es/v1/user_service_pb";

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
  }),

  getters: {
    userName: (state) => state.user?.title || state.user?.email || "",
    userEmail: (state) => state.user?.email || "",
  },

  actions: {
    async login(email: string, password: string) {
      this.isLoading = true;
      this.error = null;
      try {
        const response = await authApi.login(email, password);
        this.user = response.user ?? null;
        this.isAuthenticated = true;
        return response;
      } catch (err) {
        this.error = err instanceof Error ? err.message : "Login failed";
        throw err;
      } finally {
        this.isLoading = false;
      }
    },

    async logout() {
      try {
        await authApi.logout();
      } finally {
        this.user = null;
        this.isAuthenticated = false;
        this.error = null;
      }
    },

    async fetchCurrentUser() {
      this.isLoading = true;
      try {
        this.user = await userApi.getCurrentUser();
        this.isAuthenticated = true;
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

import { Code } from "@connectrpc/connect";
import { defineStore } from "pinia";
import * as authApi from "@/api/auth";
import * as userApi from "@/api/user";
import { type AppLocale, isAppLocale } from "@/locales";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { errorCode } from "@/utils/error";
import { useAppStore } from "./app";

// The in-flight GetCurrentUser, kept outside the store: a promise is not state,
// and two navigations in the same tick must share one request.
let inFlightProfile: Promise<void> | undefined;

// The language writes, chained rather than fired concurrently: two switches in
// quick succession must reach the server in the order they were made, or the
// first one can land last and leave the profile in a language the user has
// already left. Every link swallows its own failure, so the chain never rejects.
let languageWrites: Promise<void> = Promise.resolve();

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  // Set when the server issued a token restricted to a forced password reset.
  requireResetPassword: boolean;
  // Whether `user` came from GetCurrentUser, the only call that resolves the
  // effective permissions. The login response carries the user without them, so
  // permission-gated UI must wait for the profile to be loaded.
  permissionsLoaded: boolean;
  // The server rejected a session we believed in (as opposed to the user having
  // never signed in). The guard reads it to explain the bounce on the login page.
  sessionExpired: boolean;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    requireResetPassword: false,
    permissionsLoaded: false,
    sessionExpired: false,
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
      try {
        const response = await authApi.login(email, password);
        this.user = response.user ?? null;
        this.isAuthenticated = true;
        this.requireResetPassword = response.requireResetPassword;
        // The login response has no permissions; ensurePermissionsLoaded()
        // resolves them once the caller enters an authenticated page.
        this.permissionsLoaded = false;
        this.sessionExpired = false;
        return response;
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

    /**
     * Switches the UI language and records it on the profile. This is the one
     * entry point for the switch: the server answers the Explain SQL question in
     * the language the user reads the UI in, so the locale has to be more than a
     * browser-local preference.
     *
     * The switch is applied locally first — it must be instant, and it also has
     * to work on the login page, before there is a session to write to. The write
     * itself is queued, so the language the user chose last is the one the server
     * ends up with.
     */
    async changeLanguage(locale: AppLocale) {
      useAppStore().setLocale(locale);
      const pending = languageWrites.then(() => this.persistLanguage(locale));
      languageWrites = pending;
      await pending;
    },

    /**
     * Writes one language to the profile, or does nothing when the profile
     * already carries it. Failure is swallowed: it must not undo the language the
     * user just chose, and ensureLanguagePersisted retries before an answer
     * depends on the stored value.
     */
    async persistLanguage(locale: AppLocale) {
      const user = this.user;
      if (!user || user.language === locale) {
        return;
      }
      try {
        await userApi.updateUser({ name: user.name, language: locale }, [
          "language",
        ]);
        user.language = locale;
      } catch {
        // The profile keeps its old value.
      }
    },

    /**
     * Returns once the profile carries the language the interface is in, so a
     * caller whose answer depends on it — an Explain SQL request, which the
     * server writes in the stored language — does not race a switch or a failed
     * write. A write that already matches costs nothing.
     */
    async ensureLanguagePersisted() {
      await languageWrites;
      await this.persistLanguage(useAppStore().locale);
    },

    /**
     * Aligns the UI language with the profile once it is known. A language the
     * SPA ships is the source of truth, so a choice made on another device
     * applies here too; an account that never chose one adopts the locale this
     * browser already reads, which keeps the interface and the Explain SQL answer
     * from drifting apart on the first request.
     *
     * A stored tag this SPA does not ship — a newer client, or a locale the
     * server added — is left alone: overwriting it with this browser's locale
     * would discard the user's choice in exchange for nothing, since this
     * interface cannot render it either way.
     */
    async syncLanguage() {
      const stored = this.user?.language ?? "";
      if (stored === "") {
        await this.changeLanguage(useAppStore().locale);
        return;
      }
      if (isAppLocale(stored)) {
        await this.changeLanguage(stored);
      }
    },

    // Drops everything the session carried. Called by logout, by the global
    // Unauthenticated interceptor, and when GetCurrentUser reports the cookie is
    // gone — one definition so no call site can forget a field.
    clearSession() {
      this.user = null;
      this.isAuthenticated = false;
      this.requireResetPassword = false;
      this.permissionsLoaded = false;
    },

    /**
     * The server rejected the session: forget the user and remember that this was
     * an expiry rather than a deliberate sign-out, so the login form can say so.
     * Called by the transport interceptor and by a rejected GetCurrentUser.
     *
     * Only a session we believed in can expire. A visitor who never signed in
     * gets the same `Unauthenticated` from the first GetCurrentUser, and telling
     * them on the login page that their session expired is simply untrue — it is
     * the single most common way to arrive at that page.
     */
    handleUnauthenticated() {
      this.sessionExpired = this.isAuthenticated;
      this.clearSession();
    },

    async logout() {
      try {
        await authApi.logout();
      } finally {
        this.clearSession();
        this.sessionExpired = false;
      }
    },

    async fetchCurrentUser() {
      this.isLoading = true;
      try {
        this.user = await userApi.getCurrentUser();
        this.isAuthenticated = true;
        this.requireResetPassword = false;
        this.permissionsLoaded = true;
        await this.syncLanguage(); // a failed write is swallowed; see changeLanguage
      } catch (error) {
        // Only the server rejecting the session ends it. A network blip or a 5xx
        // must not look like a logout: the router sends an unauthenticated user
        // to the login page, and losing a valid cookie to one failed request
        // would do exactly that on the next navigation.
        if (errorCode(error) === Code.Unauthenticated) {
          this.handleUnauthenticated();
        }
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
      // Two navigations can land in the same tick right after login; both would
      // otherwise fire GetCurrentUser. The second waits on the first's promise.
      if (inFlightProfile) {
        await inFlightProfile;
        return;
      }
      inFlightProfile = this.fetchCurrentUser().finally(() => {
        inFlightProfile = undefined;
      });
      await inFlightProfile;
    },
  },
});

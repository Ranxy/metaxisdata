// Custom type definitions for the application

export interface MenuItem {
  key: string;
  label: string;
  path: string;
  icon?: string;
  children?: MenuItem[];
}

export interface Notification {
  id: string;
  type: "success" | "error" | "warning" | "info";
  message: string;
  duration?: number;
}

export type Locale = "zh-CN" | "en-US";

export interface AppConfig {
  locale: Locale;
  theme: "light" | "dark";
  sidebarCollapsed: boolean;
}

<template>
  <div class="space-y-4">
    <!-- Page Header -->
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("home.title") }}
      </h1>
    </div>

    <!-- Welcome Card -->
    <Card>
      <CardContent class="p-6">
        <div class="flex items-start space-x-4">
          <div class="flex-shrink-0">
            <div class="w-12 h-12 bg-primary/10 rounded-xl flex items-center justify-center">
              <Sparkles class="w-6 h-6 text-primary" />
            </div>
          </div>
          <div>
            <h2 class="text-lg font-semibold">
              {{ t("home.welcome") }}, {{ userName }}!
            </h2>
            <p class="text-muted-foreground">
              {{ t("home.description") }}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Quick Actions -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      <Card
        v-for="action in quickActions"
        :key="action.title"
        class="hover:shadow-md transition-shadow cursor-pointer"
      >
        <CardContent class="p-6">
          <div
            :class="[
              'w-12 h-12 rounded-xl flex items-center justify-center mb-4',
              action.bgColor,
            ]"
          >
            <component
              :is="action.icon"
              :class="['w-6 h-6', action.iconColor]"
            />
          </div>
          <h3 class="text-lg font-semibold">
            {{ action.title }}
          </h3>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ action.description }}
          </p>
        </CardContent>
      </Card>
    </div>

    <!-- Stats Section -->
    <div class="grid grid-cols-1 md:grid-cols-4 gap-6">
      <Card
        v-for="stat in stats"
        :key="stat.label"
      >
        <CardContent class="p-6">
          <p class="text-sm font-medium text-muted-foreground">
            {{ stat.label }}
          </p>
          <p class="mt-2 text-3xl font-bold">
            {{ stat.value }}
          </p>
          <div class="mt-2 flex items-center text-sm">
            <span :class="stat.changePositive ? 'text-green-600' : 'text-destructive'">
              {{ stat.changePositive ? "+" : "" }}{{ stat.change }}%
            </span>
            <span class="ml-2 text-muted-foreground">vs last month</span>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Database, Folder, Sparkles, Users } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Card, CardContent } from "@/components/ui/card";
import { useAuthStore } from "@/store/modules/auth";

const { t } = useI18n();
const authStore = useAuthStore();

const userName = computed(() => authStore.userName || "User");

const quickActions = [
  {
    title: "连接数据源",
    description: "添加新的数据库连接",
    icon: Database,
    bgColor: "bg-blue-100",
    iconColor: "text-blue-600",
  },
  {
    title: "创建项目",
    description: "开始一个新的数据项目",
    icon: Folder,
    bgColor: "bg-green-100",
    iconColor: "text-green-600",
  },
  {
    title: "管理用户",
    description: "添加或管理团队成员",
    icon: Users,
    bgColor: "bg-purple-100",
    iconColor: "text-purple-600",
  },
];

const stats = [
  { label: "数据源", value: "12", change: 8.2, changePositive: true },
  { label: "项目", value: "24", change: 12.5, changePositive: true },
  { label: "用户", value: "8", change: 0, changePositive: true },
  { label: "查询", value: "1,234", change: 3.1, changePositive: false },
];
</script>

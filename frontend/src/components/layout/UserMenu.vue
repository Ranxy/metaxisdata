<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        variant="ghost"
        class="gap-2"
      >
        <!-- Avatar -->
        <Avatar class="h-8 w-8">
          <AvatarFallback class="bg-primary text-primary-foreground text-sm">
            {{ userInitial }}
          </AvatarFallback>
        </Avatar>
        <!-- Name (hidden on small screens) -->
        <span class="hidden md:block text-sm font-medium">
          {{ userName }}
        </span>
        <!-- Dropdown Arrow -->
        <ChevronDown class="h-4 w-4 text-muted-foreground" />
      </Button>
    </DropdownMenuTrigger>

    <!-- Dropdown Menu -->
    <DropdownMenuContent
      align="end"
      class="w-56"
    >
      <!-- User Info -->
      <DropdownMenuLabel class="font-normal">
        <div class="flex flex-col space-y-1">
          <p class="text-sm font-medium leading-none">
            {{ userName }}
          </p>
          <p class="text-xs leading-none text-muted-foreground">
            {{ userEmail }}
          </p>
        </div>
      </DropdownMenuLabel>
      <DropdownMenuSeparator />

      <!-- Menu Items -->
      <DropdownMenuItem @click="handleProfile">
        <User class="mr-2 h-4 w-4" />
        {{ t("header.profile") }}
      </DropdownMenuItem>

      <DropdownMenuSeparator />

      <DropdownMenuItem
        class="text-destructive focus:text-destructive"
        @click="handleLogout"
      >
        <LogOut class="mr-2 h-4 w-4" />
        {{ t("header.logout") }}
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { ChevronDown, LogOut, User } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAuthStore } from "@/store/modules/auth";

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();

const userName = computed(() => authStore.userName || "User");
const userEmail = computed(() => authStore.userEmail || "");
const userInitial = computed(() => userName.value.charAt(0).toUpperCase());

function handleProfile() {
  // TODO: Navigate to profile page
}

async function handleLogout() {
  await authStore.logout();
  router.push({ name: "Login" });
}
</script>

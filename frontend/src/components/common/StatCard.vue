<template>
  <component
    :is="to ? RouterLink : 'div'"
    :to="to"
    :class="to ? 'group block' : ''"
  >
    <Card
      :class="[
        'h-full',
        to ? 'transition-shadow group-hover:shadow-e2' : '',
      ]"
    >
      <CardContent class="flex items-start justify-between gap-3 p-5">
        <div class="min-w-0">
          <p class="text-xs font-medium text-muted-foreground">
            {{ label }}
          </p>
          <p class="mt-1.5 text-[1.75rem] font-semibold leading-9 tracking-tight tabular-nums">
            {{ value }}
          </p>
          <p
            v-if="hint"
            class="mt-1 truncate text-xs text-muted-foreground"
          >
            {{ hint }}
          </p>
        </div>
        <span
          v-if="icon"
          class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary-subtle text-primary-subtle-foreground"
        >
          <component
            :is="icon"
            class="h-[1.125rem] w-[1.125rem]"
          />
        </span>
      </CardContent>
    </Card>
  </component>
</template>

<script setup lang="ts">
import type { Component } from "vue";
import { type RouteLocationRaw, RouterLink } from "vue-router";
import Card from "@/components/ui/card/Card.vue";
import CardContent from "@/components/ui/card/CardContent.vue";

// One number card for the whole app. The dashboard and the three OpenLineage
// index pages each used to carry their own shape — value-right-of-label on one,
// value-under-label on the other, at two different type scales — so the same
// kind of number looked like a different kind of thing depending on the page.
withDefaults(
  defineProps<{
    label: string;
    value: string | number;
    /** A second line of context under the number. */
    hint?: string;
    icon?: Component;
    /** Turns the card into a link with the matching hover affordance. */
    to?: RouteLocationRaw;
  }>(),
  { hint: "", icon: undefined, to: undefined }
);
</script>

<template>
  <component
    :is="to ? RouterLink : 'div'"
    :to="to"
    :class="to ? 'group block' : ''"
  >
    <Card
      :class="[
        'h-full',
        to ? 'transition-shadow group-hover:shadow-md' : '',
      ]"
    >
      <CardContent class="p-5">
        <div class="flex items-center justify-between gap-2">
          <span class="text-sm font-medium text-muted-foreground">
            {{ label }}
          </span>
          <component
            :is="icon"
            v-if="icon"
            class="h-4 w-4 shrink-0 text-muted-foreground"
          />
        </div>
        <p class="mt-2 text-3xl font-bold">
          {{ value }}
        </p>
        <p
          v-if="hint"
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ hint }}
        </p>
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

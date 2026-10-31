<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('notificationSettings.title')"
      :description="t('notificationSettings.pageDescription')"
    />

    <!-- Channels: who an outbound message goes to and for what. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.channels") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.channelsDescription") }}
          </CardDescription>
        </div>
        <Button
          size="sm"
          :disabled="!canUpdate"
          @click="openCreate"
        >
          <Plus class="mr-1.5 h-4 w-4" />
          {{ t("notificationSettings.newChannel") }}
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <PageState :loading="loading">
          <div
            v-if="channels.length === 0"
            class="px-4 py-10 text-center"
          >
            <p class="text-sm font-medium">
              {{ t("notificationSettings.emptyTitle") }}
            </p>
            <p class="mt-1 text-sm text-muted-foreground">
              {{ t("notificationSettings.emptyDescription") }}
            </p>
          </div>
          <div
            v-else
            class="rounded-md border"
          >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("notificationSettings.name") }}</TableHead>
                <TableHead>{{ t("notificationSettings.kind") }}</TableHead>
                <TableHead>{{ t("notificationSettings.subscription") }}</TableHead>
                <TableHead>{{ t("notificationSettings.status") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("common.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="channel in channels"
                :key="channel.name"
              >
                <TableCell class="font-medium">
                  {{ channel.displayName }}
                </TableCell>
                <TableCell>
                  {{ channelKindLabel(channel.kind) }}
                </TableCell>
                <TableCell class="text-xs text-muted-foreground">
                  {{ subscriptionLabel(channel) }}
                </TableCell>
                <TableCell>
                  <Badge
                    :variant="channel.enabled ? 'default' : 'secondary'"
                    class="font-normal"
                  >
                    {{
                      channel.enabled
                        ? t("notificationSettings.enabled")
                        : t("notificationSettings.disabled")
                    }}
                  </Badge>
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      :disabled="!canUpdate || testing === channel.name"
                      @click="testChannel(channel)"
                    >
                      <Loader2
                        v-if="testing === channel.name"
                        class="mr-1 h-4 w-4 animate-spin"
                      />
                      {{ t("notificationSettings.test") }}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      :disabled="!canUpdate"
                      @click="openEdit(channel)"
                    >
                      {{ t("common.edit") }}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      class="text-destructive"
                      :disabled="!canUpdate"
                      @click="confirmDelete(channel)"
                    >
                      {{ t("common.delete") }}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Email delivery: the SMTP server an EMAIL channel sends through. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.emailDelivery") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.emailDeliveryDescription") }}
          </CardDescription>
        </div>
        <div class="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            :disabled="!canUpdate || emailLoading || emailSaving || emailTesting"
            @click="onSendTestEmail"
          >
            <Loader2
              v-if="emailTesting"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("notificationSettings.sendTestEmail") }}
          </Button>
          <Button
            size="sm"
            :disabled="!canUpdate || emailLoading || emailSaving || emailTesting"
            @click="saveEmailSetting"
          >
            <Loader2
              v-if="emailSaving"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("common.save") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent class="pt-4">
        <p
          v-if="!canGetSettings"
          class="px-4 py-6 text-center text-sm text-muted-foreground"
        >
          {{ t("notificationSettings.emailReadOnly") }}
        </p>
        <PageState
          v-else
          :loading="emailLoading"
        >
          <div class="grid gap-4 sm:grid-cols-2">
            <FormField
              v-model="emailForm.from"
              :label="t('notificationSettings.emailFrom')"
              placeholder="notifications@example.com"
            />
            <FormField
              v-model="emailForm.fromName"
              :label="t('notificationSettings.emailFromName')"
            />
            <FormField
              v-model="emailForm.host"
              :label="t('notificationSettings.smtpHost')"
              placeholder="smtp.example.com"
            />
            <FormField
              v-model="emailForm.port"
              type="number"
              :label="t('notificationSettings.smtpPort')"
            />
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.smtpEncryption") }}</Label>
              <Select
                :model-value="String(emailForm.encryption)"
                @update:model-value="onEncryptionChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">
                    {{ t("notificationSettings.encryptionNone") }}
                  </SelectItem>
                  <SelectItem value="2">STARTTLS</SelectItem>
                  <SelectItem value="3">SSL/TLS</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.smtpAuthentication") }}</Label>
              <Select
                :model-value="String(emailForm.authentication)"
                @update:model-value="onAuthenticationChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">
                    {{ t("notificationSettings.authenticationNone") }}
                  </SelectItem>
                  <SelectItem value="2">PLAIN</SelectItem>
                  <SelectItem value="3">LOGIN</SelectItem>
                  <SelectItem value="4">CRAM-MD5</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <FormField
              v-model="emailForm.username"
              :label="t('notificationSettings.smtpUsername')"
            />
            <FormField
              v-model="emailForm.password"
              type="password"
              :label="t('notificationSettings.smtpPassword')"
              :hint="
                emailPasswordSet
                  ? t('notificationSettings.smtpPasswordKeepHint')
                  : ''
              "
            />
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Quiet hours: hold deliveries during a recurring or maintenance window. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.quietHours") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.quietHoursDescription") }}
          </CardDescription>
        </div>
        <Button
          size="sm"
          :disabled="!canUpdate || quietHoursLoading || quietHoursSaving"
          @click="saveQuietHours"
        >
          <Loader2
            v-if="quietHoursSaving"
            class="mr-1.5 h-4 w-4 animate-spin"
          />
          {{ t("common.save") }}
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <p
          v-if="!canGetSettings"
          class="px-4 py-6 text-center text-sm text-muted-foreground"
        >
          {{ t("notificationSettings.quietHoursReadOnly") }}
        </p>
        <PageState
          v-else
          :loading="quietHoursLoading"
        >
          <div class="space-y-5">
            <div class="space-y-1.5">
              <label class="flex items-center gap-2 text-sm">
                <Checkbox
                  :checked="quietHoursForm.enabled"
                  :disabled="!canUpdate"
                  @update:checked="onQuietHoursEnabledChange"
                />
                <span>{{ t("notificationSettings.quietHoursEnabled") }}</span>
              </label>
              <p
                v-if="!quietHoursForm.enabled"
                class="text-sm text-muted-foreground"
              >
                {{ t("notificationSettings.quietHoursOffHint") }}
              </p>
            </div>

            <div class="max-w-sm space-y-2.5">
              <Label>{{ t("notificationSettings.quietHoursTimezone") }}</Label>
              <Select
                :model-value="quietHoursForm.timezone"
                :disabled="quietHoursDisabled"
                @update:model-value="onQuietHoursTimezoneChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="zone in timezoneOptions"
                    :key="zone"
                    :value="zone"
                  >
                    {{ zone }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.quietHoursTimezoneHint") }}
              </p>
            </div>

            <div class="space-y-3">
              <div class="flex flex-wrap items-start justify-between gap-2">
                <div class="space-y-1">
                  <p class="text-sm font-medium">
                    {{ t("notificationSettings.quietWindows") }}
                  </p>
                  <p class="text-xs text-muted-foreground">
                    {{ t("notificationSettings.quietWindowsHint") }}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="quietHoursDisabled"
                  @click="addQuietWindow"
                >
                  <Plus class="mr-1.5 h-4 w-4" />
                  {{ t("notificationSettings.addQuietWindow") }}
                </Button>
              </div>
              <p
                v-if="quietHoursForm.windows.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("notificationSettings.noQuietWindows") }}
              </p>
              <div
                v-for="(window, index) in quietHoursForm.windows"
                :key="index"
                class="rounded-md border p-3"
              >
                <div class="flex flex-wrap items-end gap-3">
                  <div class="min-w-56 flex-1 space-y-1.5">
                    <Label class="text-xs">
                      {{ t("notificationSettings.quietWindowDays") }}
                    </Label>
                    <div class="flex flex-wrap gap-x-3 gap-y-1.5">
                      <label
                        v-for="day in dayOptions"
                        :key="day.value"
                        class="flex items-center gap-1.5 text-xs"
                      >
                        <Checkbox
                          :checked="window.days.includes(day.value)"
                          :disabled="quietHoursDisabled"
                          @update:checked="(value: boolean | 'indeterminate') => toggleQuietWindowDay(window, day.value, value === true)"
                        />
                        <span>{{ day.label }}</span>
                      </label>
                    </div>
                  </div>
                  <div class="w-32">
                    <FormField
                      v-model="window.start"
                      type="time"
                      :label="t('notificationSettings.quietWindowStart')"
                      :disabled="quietHoursDisabled"
                    />
                  </div>
                  <div class="w-32">
                    <FormField
                      v-model="window.end"
                      type="time"
                      :label="t('notificationSettings.quietWindowEnd')"
                      :disabled="quietHoursDisabled"
                    />
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="text-destructive"
                    :disabled="quietHoursDisabled"
                    @click="removeQuietWindow(index)"
                  >
                    {{ t("common.delete") }}
                  </Button>
                </div>
              </div>
            </div>

            <div class="space-y-3">
              <div class="flex flex-wrap items-start justify-between gap-2">
                <div class="space-y-1">
                  <p class="text-sm font-medium">
                    {{ t("notificationSettings.maintenanceWindows") }}
                  </p>
                  <p class="text-xs text-muted-foreground">
                    {{ t("notificationSettings.maintenanceWindowsHint") }}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="quietHoursDisabled"
                  @click="addMaintenanceWindow"
                >
                  <Plus class="mr-1.5 h-4 w-4" />
                  {{ t("notificationSettings.addMaintenanceWindow") }}
                </Button>
              </div>
              <p
                v-if="quietHoursForm.maintenanceWindows.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("notificationSettings.noMaintenanceWindows") }}
              </p>
              <div
                v-for="(window, index) in quietHoursForm.maintenanceWindows"
                :key="index"
                class="rounded-md border p-3"
              >
                <div class="flex flex-wrap items-end gap-3">
                  <div class="min-w-56 flex-1">
                    <FormField
                      v-model="window.start"
                      type="datetime-local"
                      :label="t('notificationSettings.maintenanceWindowStart')"
                      :disabled="quietHoursDisabled"
                    />
                  </div>
                  <div class="min-w-56 flex-1">
                    <FormField
                      v-model="window.end"
                      type="datetime-local"
                      :label="t('notificationSettings.maintenanceWindowEnd')"
                      :disabled="quietHoursDisabled"
                    />
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="text-destructive"
                    :disabled="quietHoursDisabled"
                    @click="removeMaintenanceWindow(index)"
                  >
                    {{ t("common.delete") }}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Routing rules: owner and team rules that select extra channels. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.routes") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.routesDescription") }}
          </CardDescription>
        </div>
        <Button
          size="sm"
          :disabled="!canUpdate"
          @click="openCreateRoute"
        >
          <Plus class="mr-1.5 h-4 w-4" />
          {{ t("notificationSettings.newRoute") }}
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <p
          v-if="!canGetSettings"
          class="px-4 py-6 text-center text-sm text-muted-foreground"
        >
          {{ t("notificationSettings.routesReadOnly") }}
        </p>
        <PageState
          v-else
          :loading="routesLoading"
        >
          <div
            v-if="routes.length === 0"
            class="px-4 py-10 text-center"
          >
            <p class="text-sm font-medium">
              {{ t("notificationSettings.emptyRoutesTitle") }}
            </p>
            <p class="mt-1 text-sm text-muted-foreground">
              {{ t("notificationSettings.emptyRoutesDescription") }}
            </p>
          </div>
          <div
            v-else
            class="rounded-md border"
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("notificationSettings.name") }}</TableHead>
                  <TableHead>{{ t("notificationSettings.status") }}</TableHead>
                  <TableHead>{{ t("notificationSettings.routeMatch") }}</TableHead>
                  <TableHead>{{ t("notificationSettings.routeChannels") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("common.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="route in routes"
                  :key="route.name"
                >
                  <TableCell class="font-medium">
                    {{ route.displayName }}
                  </TableCell>
                  <TableCell>
                    <Badge
                      :variant="route.enabled ? 'default' : 'secondary'"
                      class="font-normal"
                    >
                      {{
                        route.enabled
                          ? t("notificationSettings.enabled")
                          : t("notificationSettings.disabled")
                      }}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground">
                    {{ routeMatchLabel(route) }}
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground">
                    {{ routeChannelsLabel(route) }}
                  </TableCell>
                  <TableCell class="text-right">
                    <div class="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        :disabled="!canUpdate"
                        @click="openEditRoute(route)"
                      >
                        {{ t("common.edit") }}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        class="text-destructive"
                        :disabled="!canUpdate"
                        @click="confirmDeleteRoute(route)"
                      >
                        {{ t("common.delete") }}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Deliveries: what was queued, what was sent, what is stuck. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.deliveries") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.deliveriesDescription") }}
          </CardDescription>
        </div>
        <div class="flex items-center gap-2">
          <Select
            :model-value="deliveryFilter"
            @update:model-value="onDeliveryFilter"
          >
            <SelectTrigger class="h-8 w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">
                {{ t("notificationSettings.allStates") }}
              </SelectItem>
              <SelectItem value="dead">
                {{ t("notificationSettings.stateDead") }}
              </SelectItem>
              <SelectItem value="pending">
                {{ t("notificationSettings.statePending") }}
              </SelectItem>
              <SelectItem value="sending">
                {{ t("notificationSettings.stateSending") }}
              </SelectItem>
              <SelectItem value="sent">
                {{ t("notificationSettings.stateSent") }}
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="sm"
            @click="refreshDeliveries"
          >
            <RefreshCcw class="mr-1.5 h-4 w-4" />
            {{ t("common.refresh") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent class="pt-4">
        <PageState :loading="deliveriesLoading">
          <div
            v-if="deliveries.length === 0"
            class="px-4 py-10 text-center"
          >
            <p class="text-sm font-medium">
              {{ t("notificationSettings.noDeliveriesTitle") }}
            </p>
            <p class="mt-1 text-sm text-muted-foreground">
              {{ t("notificationSettings.noDeliveriesDescription") }}
            </p>
          </div>
          <div
            v-else
            class="rounded-md border"
          >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("notificationSettings.deliveryTime") }}</TableHead>
                <TableHead>{{ t("notificationSettings.channel") }}</TableHead>
                <TableHead>{{ t("notificationSettings.event") }}</TableHead>
                <TableHead>{{ t("notificationSettings.status") }}</TableHead>
                <TableHead>{{ t("notificationSettings.attempts") }}</TableHead>
                <TableHead>{{ t("notificationSettings.nextAttempt") }}</TableHead>
                <TableHead>{{ t("notificationSettings.lastError") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("common.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="delivery in deliveries"
                :key="delivery.name"
              >
                <TableCell class="whitespace-nowrap text-xs text-muted-foreground">
                  {{ formatDateTime(delivery.createTime, locale) }}
                </TableCell>
                <TableCell class="text-xs">
                  {{ delivery.channelName || "—" }}
                </TableCell>
                <TableCell class="whitespace-nowrap text-xs">
                  {{ eventTypeLabel(delivery.eventType) }}
                </TableCell>
                <TableCell>
                  <Badge
                    :variant="deliveryStateVariant(delivery.state)"
                    class="font-normal"
                  >
                    {{ deliveryStateLabel(delivery.state) }}
                  </Badge>
                </TableCell>
                <TableCell class="text-xs">
                  {{ delivery.attempts }}
                </TableCell>
                <TableCell class="whitespace-nowrap text-xs text-muted-foreground">
                  {{
                    delivery.nextAttemptTime
                      ? formatDateTime(delivery.nextAttemptTime, locale)
                      : "—"
                  }}
                </TableCell>
                <TableCell
                  class="max-w-[22rem] truncate text-xs text-muted-foreground"
                  :title="delivery.lastError"
                >
                  {{ delivery.lastError || "—" }}
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    v-if="delivery.state === NotificationDeliveryState.DEAD"
                    variant="ghost"
                    size="sm"
                    :disabled="!canUpdate"
                    @click="retryDelivery(delivery)"
                  >
                    {{ t("notificationSettings.retry") }}
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          </div>
        </PageState>
        <div
          v-if="nextDeliveryToken"
          class="mt-3 flex justify-center"
        >
          <Button
            variant="outline"
            size="sm"
            :disabled="deliveriesLoading"
            @click="loadMoreDeliveries"
          >
            {{ t("notificationSettings.loadMore") }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <!-- Create / edit one channel. -->
    <Dialog
      :open="dialogOpen"
      @update:open="(open) => (dialogOpen = open)"
    >
      <DialogContent class="max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {{
              editingChannel
                ? t("notificationSettings.editChannel")
                : t("notificationSettings.newChannel")
            }}
          </DialogTitle>
          <DialogDescription>
            {{ t("notificationSettings.dialogDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="space-y-4">
          <FormField
            v-model="form.displayName"
            :label="t('notificationSettings.name')"
            :placeholder="t('notificationSettings.namePlaceholder')"
            required
          />

          <div class="space-y-2.5">
            <Label>{{ t("notificationSettings.kind") }}</Label>
            <Select
              v-if="!editingChannel"
              :model-value="String(form.kind)"
              @update:model-value="onKindChange"
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in kindOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ option.label }}
                </SelectItem>
              </SelectContent>
            </Select>
            <p
              v-else
              class="text-sm"
            >
              {{ channelKindLabel(form.kind) }}
            </p>
          </div>

          <template v-if="isEmailKind">
            <FormField
              v-model="form.addresses"
              :label="t('notificationSettings.recipients')"
              :placeholder="t('notificationSettings.recipientsPlaceholder')"
              :hint="t('notificationSettings.recipientsHint')"
            />
            <div class="flex items-start gap-2">
              <Checkbox
                :checked="form.includeAudience"
                @update:checked="onIncludeAudienceChange"
              />
              <Label class="text-sm leading-5">
                {{ t("notificationSettings.includeAudience") }}
              </Label>
            </div>
            <p class="text-xs text-muted-foreground">
              {{ t("notificationSettings.emailSmtpHint") }}
            </p>
          </template>
          <template v-else>
            <FormField
              v-model="form.endpoint"
              :label="t('notificationSettings.endpoint')"
              placeholder="https://..."
              :hint="endpointHint"
              :required="!editingChannel"
            />
            <template v-if="supportsSigning">
              <div class="flex items-center gap-2">
                <Checkbox
                  :checked="form.sign"
                  @update:checked="onSignChange"
                />
                <Label class="text-sm">
                  {{ signingLabel }}
                </Label>
              </div>
              <FormField
                v-if="form.sign"
                v-model="form.signingSecret"
                type="password"
                :label="t('notificationSettings.signingSecret')"
                :hint="signingSecretHint"
              />
            </template>
          </template>

          <div class="grid gap-4 sm:grid-cols-2">
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.minSeverity") }}</Label>
              <Select
                :model-value="String(form.minSeverity)"
                @update:model-value="onMinSeverityChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">
                    {{ t("notificationSettings.severityAny") }}
                  </SelectItem>
                  <SelectItem value="1">
                    {{ t("notificationSettings.severityInfo") }}
                  </SelectItem>
                  <SelectItem value="2">
                    {{ t("notificationSettings.severityWarning") }}
                  </SelectItem>
                  <SelectItem value="3">
                    {{ t("notificationSettings.severityError") }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.language") }}</Label>
              <Select
                :model-value="form.language"
                @update:model-value="onLanguageChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="zh-CN">简体中文</SelectItem>
                  <SelectItem value="en-US">English</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div class="space-y-2.5">
            <Label>{{ t("notificationSettings.eventTypes") }}</Label>
            <div class="space-y-2">
              <label
                v-for="option in eventTypeOptions"
                :key="option.value"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :checked="form.eventTypes.includes(option.value)"
                  @update:checked="(value: boolean | 'indeterminate') => toggleEventType(option.value, value === true)"
                />
                <span>{{ option.label }}</span>
              </label>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.eventTypesHint") }}
              </p>
            </div>
          </div>

          <div class="flex items-center gap-2">
            <Checkbox
              :checked="form.enabled"
              @update:checked="onEnabledChange"
            />
            <Label class="text-sm">{{ t("notificationSettings.enabled") }}</Label>
          </div>

          <div class="space-y-3">
            <label class="flex items-start gap-2 text-sm">
              <Checkbox
                :checked="form.bypassQuietHours"
                @update:checked="onBypassQuietHoursChange"
              />
              <span>
                {{ t("notificationSettings.bypassQuietHours") }}
                <span class="block text-xs text-muted-foreground">
                  {{ t("notificationSettings.bypassQuietHoursHint") }}
                </span>
              </span>
            </label>
            <label class="flex items-start gap-2 text-sm">
              <Checkbox
                :checked="form.routedOnly"
                @update:checked="onRoutedOnlyChange"
              />
              <span>
                {{ t("notificationSettings.routedOnly") }}
                <span class="block text-xs text-muted-foreground">
                  {{ t("notificationSettings.routedOnlyHint") }}
                </span>
              </span>
            </label>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            @click="dialogOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving"
            @click="saveChannel"
          >
            <Loader2
              v-if="saving"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("common.save") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="deleteOpen"
      :title="t('notificationSettings.deleteTitle')"
      :message="t('notificationSettings.deleteDescription')"
      :item-name="pendingDelete?.displayName ?? ''"
      @confirm="deleteChannel"
    />

    <!-- Create / edit one routing rule. -->
    <Dialog
      :open="routeDialogOpen"
      @update:open="(open) => (routeDialogOpen = open)"
    >
      <DialogContent class="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {{
              editingRoute
                ? t("notificationSettings.editRoute")
                : t("notificationSettings.newRoute")
            }}
          </DialogTitle>
          <DialogDescription>
            {{ t("notificationSettings.routeDialogDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="space-y-4">
          <FormField
            v-model="routeForm.displayName"
            :label="t('notificationSettings.name')"
            :placeholder="t('notificationSettings.routeNamePlaceholder')"
            required
          />

          <div class="flex items-center gap-2">
            <Checkbox
              :checked="routeForm.enabled"
              @update:checked="onRouteEnabledChange"
            />
            <Label class="text-sm">{{ t("notificationSettings.enabled") }}</Label>
          </div>

          <div class="space-y-1">
            <p class="text-sm font-medium">
              {{ t("notificationSettings.routeMatch") }}
            </p>
            <p class="text-xs text-muted-foreground">
              {{ t("notificationSettings.routeMatchHint") }}
            </p>
          </div>

          <div class="space-y-2.5">
            <Label>{{ t("notificationSettings.minSeverity") }}</Label>
            <Select
              :model-value="String(routeForm.minSeverity)"
              @update:model-value="onRouteMinSeverityChange"
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">
                  {{ t("notificationSettings.severityAny") }}
                </SelectItem>
                <SelectItem value="1">
                  {{ t("notificationSettings.severityInfo") }}
                </SelectItem>
                <SelectItem value="2">
                  {{ t("notificationSettings.severityWarning") }}
                </SelectItem>
                <SelectItem value="3">
                  {{ t("notificationSettings.severityError") }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="space-y-2">
            <Label>{{ t("notificationSettings.eventTypes") }}</Label>
            <div class="space-y-2">
              <label
                v-for="option in routeEventTypeOptions"
                :key="option.value"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :checked="routeForm.eventTypes.includes(option.value)"
                  @update:checked="(value: boolean | 'indeterminate') => toggleRouteEventType(option.value, value === true)"
                />
                <span>{{ option.label }}</span>
              </label>
            </div>
          </div>

          <div class="space-y-2">
            <div class="space-y-1">
              <Label>{{ t("notificationSettings.routeOwners") }}</Label>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.routeAudienceHint") }}
              </p>
            </div>
            <div class="max-h-40 space-y-2 overflow-y-auto rounded-md border p-3">
              <p
                v-if="users.length === 0"
                class="text-xs text-muted-foreground"
              >
                {{ t("notificationSettings.routeNoUsers") }}
              </p>
              <label
                v-for="user in users"
                :key="user.name"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :checked="routeForm.owners.includes(user.name)"
                  @update:checked="(value: boolean | 'indeterminate') => toggleRouteOwner(user.name, value === true)"
                />
                <span>{{ user.email }}</span>
              </label>
            </div>
          </div>

          <div class="space-y-2">
            <div class="space-y-1">
              <Label>{{ t("notificationSettings.routeGroups") }}</Label>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.routeAudienceHint") }}
              </p>
            </div>
            <div class="max-h-40 space-y-2 overflow-y-auto rounded-md border p-3">
              <p
                v-if="groups.length === 0"
                class="text-xs text-muted-foreground"
              >
                {{ t("notificationSettings.routeNoGroups") }}
              </p>
              <label
                v-for="group in groups"
                :key="group.name"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :checked="routeForm.groups.includes(groupEmail(group))"
                  @update:checked="(value: boolean | 'indeterminate') => toggleRouteGroup(groupEmail(group), value === true)"
                />
                <span>{{ group.title || groupEmail(group) }}</span>
              </label>
            </div>
          </div>

          <div class="space-y-2">
            <div class="space-y-1">
              <Label>{{ t("notificationSettings.routeChannels") }}</Label>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.routeChannelsHint") }}
              </p>
            </div>
            <div class="max-h-40 space-y-2 overflow-y-auto rounded-md border p-3">
              <p
                v-if="channels.length === 0"
                class="text-xs text-muted-foreground"
              >
                {{ t("notificationSettings.routeNoChannels") }}
              </p>
              <label
                v-for="channel in channels"
                :key="channel.name"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :checked="routeForm.channels.includes(channel.name)"
                  @update:checked="(value: boolean | 'indeterminate') => toggleRouteChannel(channel.name, value === true)"
                />
                <span>{{ channel.displayName }}</span>
              </label>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            @click="routeDialogOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="routeSaving"
            @click="saveRoute"
          >
            <Loader2
              v-if="routeSaving"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("common.save") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="routeDeleteOpen"
      :title="t('notificationSettings.deleteRouteTitle')"
      :message="t('notificationSettings.deleteRouteDescription')"
      :item-name="pendingDeleteRoute?.displayName ?? ''"
      @confirm="deleteRoute"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  type Timestamp,
  timestampDate,
  timestampFromDate,
} from "@bufbuild/protobuf/wkt";
import { Loader2, Plus, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listGroups } from "@/api/group";
import {
  createNotificationChannel,
  createNotificationRoute,
  deleteNotificationChannel,
  deleteNotificationRoute,
  listNotificationChannels,
  listNotificationDeliveriesPage,
  listNotificationRoutes,
  retryNotificationDelivery,
  testNotificationChannel,
  updateNotificationChannel,
  updateNotificationRoute,
} from "@/api/notification";
import {
  getEmailSetting,
  getQuietHoursSetting,
  sendTestEmail,
  updateEmailSetting,
  updateQuietHoursSetting,
} from "@/api/setting";
import { listAllUsers } from "@/api/user";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import {
  type NotificationChannel,
  NotificationChannelConfigSchema,
  NotificationChannelKind,
  NotificationChannelSchema,
  NotificationChannelSecretSchema,
  type NotificationDelivery,
  NotificationDeliveryState,
  type NotificationRoute,
  type NotificationRouteMatch,
  NotificationRouteMatchSchema,
  NotificationRouteSchema,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import {
  DayOfWeek,
  EmailSettingSchema,
  type MaintenanceWindow,
  MaintenanceWindowSchema,
  type QuietHoursSetting,
  QuietHoursSettingSchema,
  type QuietWindow,
  QuietWindowSchema,
  SMTPAuthentication,
  SMTPConfigSchema,
  SMTPEncryption,
} from "@/types/proto-es/v1/setting_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * Outbound notification channels. A channel is a destination an administrator
 * configures; every in-app message an event produces is posted to it once. The
 * credential (the endpoint URL, and the signing key when signing) is write-only:
 * the read reports only whether one is stored, so editing keeps it unless the
 * field is filled in again. An email channel has no credential — it delivers
 * through the workspace SMTP setting rendered above the delivery table.
 */

const { t, locale } = useI18n();
const authStore = useAuthStore();
const { formatError } = useErrorHandler();

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);
const canGetSettings = computed(() =>
  authStore.hasPermission("metaxisdata.settings.get")
);

const loading = ref(false);
const channels = ref<NotificationChannel[]>([]);
const testing = ref("");

const deliveriesLoading = ref(false);
const deliveries = ref<NotificationDelivery[]>([]);
const nextDeliveryToken = ref("");
const deliveryFilter = ref<"all" | "pending" | "sending" | "dead" | "sent">(
  "all"
);

const dialogOpen = ref(false);
const saving = ref(false);
const editingChannel = ref<NotificationChannel | null>(null);
const pendingDelete = ref<NotificationChannel | null>(null);
const deleteOpen = ref(false);

interface ChannelForm {
  displayName: string;
  kind: NotificationChannelKind;
  endpoint: string;
  signingSecret: string;
  sign: boolean;
  addresses: string;
  includeAudience: boolean;
  bypassQuietHours: boolean;
  routedOnly: boolean;
  enabled: boolean;
  eventTypes: NotificationType[];
  minSeverity: NotificationSeverity;
  language: string;
}

const form = reactive<ChannelForm>({
  displayName: "",
  kind: NotificationChannelKind.WEBHOOK,
  endpoint: "",
  signingSecret: "",
  sign: false,
  addresses: "",
  includeAudience: false,
  bypassQuietHours: false,
  routedOnly: false,
  enabled: true,
  eventTypes: [],
  minSeverity: NotificationSeverity.UNSPECIFIED,
  language: locale.value === "zh-CN" ? "zh-CN" : "en-US",
});

const isEmailKind = computed(() => form.kind === NotificationChannelKind.EMAIL);

const supportsSigning = computed(
  () =>
    form.kind === NotificationChannelKind.WEBHOOK ||
    form.kind === NotificationChannelKind.DINGTALK ||
    form.kind === NotificationChannelKind.FEISHU
);

const kindOptions = computed(() =>
  [
    NotificationChannelKind.WEBHOOK,
    NotificationChannelKind.DINGTALK,
    NotificationChannelKind.WECOM,
    NotificationChannelKind.FEISHU,
    NotificationChannelKind.EMAIL,
  ].map((value) => ({ value, label: channelKindLabel(value) }))
);

const endpointHint = computed(() => {
  if (editingChannel.value) {
    return t("notificationSettings.endpointKeepHint");
  }
  if (form.kind === NotificationChannelKind.WECOM) {
    return t("notificationSettings.wecomEndpointHint");
  }
  return t("notificationSettings.endpointHint");
});

const signingLabel = computed(() =>
  form.kind === NotificationChannelKind.WEBHOOK
    ? t("notificationSettings.signBody")
    : t("notificationSettings.signRequest")
);

const signingSecretHint = computed(() => {
  if (editingChannel.value) {
    return t("notificationSettings.secretKeepHint");
  }
  if (form.kind === NotificationChannelKind.WEBHOOK) {
    return t("notificationSettings.signingSecretHint");
  }
  return t("notificationSettings.robotSigningSecretHint");
});

/**
 * The event types a channel may subscribe to. The weekly digest is opt-in: an
 * empty list means every event type except it, so a channel receives it only
 * when the box is ticked.
 */
const eventTypeOptions = computed(() => [
  {
    value: NotificationType.DATA_QUALITY,
    label: t("notificationSettings.eventDataQuality"),
  },
  {
    value: NotificationType.DATA_QUALITY_DIGEST,
    label: t("notificationSettings.eventDataQualityDigest"),
  },
  {
    value: NotificationType.SCHEMA_SYNC,
    label: t("notificationSettings.eventSchemaSync"),
  },
  {
    value: NotificationType.OPENLINEAGE,
    label: t("notificationSettings.eventOpenLineage"),
  },
]);

/**
 * Routing rules select events by audience, and the digest has none: it is one
 * workspace summary delivered to the channels that opt in, never per person.
 */
const routeEventTypeOptions = computed(() =>
  eventTypeOptions.value.filter(
    (option) => option.value !== NotificationType.DATA_QUALITY_DIGEST
  )
);

async function loadChannels() {
  loading.value = true;
  try {
    channels.value = await listNotificationChannels();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    loading.value = false;
  }
}

function deliveryStateForFilter(): NotificationDeliveryState {
  switch (deliveryFilter.value) {
    case "pending":
      return NotificationDeliveryState.PENDING;
    case "dead":
      return NotificationDeliveryState.DEAD;
    case "sending":
      return NotificationDeliveryState.SENDING;
    case "sent":
      return NotificationDeliveryState.SENT;
    default:
      return NotificationDeliveryState.UNSPECIFIED;
  }
}

async function loadDeliveries(reset: boolean) {
  deliveriesLoading.value = true;
  try {
    const page = await listNotificationDeliveriesPage({
      pageSize: 50,
      pageToken: reset ? "" : nextDeliveryToken.value,
      state: deliveryStateForFilter(),
    });
    deliveries.value = reset
      ? page.items
      : [...deliveries.value, ...page.items];
    nextDeliveryToken.value = page.nextPageToken;
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    deliveriesLoading.value = false;
  }
}

async function refreshDeliveries() {
  await loadDeliveries(true);
}

function setDeliveryFilter(value: string) {
  deliveryFilter.value = value as typeof deliveryFilter.value;
  deliveries.value = [];
  nextDeliveryToken.value = "";
  void loadDeliveries(true);
}

async function loadMoreDeliveries() {
  await loadDeliveries(false);
}

// ---- Workspace SMTP setting ------------------------------------------------

interface EmailForm {
  from: string;
  fromName: string;
  host: string;
  port: string;
  encryption: SMTPEncryption;
  authentication: SMTPAuthentication;
  username: string;
  password: string;
}

const emailLoading = ref(false);
const emailSaving = ref(false);
const emailTesting = ref(false);
const emailPasswordSet = ref(false);

// A sensible STARTTLS/587/PLAIN default, so an unset workspace starts from the
// settings most providers document and the select always has a valid option.
const emailForm = reactive<EmailForm>({
  from: "",
  fromName: "",
  host: "",
  port: "587",
  encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
  authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
  username: "",
  password: "",
});

async function loadEmailSetting() {
  if (!canGetSettings.value) {
    return;
  }
  emailLoading.value = true;
  try {
    const setting = await getEmailSetting();
    const smtp = setting.smtp;
    emailForm.from = setting.from;
    emailForm.fromName = setting.fromName;
    emailForm.host = smtp?.host ?? "";
    emailForm.port = smtp?.port ? String(smtp.port) : "587";
    // UNSPECIFIED is the zero value of an unset server, so a falsy enum keeps
    // the documented STARTTLS / PLAIN default the select shows.
    emailForm.encryption =
      smtp?.encryption || SMTPEncryption.SMTP_ENCRYPTION_STARTTLS;
    emailForm.authentication =
      smtp?.authentication || SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN;
    emailForm.username = smtp?.username ?? "";
    emailForm.password = "";
    emailPasswordSet.value = setting.passwordSet;
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailLoading.value = false;
  }
}

function buildEmailSetting() {
  return create(EmailSettingSchema, {
    from: emailForm.from.trim(),
    fromName: emailForm.fromName.trim(),
    smtp: create(SMTPConfigSchema, {
      host: emailForm.host.trim(),
      port: Number.parseInt(emailForm.port, 10) || 0,
      encryption: emailForm.encryption,
      authentication: emailForm.authentication,
      username: emailForm.username.trim(),
      password: emailForm.password,
    }),
  });
}

async function saveEmailSetting() {
  if (!canUpdate.value) {
    return;
  }
  emailSaving.value = true;
  try {
    await updateEmailSetting(buildEmailSetting());
    notify.success(t("notificationSettings.emailSaved"));
    await loadEmailSetting();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailSaving.value = false;
  }
}

async function onSendTestEmail() {
  if (!canUpdate.value) {
    return;
  }
  emailTesting.value = true;
  try {
    await sendTestEmail();
    notify.success(t("notificationSettings.testEmailSent"));
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailTesting.value = false;
  }
}

function onEncryptionChange(value: unknown) {
  emailForm.encryption = Number(value) as SMTPEncryption;
}

function onAuthenticationChange(value: unknown) {
  emailForm.authentication = Number(value) as SMTPAuthentication;
}

// ---- Workspace quiet hours -------------------------------------------------

const HHMM_RE = /^([01]\d|2[0-3]):[0-5]\d$/;

const browserTimezone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
})();

// A short curated list; the browser zone and any stored zone are added to it.
const QUIET_HOURS_TIMEZONES = [
  "UTC",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Asia/Kolkata",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Paris",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Australia/Sydney",
];

interface QuietWindowForm {
  days: DayOfWeek[];
  start: string;
  end: string;
}

interface MaintenanceWindowForm {
  start: string;
  end: string;
}

interface QuietHoursFormState {
  enabled: boolean;
  timezone: string;
  windows: QuietWindowForm[];
  maintenanceWindows: MaintenanceWindowForm[];
}

const quietHoursLoading = ref(false);
const quietHoursSaving = ref(false);
const quietHoursForm = reactive<QuietHoursFormState>({
  enabled: false,
  timezone: browserTimezone,
  windows: [],
  maintenanceWindows: [],
});

const quietHoursDisabled = computed(
  () => !canUpdate.value || !quietHoursForm.enabled
);

const timezoneOptions = computed(() => {
  const options = [...QUIET_HOURS_TIMEZONES];
  for (const zone of [browserTimezone, quietHoursForm.timezone]) {
    if (zone && !options.includes(zone)) {
      options.unshift(zone);
    }
  }
  return options;
});

const dayOptions = computed(() => [
  { value: DayOfWeek.MONDAY, label: t("notificationSettings.dayMonday") },
  { value: DayOfWeek.TUESDAY, label: t("notificationSettings.dayTuesday") },
  { value: DayOfWeek.WEDNESDAY, label: t("notificationSettings.dayWednesday") },
  { value: DayOfWeek.THURSDAY, label: t("notificationSettings.dayThursday") },
  { value: DayOfWeek.FRIDAY, label: t("notificationSettings.dayFriday") },
  { value: DayOfWeek.SATURDAY, label: t("notificationSettings.daySaturday") },
  { value: DayOfWeek.SUNDAY, label: t("notificationSettings.daySunday") },
]);

function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

/** A Timestamp as the `YYYY-MM-DDTHH:mm` value a datetime-local input takes. */
function toLocalDateTimeInput(value?: Timestamp): string {
  if (!value) {
    return "";
  }
  const date = timestampDate(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const day = `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(
    date.getDate()
  )}`;
  return `${day}T${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}

/** A `YYYY-MM-DDTHH:mm` value, read as local time, as a Timestamp. */
function fromLocalDateTimeInput(value: string): Timestamp | undefined {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return timestampFromDate(date);
}

async function loadQuietHours() {
  if (!canGetSettings.value) {
    return;
  }
  quietHoursLoading.value = true;
  try {
    const setting = await getQuietHoursSetting();
    quietHoursForm.enabled = setting.enabled;
    quietHoursForm.timezone = setting.timezone || browserTimezone;
    quietHoursForm.windows = setting.windows.map((window) => ({
      days: [...window.days],
      start: window.start,
      end: window.end,
    }));
    quietHoursForm.maintenanceWindows = setting.maintenanceWindows.map(
      (window) => ({
        start: toLocalDateTimeInput(window.start),
        end: toLocalDateTimeInput(window.end),
      })
    );
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    quietHoursLoading.value = false;
  }
}

function onQuietHoursEnabledChange(value: boolean | "indeterminate") {
  quietHoursForm.enabled = value === true;
}

function onQuietHoursTimezoneChange(value: unknown) {
  quietHoursForm.timezone = String(value);
}

function addQuietWindow() {
  quietHoursForm.windows.push({ days: [], start: "", end: "" });
}

function removeQuietWindow(index: number) {
  quietHoursForm.windows.splice(index, 1);
}

function toggleQuietWindowDay(
  window: QuietWindowForm,
  day: DayOfWeek,
  checked: boolean
) {
  if (checked) {
    if (!window.days.includes(day)) {
      window.days.push(day);
    }
    return;
  }
  window.days = window.days.filter((item) => item !== day);
}

function addMaintenanceWindow() {
  quietHoursForm.maintenanceWindows.push({ start: "", end: "" });
}

function removeMaintenanceWindow(index: number) {
  quietHoursForm.maintenanceWindows.splice(index, 1);
}

/**
 * The schedule to write, or null when the form is invalid (a toast says why).
 * A completely blank editor row is skipped instead of blocking the save.
 */
function buildQuietHoursSetting(): QuietHoursSetting | null {
  const enabled = quietHoursForm.enabled;
  const timezone = quietHoursForm.timezone.trim();
  if (enabled && timezone === "") {
    notify.error(t("notificationSettings.quietHoursTimezoneRequired"));
    return null;
  }
  const windows: QuietWindow[] = [];
  for (const window of quietHoursForm.windows) {
    const start = window.start.trim();
    const end = window.end.trim();
    if (window.days.length === 0 && !start && !end) {
      continue;
    }
    if (
      window.days.length === 0 ||
      !HHMM_RE.test(start) ||
      !HHMM_RE.test(end) ||
      start === end
    ) {
      notify.error(t("notificationSettings.quietWindowInvalid"));
      return null;
    }
    windows.push(create(QuietWindowSchema, { days: window.days, start, end }));
  }
  const maintenanceWindows: MaintenanceWindow[] = [];
  for (const window of quietHoursForm.maintenanceWindows) {
    const start = fromLocalDateTimeInput(window.start.trim());
    const end = fromLocalDateTimeInput(window.end.trim());
    if (!start && !end) {
      continue;
    }
    if (!start || !end || start.seconds >= end.seconds) {
      notify.error(t("notificationSettings.maintenanceWindowInvalid"));
      return null;
    }
    maintenanceWindows.push(create(MaintenanceWindowSchema, { start, end }));
  }
  if (enabled && windows.length === 0 && maintenanceWindows.length === 0) {
    notify.error(t("notificationSettings.quietHoursNeedWindow"));
    return null;
  }
  return create(QuietHoursSettingSchema, {
    enabled,
    timezone,
    windows,
    maintenanceWindows,
  });
}

async function saveQuietHours() {
  if (!canUpdate.value) {
    return;
  }
  const setting = buildQuietHoursSetting();
  if (!setting) {
    return;
  }
  quietHoursSaving.value = true;
  try {
    await updateQuietHoursSetting(setting);
    notify.success(t("notificationSettings.quietHoursSaved"));
    await loadQuietHours();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    quietHoursSaving.value = false;
  }
}

// ---- Routing rules ---------------------------------------------------------

const routes = ref<NotificationRoute[]>([]);
const routesLoading = ref(false);
const users = ref<User[]>([]);
const groups = ref<Group[]>([]);

const routeDialogOpen = ref(false);
const routeSaving = ref(false);
const editingRoute = ref<NotificationRoute | null>(null);
const pendingDeleteRoute = ref<NotificationRoute | null>(null);
const routeDeleteOpen = ref(false);

interface RouteForm {
  displayName: string;
  enabled: boolean;
  eventTypes: NotificationType[];
  minSeverity: NotificationSeverity;
  owners: string[];
  groups: string[];
  channels: string[];
}

const routeForm = reactive<RouteForm>({
  displayName: "",
  enabled: true,
  eventTypes: [],
  minSeverity: NotificationSeverity.UNSPECIFIED,
  owners: [],
  groups: [],
  channels: [],
});

async function loadRoutes() {
  if (!canGetSettings.value) {
    return;
  }
  routesLoading.value = true;
  try {
    routes.value = await listNotificationRoutes();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    routesLoading.value = false;
  }
}

// The routing dialog's owner and group pickers are loaded on first open, not on
// page load: a reader with settings.get but without users.list/groups.list would
// otherwise get an error toast every visit, and the routes card itself still works.
let routeChoicesLoaded = false;

async function loadRouteChoices() {
  if (routeChoicesLoaded || !canGetSettings.value) {
    return;
  }
  routeChoicesLoaded = true;
  const [userResult, groupResult] = await Promise.allSettled([
    listAllUsers(),
    listGroups(),
  ]);
  if (userResult.status === "fulfilled") {
    users.value = userResult.value;
  }
  if (groupResult.status === "fulfilled") {
    groups.value = groupResult.value.groups;
  }
}

function resetRouteForm() {
  routeForm.displayName = "";
  routeForm.enabled = true;
  routeForm.eventTypes = [];
  routeForm.minSeverity = NotificationSeverity.UNSPECIFIED;
  routeForm.owners = [];
  routeForm.groups = [];
  routeForm.channels = [];
}

function openCreateRoute() {
  editingRoute.value = null;
  resetRouteForm();
  void loadRouteChoices();
  routeDialogOpen.value = true;
}

function openEditRoute(route: NotificationRoute) {
  editingRoute.value = route;
  void loadRouteChoices();
  routeForm.displayName = route.displayName;
  routeForm.enabled = route.enabled;
  routeForm.eventTypes = [...(route.match?.eventTypes ?? [])];
  routeForm.minSeverity =
    route.match?.minSeverity ?? NotificationSeverity.UNSPECIFIED;
  routeForm.owners = [...(route.match?.owners ?? [])];
  routeForm.groups = [...(route.match?.groups ?? [])];
  routeForm.channels = [...route.channels];
  routeDialogOpen.value = true;
}

function onRouteEnabledChange(value: boolean | "indeterminate") {
  routeForm.enabled = value === true;
}

function onRouteMinSeverityChange(value: unknown) {
  routeForm.minSeverity = Number(value) as NotificationSeverity;
}

function toggleRouteEventType(value: NotificationType, checked: boolean) {
  if (checked) {
    if (!routeForm.eventTypes.includes(value)) {
      routeForm.eventTypes.push(value);
    }
    return;
  }
  routeForm.eventTypes = routeForm.eventTypes.filter((item) => item !== value);
}

function toggleRouteOwner(name: string, checked: boolean) {
  routeForm.owners = checked
    ? [...routeForm.owners, name]
    : routeForm.owners.filter((item) => item !== name);
}

function toggleRouteGroup(email: string, checked: boolean) {
  routeForm.groups = checked
    ? [...routeForm.groups, email]
    : routeForm.groups.filter((item) => item !== email);
}

function toggleRouteChannel(name: string, checked: boolean) {
  routeForm.channels = checked
    ? [...routeForm.channels, name]
    : routeForm.channels.filter((item) => item !== name);
}

function buildRoute(): NotificationRoute {
  return create(NotificationRouteSchema, {
    name: editingRoute.value?.name ?? "",
    displayName: routeForm.displayName.trim(),
    enabled: routeForm.enabled,
    match: create(NotificationRouteMatchSchema, {
      eventTypes: routeForm.eventTypes,
      minSeverity: routeForm.minSeverity,
      owners: routeForm.owners,
      groups: routeForm.groups,
    }),
    channels: routeForm.channels,
  });
}

async function saveRoute() {
  if (routeForm.displayName.trim() === "") {
    notify.error(t("notificationSettings.routeNameRequired"));
    return;
  }
  if (routeForm.channels.length === 0) {
    notify.error(t("notificationSettings.routeChannelsRequired"));
    return;
  }
  routeSaving.value = true;
  try {
    if (editingRoute.value) {
      await updateNotificationRoute(buildRoute(), [
        "display_name",
        "enabled",
        "match",
        "channels",
      ]);
      notify.success(t("notificationSettings.routeUpdated"));
    } else {
      await createNotificationRoute(buildRoute());
      notify.success(t("notificationSettings.routeCreated"));
    }
    routeDialogOpen.value = false;
    await loadRoutes();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    routeSaving.value = false;
  }
}

function confirmDeleteRoute(route: NotificationRoute) {
  pendingDeleteRoute.value = route;
  routeDeleteOpen.value = true;
}

async function deleteRoute() {
  const route = pendingDeleteRoute.value;
  if (!route) {
    return;
  }
  try {
    await deleteNotificationRoute(route.name);
    notify.success(t("notificationSettings.routeDeleted"));
    pendingDeleteRoute.value = null;
    routeDeleteOpen.value = false;
    await loadRoutes();
  } catch (error) {
    notify.error(formatError(error));
  }
}

function groupEmail(group: Group): string {
  return group.name.replace(/^groups\//, "");
}

function userLabel(name: string): string {
  return users.value.find((user) => user.name === name)?.email ?? name;
}

function audienceLabel(match?: NotificationRouteMatch): string {
  const owners = match?.owners ?? [];
  const routeGroups = match?.groups ?? [];
  if (owners.length === 0 && routeGroups.length === 0) {
    return t("notificationSettings.matchAnyAudience");
  }
  return [...owners.map((name) => userLabel(name)), ...routeGroups].join(
    listSeparator.value
  );
}

function routeMatchLabel(route: NotificationRoute): string {
  const match = route.match;
  const types = match?.eventTypes.length
    ? match.eventTypes
        .map((value) => eventTypeLabel(value))
        .join(listSeparator.value)
    : t("notificationSettings.allEvents");
  const severity = severityLabel(
    match?.minSeverity ?? NotificationSeverity.UNSPECIFIED
  );
  return `${types} · ${severity} · ${audienceLabel(match)}`;
}

function channelDisplayName(name: string): string {
  return (
    channels.value.find((channel) => channel.name === name)?.displayName ?? name
  );
}

function routeChannelsLabel(route: NotificationRoute): string {
  if (route.channels.length === 0) {
    return t("notificationSettings.noChannels");
  }
  return route.channels
    .map((name) => channelDisplayName(name))
    .join(listSeparator.value);
}

// ---- Channel dialog --------------------------------------------------------
function resetKindFields() {
  form.endpoint = "";
  form.signingSecret = "";
  form.sign = false;
  form.addresses = "";
  form.includeAudience = false;
}

function resetForm() {
  form.displayName = "";
  form.kind = NotificationChannelKind.WEBHOOK;
  resetKindFields();
  form.bypassQuietHours = false;
  form.routedOnly = false;
  form.enabled = true;
  form.eventTypes = [];
  form.minSeverity = NotificationSeverity.UNSPECIFIED;
  form.language = locale.value === "zh-CN" ? "zh-CN" : "en-US";
}

function openCreate() {
  editingChannel.value = null;
  resetForm();
  dialogOpen.value = true;
}

function openEdit(channel: NotificationChannel) {
  editingChannel.value = channel;
  form.displayName = channel.displayName;
  form.kind = channel.kind;
  resetKindFields();
  const config = channel.config?.config;
  if (
    config?.case === "webhook" ||
    config?.case === "dingtalk" ||
    config?.case === "feishu"
  ) {
    form.sign = Boolean(config.value.sign);
  }
  if (config?.case === "email") {
    form.addresses = config.value.addresses.join(", ");
    form.includeAudience = config.value.includeAudience;
  }
  form.bypassQuietHours = channel.bypassQuietHours;
  form.routedOnly = channel.routedOnly;
  form.enabled = channel.enabled;
  form.eventTypes = [...channel.eventTypes];
  form.minSeverity = channel.minSeverity;
  form.language = channel.language || "en-US";
  dialogOpen.value = true;
}

function onDeliveryFilter(value: unknown) {
  setDeliveryFilter(String(value));
}

function onKindChange(value: unknown) {
  form.kind = Number(value) as NotificationChannelKind;
  resetKindFields();
}

function onSignChange(value: boolean | "indeterminate") {
  form.sign = value === true;
}

function onIncludeAudienceChange(value: boolean | "indeterminate") {
  form.includeAudience = value === true;
}

function onMinSeverityChange(value: unknown) {
  form.minSeverity = Number(value) as NotificationSeverity;
}

function onLanguageChange(value: unknown) {
  form.language = String(value);
}

function onEnabledChange(value: boolean | "indeterminate") {
  form.enabled = value === true;
}

function onBypassQuietHoursChange(value: boolean | "indeterminate") {
  form.bypassQuietHours = value === true;
}

function onRoutedOnlyChange(value: boolean | "indeterminate") {
  form.routedOnly = value === true;
}

function toggleEventType(value: NotificationType, checked: boolean) {
  if (checked) {
    if (!form.eventTypes.includes(value)) {
      form.eventTypes.push(value);
    }
    return;
  }
  form.eventTypes = form.eventTypes.filter((item) => item !== value);
}

/** Recipients as typed: commas, semicolons, spaces or newlines separate addresses. */
function parseRecipients(): string[] {
  return form.addresses
    .split(/[\s,;]+/)
    .map((address) => address.trim())
    .filter((address) => address !== "");
}

function buildConfig() {
  switch (form.kind) {
    case NotificationChannelKind.DINGTALK:
      return create(NotificationChannelConfigSchema, {
        config: { case: "dingtalk", value: { sign: form.sign } },
      });
    case NotificationChannelKind.WECOM:
      return create(NotificationChannelConfigSchema, {
        config: { case: "wecom", value: {} },
      });
    case NotificationChannelKind.FEISHU:
      return create(NotificationChannelConfigSchema, {
        config: { case: "feishu", value: { sign: form.sign } },
      });
    case NotificationChannelKind.EMAIL:
      return create(NotificationChannelConfigSchema, {
        config: {
          case: "email",
          value: {
            addresses: parseRecipients(),
            includeAudience: form.includeAudience,
          },
        },
      });
    default:
      return create(NotificationChannelConfigSchema, {
        config: { case: "webhook", value: { sign: form.sign } },
      });
  }
}

function buildChannel(): NotificationChannel {
  return create(NotificationChannelSchema, {
    name: editingChannel.value?.name ?? "",
    displayName: form.displayName.trim(),
    kind: form.kind,
    enabled: form.enabled,
    eventTypes: form.eventTypes,
    minSeverity: form.minSeverity,
    language: form.language,
    config: buildConfig(),
    bypassQuietHours: form.bypassQuietHours,
    routedOnly: form.routedOnly,
  });
}

function buildSecret() {
  if (isEmailKind.value) {
    return undefined;
  }
  const endpoint = form.endpoint.trim();
  const signingSecret = supportsSigning.value ? form.signingSecret : "";
  if (!endpoint && !signingSecret) {
    return undefined;
  }
  return create(NotificationChannelSecretSchema, {
    endpoint,
    signingSecret,
  });
}

async function saveChannel() {
  if (form.displayName.trim() === "") {
    notify.error(t("notificationSettings.nameRequired"));
    return;
  }
  if (isEmailKind.value) {
    if (parseRecipients().length === 0 && !form.includeAudience) {
      notify.error(t("notificationSettings.recipientsRequired"));
      return;
    }
  } else if (!editingChannel.value && form.endpoint.trim() === "") {
    notify.error(t("notificationSettings.endpointRequired"));
    return;
  }
  if (
    supportsSigning.value &&
    form.sign &&
    !form.signingSecret &&
    !editingChannel.value?.secretSet
  ) {
    notify.error(t("notificationSettings.signingSecretRequired"));
    return;
  }
  saving.value = true;
  try {
    if (editingChannel.value) {
      await updateNotificationChannel(
        buildChannel(),
        [
          "display_name",
          "enabled",
          "event_types",
          "min_severity",
          "language",
          "config",
          "bypass_quiet_hours",
          "routed_only",
        ],
        buildSecret()
      );
      notify.success(t("notificationSettings.updated"));
    } else {
      await createNotificationChannel(buildChannel(), buildSecret());
      notify.success(t("notificationSettings.created"));
    }
    dialogOpen.value = false;
    await loadChannels();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    saving.value = false;
  }
}

async function testChannel(channel: NotificationChannel) {
  testing.value = channel.name;
  try {
    await testNotificationChannel(channel.name);
    notify.success(t("notificationSettings.testSent"));
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    testing.value = "";
  }
}

function confirmDelete(channel: NotificationChannel) {
  pendingDelete.value = channel;
  deleteOpen.value = true;
}

async function deleteChannel() {
  const channel = pendingDelete.value;
  if (!channel) {
    return;
  }
  try {
    await deleteNotificationChannel(channel.name);
    notify.success(t("notificationSettings.deleted"));
    pendingDelete.value = null;
    deleteOpen.value = false;
    await loadChannels();
    await refreshDeliveries();
  } catch (error) {
    notify.error(formatError(error));
  }
}

async function retryDelivery(delivery: NotificationDelivery) {
  try {
    await retryNotificationDelivery(delivery.name);
    notify.success(t("notificationSettings.retried"));
    await refreshDeliveries();
  } catch (error) {
    notify.error(formatError(error));
  }
}

function channelKindLabel(kind: NotificationChannelKind): string {
  switch (kind) {
    case NotificationChannelKind.WEBHOOK:
      return t("notificationSettings.kindWebhook");
    case NotificationChannelKind.DINGTALK:
      return t("notificationSettings.kindDingTalk");
    case NotificationChannelKind.WECOM:
      return t("notificationSettings.kindWeCom");
    case NotificationChannelKind.FEISHU:
      return t("notificationSettings.kindFeishu");
    case NotificationChannelKind.EMAIL:
      return t("notificationSettings.kindEmail");
    default:
      return "—";
  }
}

const listSeparator = computed(() => t("common.listSeparator"));

function subscriptionLabel(channel: NotificationChannel): string {
  const types =
    channel.eventTypes.length === 0
      ? t("notificationSettings.allEvents")
      : channel.eventTypes
          .map((value) => eventTypeLabel(value))
          .join(listSeparator.value);
  return `${types} · ${severityLabel(channel.minSeverity)}`;
}

function eventTypeLabel(value: NotificationType): string {
  switch (value) {
    case NotificationType.DATA_QUALITY:
      return t("notificationSettings.eventDataQuality");
    case NotificationType.DATA_QUALITY_DIGEST:
      return t("notificationSettings.eventDataQualityDigest");
    case NotificationType.SCHEMA_SYNC:
      return t("notificationSettings.eventSchemaSync");
    case NotificationType.OPENLINEAGE:
      return t("notificationSettings.eventOpenLineage");
    default:
      return "—";
  }
}

function severityLabel(value: NotificationSeverity): string {
  switch (value) {
    case NotificationSeverity.INFO:
      return t("notificationSettings.severityInfo");
    case NotificationSeverity.WARNING:
      return t("notificationSettings.severityWarning");
    case NotificationSeverity.ERROR:
      return t("notificationSettings.severityError");
    default:
      return t("notificationSettings.severityAny");
  }
}

function deliveryStateLabel(state: NotificationDeliveryState): string {
  switch (state) {
    case NotificationDeliveryState.PENDING:
      return t("notificationSettings.statePending");
    case NotificationDeliveryState.SENDING:
      return t("notificationSettings.stateSending");
    case NotificationDeliveryState.SENT:
      return t("notificationSettings.stateSent");
    case NotificationDeliveryState.DEAD:
      return t("notificationSettings.stateDead");
    default:
      return "—";
  }
}

function deliveryStateVariant(
  state: NotificationDeliveryState
): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case NotificationDeliveryState.SENT:
      return "default";
    case NotificationDeliveryState.DEAD:
      return "destructive";
    case NotificationDeliveryState.SENDING:
      return "outline";
    default:
      return "secondary";
  }
}

onMounted(async () => {
  await Promise.all([
    loadChannels(),
    loadDeliveries(true),
    loadEmailSetting(),
    loadQuietHours(),
    loadRoutes(),
  ]);
});
</script>

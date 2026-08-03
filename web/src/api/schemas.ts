import { z } from "zod";

export const accountStatusSchema = z.enum(["pending", "active", "error"]);

export const accountSchema = z
  .object({
    id: z.string().min(1),
    name: z.string(),
    real_email: z.string(),
    icloud_email: z.string(),
    host: z.string(),
    status: accountStatusSchema,
    alias_total: z.number().int().nonnegative(),
    alias_active: z.number().int().nonnegative(),
    last_validated: z.string(),
    last_error: z.string(),
    created_at: z.string(),
    has_cookies: z.boolean(),
    has_app_password: z.boolean(),
    proxy_configured: z.boolean(),
  })
  .strip();

export const otpChallengeSchema = z
  .object({
    status: z.literal("otp_required"),
    challenge_id: z.string().min(1),
    expires_in: z.number().int().positive(),
  })
  .strip();

export const aliasSchema = z
  .object({
    email: z.string().min(1),
    anonymousId: z.string().min(1),
    label: z.string(),
    active: z.boolean(),
    createdAt: z.string().optional().default(""),
  })
  .strip();

export const aliasesSchema = z
  .object({
    account_id: z.string().min(1),
    count: z.number().int().nonnegative(),
    aliases: z.array(aliasSchema),
  })
  .strip();

export const createdAliasSchema = z
  .object({
    email: z.string().min(1),
    label: z.string(),
    created_at: z.string(),
    account_id: z.string().min(1),
    batch_id: z.string().optional().default(""),
  })
  .strip();

export const aliasAutomationStatusSchema = z.enum(["success", "partial", "skipped", "error"]);
export const aliasAutomationPauseReasonSchema = z.enum([
  "target_reached",
  "alias_limit",
  "failure_limit",
  "manual",
]);

export const aliasAutomationSchema = z
  .object({
    enabled: z.boolean(),
    interval_minutes: z.number().int().min(5).max(10080),
    allowed_weekdays: z.array(z.number().int().min(0).max(6)).optional().default([]),
    execution_window_start: z.string().max(5).optional().default(""),
    execution_window_end: z.string().max(5).optional().default(""),
    scheduled_batch_size: z.number().int().min(0).max(20),
    minimum_active: z.number().int().min(0).max(100),
    target_active: z.number().int().min(0).max(100),
    max_batch_size: z.number().int().min(1).max(20),
    max_total_aliases: z.number().int().min(1).max(1000).optional().default(1000),
    max_failure_count: z.number().int().min(1).max(10).optional().default(3),
    daily_creation_limit: z.number().int().min(0).max(1000).optional().default(0),
    target_created: z.number().int().min(0).max(1000).optional().default(0),
    label_prefix: z.string().max(196),
    last_run_at: z.string().optional().default(""),
    next_run_at: z.string().optional().default(""),
    last_status: aliasAutomationStatusSchema.optional().or(z.literal("")).default(""),
    last_active: z.number().int().nonnegative(),
    last_created: z.number().int().nonnegative(),
    created_total: z.number().int().nonnegative().optional().default(0),
    consecutive_failure: z.number().int().nonnegative().optional().default(0),
    daily_created: z.number().int().nonnegative().optional().default(0),
    daily_created_date: z.string().optional().default(""),
    pause_reason: aliasAutomationPauseReasonSchema.optional().or(z.literal("")).default(""),
    last_error: z.string().optional().default(""),
  })
  .strip();

export const aliasCreationHistoryAliasSchema = z
  .object({
    created_at: z.string(),
    email: z.string().min(1),
    label: z.string(),
  })
  .strip();

export const aliasCreationHistoryEntrySchema = z
  .object({
    aliases: z.array(aliasCreationHistoryAliasSchema),
    batch_id: z.string().min(1),
    complete: z.boolean(),
    created: z.number().int().nonnegative(),
    created_at: z.string(),
    error: z.string().optional().default(""),
    failed: z.number().int().nonnegative(),
    label_prefix: z.string(),
    requested: z.number().int().nonnegative(),
    status: aliasAutomationStatusSchema,
    trigger: z.enum(["manual", "batch", "automation_manual", "automation_scheduled"]),
  })
  .strip();

export const aliasCreationHistorySchema = z
  .object({
    account_id: z.string().min(1),
    count: z.number().int().nonnegative(),
    entries: z.array(aliasCreationHistoryEntrySchema),
  })
  .strip();

export const aliasBatchResultSchema = z
  .object({
    account_id: z.string().min(1),
    aliases: z.array(createdAliasSchema),
    batch_id: z.string().optional().default(""),
    complete: z.boolean(),
    created: z.number().int().nonnegative(),
    error: z.string().optional().default(""),
    failed: z.number().int().nonnegative(),
    requested: z.number().int().positive(),
  })
  .strip();

export const aliasAutomationRunSchema = z
  .object({
    account_id: z.string().min(1),
    active_before: z.number().int().nonnegative(),
    aliases: z.array(createdAliasSchema),
    automation: aliasAutomationSchema,
    batch_id: z.string().optional().default(""),
    complete: z.boolean(),
    created: z.number().int().nonnegative(),
    error: z.string().optional().default(""),
    failed: z.number().int().nonnegative(),
    requested: z.number().int().nonnegative(),
    status: aliasAutomationStatusSchema,
    trigger: z.enum(["manual", "scheduled"]),
  })
  .strip();

export const aliasAutomationPreviewSchema = z
  .object({
    account_id: z.string().min(1),
    active_aliases: z.number().int().nonnegative(),
    automation: aliasAutomationSchema,
    daily_remaining: z.number().int().nonnegative(),
    max_total_aliases: z.number().int().positive(),
    next_eligible_at: z.string().optional().default(""),
    remaining_total_capacity: z.number().int().nonnegative(),
    requested: z.number().int().nonnegative(),
    schedule_allowed: z.boolean(),
    schedule_reason: z.string().optional().default(""),
    target_remaining: z.number().int().nonnegative(),
    total_aliases: z.number().int().nonnegative(),
  })
  .strip();

export const aliasActionSchema = z
  .object({
    anonymous_id: z.string().min(1),
    success: z.boolean(),
  })
  .strip();

export const deletedAliasSchema = z
  .object({
    anonymous_id: z.string().min(1),
  })
  .strip();

export const inboxMessageSchema = z
  .object({
    id: z.string().min(1),
    from: z.string(),
    to: z.string(),
    subject: z.string(),
    date: z.string(),
    preview: z.string(),
  })
  .strip();

export const inboxSchema = z
  .object({
    account_id: z.string().min(1),
    alias: z.string().optional().default(""),
    count: z.number().int().nonnegative(),
    has_more: z.boolean().optional().default(false),
    method: z.enum(["imap", "web_api"]),
    messages: z.array(inboxMessageSchema),
    next_cursor: z.string().optional().default(""),
  })
  .strip();

export const healthSchema = z
  .object({
    service: z.literal("icloud-hme"),
    version: z.string().min(1),
    status: z.enum(["ok", "degraded"]),
    config_available: z.boolean(),
  })
  .strip();

export const platformAuthStatusSchema = z
  .object({
    authenticated: z.boolean(),
    configured: z.boolean(),
    expires_at: z.string().optional().default(""),
    username: z.string().optional().default(""),
  })
  .strip();

export const operationLogLevelSchema = z.enum(["info", "warning", "error"]);

export const operationLogEntrySchema = z
  .object({
    duration_ms: z.number().int().nonnegative(),
    level: operationLogLevelSchema,
    operation: z.string().min(1),
    status: z.number().int().nonnegative(),
    timestamp: z.string().min(1),
  })
  .strip();

export const operationLogsSchema = z
  .object({
    count: z.number().int().nonnegative(),
    entries: z.array(operationLogEntrySchema),
    retention_days: z.number().int().positive(),
  })
  .strip();

export const emailNotificationSchema = z
  .object({
    configured: z.boolean(),
    enabled: z.boolean(),
    provider: z.literal("163"),
    sender_email: z.string(),
    recipient_email: z.string(),
    smtp_host: z.string(),
    smtp_port: z.number().int().positive(),
  })
  .strip();

export const emailNotificationTestSchema = z
  .object({
    message: z.string().min(1),
  })
  .strip();

export const webhookNotificationSchema = z
  .object({
    configured: z.boolean(),
    enabled: z.boolean(),
    url: z.string(),
  })
  .strip();

export const webhookNotificationTestSchema = z
  .object({
    message: z.string().min(1),
  })
  .strip();

export const reloadedConfigSchema = z
  .object({
    message: z.string().min(1),
  })
  .strip();

export const deletedAccountSchema = z
  .object({
    id: z.string().min(1),
  })
  .strip();

export const apiEnvelopeSchema = z
  .object({
    success: z.boolean(),
    code: z.string().optional(),
    data: z.unknown().optional(),
    message: z.string().optional(),
  })
  .strip();

export type Account = z.infer<typeof accountSchema>;
export type AccountStatus = z.infer<typeof accountStatusSchema>;
export type Alias = z.infer<typeof aliasSchema>;
export type AliasAction = z.infer<typeof aliasActionSchema>;
export type Aliases = z.infer<typeof aliasesSchema>;
export type CreatedAlias = z.infer<typeof createdAliasSchema>;
export type AliasAutomation = z.infer<typeof aliasAutomationSchema>;
export type AliasAutomationRun = z.infer<typeof aliasAutomationRunSchema>;
export type AliasAutomationPreview = z.infer<typeof aliasAutomationPreviewSchema>;
export type AliasBatchResult = z.infer<typeof aliasBatchResultSchema>;
export type AliasCreationHistory = z.infer<typeof aliasCreationHistorySchema>;
export type AliasCreationHistoryEntry = z.infer<typeof aliasCreationHistoryEntrySchema>;
export type DeletedAccount = z.infer<typeof deletedAccountSchema>;
export type DeletedAlias = z.infer<typeof deletedAliasSchema>;
export type Health = z.infer<typeof healthSchema>;
export type PlatformAuthStatus = z.infer<typeof platformAuthStatusSchema>;
export type OperationLogEntry = z.infer<typeof operationLogEntrySchema>;
export type OperationLogs = z.infer<typeof operationLogsSchema>;
export type EmailNotification = z.infer<typeof emailNotificationSchema>;
export type EmailNotificationTestResult = z.infer<typeof emailNotificationTestSchema>;
export type WebhookNotification = z.infer<typeof webhookNotificationSchema>;
export type WebhookNotificationTestResult = z.infer<typeof webhookNotificationTestSchema>;
export type Inbox = z.infer<typeof inboxSchema>;
export type InboxMessage = z.infer<typeof inboxMessageSchema>;
export type OtpChallenge = z.infer<typeof otpChallengeSchema>;
export type ReloadedConfig = z.infer<typeof reloadedConfigSchema>;

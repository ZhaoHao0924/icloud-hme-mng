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
  })
  .strip();

export const aliasAutomationStatusSchema = z.enum(["success", "partial", "skipped", "error"]);

export const aliasAutomationSchema = z
  .object({
    enabled: z.boolean(),
    interval_minutes: z.number().int().min(5).max(10080),
    scheduled_batch_size: z.number().int().min(0).max(20),
    minimum_active: z.number().int().min(0).max(100),
    target_active: z.number().int().min(0).max(100),
    max_batch_size: z.number().int().min(1).max(20),
    label_prefix: z.string().max(196),
    last_run_at: z.string().optional().default(""),
    next_run_at: z.string().optional().default(""),
    last_status: aliasAutomationStatusSchema.optional().or(z.literal("")).default(""),
    last_active: z.number().int().nonnegative(),
    last_created: z.number().int().nonnegative(),
    last_error: z.string().optional().default(""),
  })
  .strip();

export const aliasBatchResultSchema = z
  .object({
    account_id: z.string().min(1),
    aliases: z.array(createdAliasSchema),
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
    complete: z.boolean(),
    created: z.number().int().nonnegative(),
    error: z.string().optional().default(""),
    failed: z.number().int().nonnegative(),
    requested: z.number().int().nonnegative(),
    status: aliasAutomationStatusSchema,
    trigger: z.enum(["manual", "scheduled"]),
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
    method: z.enum(["imap", "web_api"]),
    messages: z.array(inboxMessageSchema),
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
export type AliasBatchResult = z.infer<typeof aliasBatchResultSchema>;
export type DeletedAccount = z.infer<typeof deletedAccountSchema>;
export type DeletedAlias = z.infer<typeof deletedAliasSchema>;
export type Health = z.infer<typeof healthSchema>;
export type Inbox = z.infer<typeof inboxSchema>;
export type InboxMessage = z.infer<typeof inboxMessageSchema>;
export type OtpChallenge = z.infer<typeof otpChallengeSchema>;
export type ReloadedConfig = z.infer<typeof reloadedConfigSchema>;

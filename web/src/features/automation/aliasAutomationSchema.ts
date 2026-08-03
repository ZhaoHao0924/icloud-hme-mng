import { z } from "zod";

const minIntervalMinutes = 5;
const maxIntervalMinutes = 10_080;
const maxBatchSize = 20;
const maxTargetActive = 100;
const maxTargetCreated = 1000;
const maxTotalAliases = 1000;
const maxFailureCount = 10;
const maxDailyCreationLimit = 1000;
const maxLabelPrefixCharacters = 196;

function clockMinutes(value: string) {
  if (!/^\d{2}:\d{2}$/.test(value)) return null;
  const [hours, minutes] = value.split(":").map(Number);
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

export const aliasAutomationFormSchema = z
  .object({
    enabled: z.boolean(),
    intervalMinutes: z
      .number({ message: "请输入执行间隔" })
      .int("执行间隔必须是整数")
      .min(minIntervalMinutes, `执行间隔不能小于 ${minIntervalMinutes} 分钟`)
      .max(maxIntervalMinutes, `执行间隔不能大于 ${maxIntervalMinutes} 分钟`),
    allowedWeekdays: z.array(z.number().int().min(0).max(6)),
    executionWindowStart: z.string().trim(),
    executionWindowEnd: z.string().trim(),
    scheduledBatchSize: z
      .number({ message: "请输入定时创建数量" })
      .int("定时创建数量必须是整数")
      .min(0, "定时创建数量不能小于 0")
      .max(maxBatchSize, `定时创建数量不能大于 ${maxBatchSize}`),
    minimumActive: z
      .number({ message: "请输入库存阈值" })
      .int("库存阈值必须是整数")
      .min(0, "库存阈值不能小于 0")
      .max(maxTargetActive, `库存阈值不能大于 ${maxTargetActive}`),
    targetActive: z
      .number({ message: "请输入补充目标" })
      .int("补充目标必须是整数")
      .min(0, "补充目标不能小于 0")
      .max(maxTargetActive, `补充目标不能大于 ${maxTargetActive}`),
    targetCreated: z
      .number({ message: "请输入累计创建目标" })
      .int("累计创建目标必须是整数")
      .min(0, "累计创建目标不能小于 0")
      .max(maxTargetCreated, `累计创建目标不能大于 ${maxTargetCreated}`),
    maxTotalAliases: z
      .number({ message: "请输入总别名安全上限" })
      .int("总别名安全上限必须是整数")
      .min(1, "总别名安全上限不能小于 1")
      .max(maxTotalAliases, `总别名安全上限不能大于 ${maxTotalAliases}`),
    maxFailureCount: z
      .number({ message: "请输入连续失败上限" })
      .int("连续失败上限必须是整数")
      .min(1, "连续失败上限不能小于 1")
      .max(maxFailureCount, `连续失败上限不能大于 ${maxFailureCount}`),
    dailyCreationLimit: z
      .number({ message: "请输入每日自动创建上限" })
      .int("每日自动创建上限必须是整数")
      .min(0, "每日自动创建上限不能小于 0")
      .max(maxDailyCreationLimit, `每日自动创建上限不能大于 ${maxDailyCreationLimit}`),
    maxBatchSize: z
      .number({ message: "请输入单次上限" })
      .int("单次上限必须是整数")
      .min(1, "单次上限不能小于 1")
      .max(maxBatchSize, `单次上限不能大于 ${maxBatchSize}`),
    labelPrefix: z
      .string()
      .trim()
      .refine((value) => Array.from(value).length <= maxLabelPrefixCharacters, {
        message: `标签前缀不能超过 ${maxLabelPrefixCharacters} 个字符`,
      }),
  })
  .superRefine((values, context) => {
    if (new Set(values.allowedWeekdays).size !== values.allowedWeekdays.length) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "执行日不能重复",
        path: ["allowedWeekdays"],
      });
    }
    const start = values.executionWindowStart;
    const end = values.executionWindowEnd;
    if ((start === "") !== (end === "")) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "开始和结束时间需要同时设置",
        path: start === "" ? ["executionWindowStart"] : ["executionWindowEnd"],
      });
    } else if (start !== "" && end !== "") {
      const startMinutes = clockMinutes(start);
      const endMinutes = clockMinutes(end);
      if (startMinutes === null) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          message: "开始时间格式无效",
          path: ["executionWindowStart"],
        });
      }
      if (endMinutes === null) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          message: "结束时间格式无效",
          path: ["executionWindowEnd"],
        });
      }
      if (startMinutes !== null && endMinutes !== null && startMinutes >= endMinutes) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          message: "结束时间必须晚于开始时间",
          path: ["executionWindowEnd"],
        });
      }
    }
    if (values.scheduledBatchSize > values.maxBatchSize) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "定时创建数量不能大于单次上限",
        path: ["scheduledBatchSize"],
      });
    }
    if (values.minimumActive === 0 && values.targetActive !== 0) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "库存阈值为 0 时，补充目标也必须为 0",
        path: ["targetActive"],
      });
    }
    if (values.minimumActive > 0 && values.targetActive < values.minimumActive) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "补充目标不能小于库存阈值",
        path: ["targetActive"],
      });
    }
    if (
      values.enabled &&
      values.scheduledBatchSize === 0 &&
      values.minimumActive === 0 &&
      values.targetCreated === 0
    ) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "启用自动化前请设置定时创建数量、库存阈值或累计创建目标",
        path: ["enabled"],
      });
    }
  });

export const batchCreateFormSchema = z.object({
  count: z
    .number({ message: "请输入创建数量" })
    .int("创建数量必须是整数")
    .min(1, "创建数量不能小于 1")
    .max(maxBatchSize, `创建数量不能大于 ${maxBatchSize}`),
  labelPrefix: z
    .string()
    .trim()
    .refine((value) => Array.from(value).length <= maxLabelPrefixCharacters, {
      message: `标签前缀不能超过 ${maxLabelPrefixCharacters} 个字符`,
    }),
});

export type AliasAutomationFormValues = z.infer<typeof aliasAutomationFormSchema>;
export type BatchCreateFormValues = z.infer<typeof batchCreateFormSchema>;

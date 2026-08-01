import { z } from "zod";

const minIntervalMinutes = 5;
const maxIntervalMinutes = 10_080;
const maxBatchSize = 20;
const maxTargetActive = 100;
const maxLabelPrefixCharacters = 196;

export const aliasAutomationFormSchema = z
  .object({
    enabled: z.boolean(),
    intervalMinutes: z
      .number({ message: "请输入执行间隔" })
      .int("执行间隔必须是整数")
      .min(minIntervalMinutes, `执行间隔不能小于 ${minIntervalMinutes} 分钟`)
      .max(maxIntervalMinutes, `执行间隔不能大于 ${maxIntervalMinutes} 分钟`),
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
    if (values.enabled && values.scheduledBatchSize === 0 && values.minimumActive === 0) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: "启用自动化前请设置定时创建数量或库存阈值",
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

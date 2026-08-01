import { z } from "zod";

export const maxAliasLabelCharacters = 200;

export const createAliasSchema = z.object({
  label: z
    .string()
    .trim()
    .refine((value) => Array.from(value).length <= maxAliasLabelCharacters, {
      message: `标签不能超过 ${maxAliasLabelCharacters} 个字符`,
    }),
});

export type CreateAliasValues = z.infer<typeof createAliasSchema>;

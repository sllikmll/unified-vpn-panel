import { z } from 'zod';

export const ConfigProfileSchema = z.object({
  id: z.number().int().nonnegative(),
  name: z.string(),
  description: z.string().default(''),
  enabled: z.boolean(),
  version: z.number().int().positive(),
  profile: z.string(),
  createdAt: z.number().int().nonnegative(),
  updatedAt: z.number().int().nonnegative(),
});

export type ConfigProfile = z.infer<typeof ConfigProfileSchema>;

export const ConfigProfileListSchema = z.array(ConfigProfileSchema);

export const ConfigProfileFormSchema = z.object({
  name: z.string().trim().min(1).max(128),
  description: z.string().max(1024).default(''),
  enabled: z.boolean().default(true),
  version: z.number().int().positive().default(1),
  profile: z.string().trim().min(2).refine((value) => {
    try {
      JSON.parse(value);
      return true;
    } catch {
      return false;
    }
  }, 'pages.profiles.toasts.invalidJson'),
});

export type ConfigProfileFormValues = z.infer<typeof ConfigProfileFormSchema>;

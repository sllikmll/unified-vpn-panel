import { z } from 'zod';

import {
  ManagedEndpointCapabilitiesSchema as GeneratedManagedEndpointCapabilitiesSchema,
  ManagedEndpointCapabilitySchema as GeneratedManagedEndpointCapabilitySchema,
  ManagedEndpointViewSchema as GeneratedManagedEndpointViewSchema,
  ManagedHealthViewSchema as GeneratedManagedHealthViewSchema,
  ManagedSecretSummarySchema as GeneratedManagedSecretSummarySchema,
  ManagedTrafficViewSchema as GeneratedManagedTrafficViewSchema,
} from '@/generated/zod';

export const ManagedTrafficViewSchema = GeneratedManagedTrafficViewSchema.strict();
export const ManagedHealthViewSchema = GeneratedManagedHealthViewSchema.strict();
export const ManagedSecretSummarySchema = GeneratedManagedSecretSummarySchema.strict();
export const ManagedEndpointViewSchema = GeneratedManagedEndpointViewSchema.extend({
  traffic: ManagedTrafficViewSchema,
  health: ManagedHealthViewSchema,
  secretSummary: ManagedSecretSummarySchema,
  config: z.unknown().optional(),
  installPlan: z.unknown().optional(),
  capabilities: z.unknown().optional(),
  actionPlan: z.unknown().optional(),
}).strict();
export const ManagedEndpointCapabilitySchema = GeneratedManagedEndpointCapabilitySchema.strict();
export const ManagedEndpointCapabilitiesSchema = GeneratedManagedEndpointCapabilitiesSchema.extend({
  runtimeKinds: z.array(ManagedEndpointCapabilitySchema),
}).strict();

export const ManagedInstallPlanSchema = z.object({
  runtimeKind: z.string(),
  supported: z.boolean(),
  blocked: z.boolean(),
  requiresPinnedImage: z.boolean().optional(),
  imageRef: z.string().optional(),
  artifactRef: z.string().optional(),
  version: z.string().optional(),
  reason: z.string().optional(),
  capabilities: z.array(z.string()).optional(),
  backendProfiles: z.array(z.object({
    kind: z.string(),
    containerName: z.string().optional(),
    hostConfigDir: z.string().optional(),
    containerConfigDir: z.string().optional(),
  }).passthrough()).optional(),
}).strict();

export type ManagedEndpoint = z.infer<typeof ManagedEndpointViewSchema>;
export type ManagedEndpointCapabilities = z.infer<typeof ManagedEndpointCapabilitiesSchema>;
export type ManagedInstallPlan = z.infer<typeof ManagedInstallPlanSchema>;

export const ManagedEndpointListSchema = z.array(ManagedEndpointViewSchema);
export const ManagedInstallPlanListSchema = z.array(ManagedInstallPlanSchema);

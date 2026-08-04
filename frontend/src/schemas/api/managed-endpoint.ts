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
}).strict();
export const ManagedEndpointCapabilitySchema = GeneratedManagedEndpointCapabilitySchema.strict();
export const ManagedEndpointCapabilitiesSchema = GeneratedManagedEndpointCapabilitiesSchema.extend({
  runtimeKinds: z.array(ManagedEndpointCapabilitySchema),
}).strict();

export type ManagedEndpoint = z.infer<typeof ManagedEndpointViewSchema>;
export type ManagedEndpointCapabilities = z.infer<typeof ManagedEndpointCapabilitiesSchema>;

export const ManagedEndpointListSchema = z.array(ManagedEndpointViewSchema);

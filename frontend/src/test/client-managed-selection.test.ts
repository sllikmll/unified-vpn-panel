import { describe, expect, it } from 'vitest';

import { ClientCreateFormSchema } from '@/schemas/client';

const base = {
  email: 'client',
  subId: 'sub-id',
  uuid: '',
  password: '',
  auth: '',
  flow: '',
  security: 'auto',
  reverseTag: '',
  totalGB: 0,
  delayedStart: false,
  delayedDays: 0,
  reset: 0,
  limitIp: 0,
  tgId: 0,
  group: '',
  comment: '',
  enable: true,
};

describe('ClientCreateFormSchema managed targets', () => {
  it('accepts a managed-only create', () => {
    expect(ClientCreateFormSchema.safeParse({ ...base, inboundIds: [], managedEndpointIds: ['managed:1'] }).success).toBe(true);
  });

  it('rejects mixing legacy and managed lifecycles', () => {
    expect(ClientCreateFormSchema.safeParse({ ...base, inboundIds: [1], managedEndpointIds: ['managed:1'] }).success).toBe(false);
  });
});

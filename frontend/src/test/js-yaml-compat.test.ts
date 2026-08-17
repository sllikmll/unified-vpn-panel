import { describe, expect, it } from 'vitest';
import YAML, { JSON_SCHEMA, load } from 'js-yaml';

describe('js-yaml v5 compatibility adapter', () => {
  it('preserves Swagger Client default and named imports', () => {
    expect(YAML.load('enabled: true')).toEqual({ enabled: true });
    expect(load('port: 443', { schema: JSON_SCHEMA })).toEqual({ port: 443 });
  });
});

export function normalizeClientTargetSelection(targets: string[], previous: string[]): string[] {
  const added = targets.find((value) => !previous.includes(value));
  if (!added) return targets;

  const prefix = added.startsWith('managed:')
    ? 'managed:'
    : added.startsWith('legacy:')
      ? 'legacy:'
      : '';
  return prefix ? targets.filter((value) => value.startsWith(prefix)) : targets;
}

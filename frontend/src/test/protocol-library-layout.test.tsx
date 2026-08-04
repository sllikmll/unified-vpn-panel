import { render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import ProtocolLibraryPage from '@/pages/protocol-library/ProtocolLibraryPage';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => <aside data-testid="sidebar" /> }));
vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ isDark: false, isUltra: false, antdThemeConfig: {} }),
}));
vi.mock('@/api/queries/useProtocolConnections', async () => {
  const actual = await vi.importActual<typeof import('@/api/queries/useProtocolConnections')>('@/api/queries/useProtocolConnections');
  return {
    ...actual,
    useProtocolConnectionsQuery: () => ({
      connections: [],
      protocols: [],
      loading: false,
      fetchError: null,
      refetch: vi.fn(),
    }),
    useProtocolConnectionMutations: () => ({
      importConnection: vi.fn(),
      updateConnection: vi.fn(),
      deleteConnection: vi.fn(),
      preview: vi.fn(),
      revealConnection: vi.fn(),
      importing: false,
      updating: false,
      deleting: false,
      previewing: false,
      revealing: false,
    }),
  };
});

describe('ProtocolLibraryPage layout', () => {
  it('uses the standard horizontal application layout', () => {
    const { container, getByTestId } = render(<ProtocolLibraryPage />);
    const rootLayout = getByTestId('sidebar').parentElement;
    const contentShell = container.querySelector('.content-shell');
    const contentArea = container.querySelector('.content-area');
    const page = container.querySelector('.protocol-library-page');

    expect(rootLayout?.classList.contains('ant-layout')).toBe(true);
    expect(contentShell?.parentElement).toBe(rootLayout);
    expect(contentArea?.contains(page)).toBe(true);
    expect(page?.tagName).toBe('DIV');
  });
});

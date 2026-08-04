import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';

import { Status } from '@/models/status';
import OverviewActionBar from '@/pages/index/OverviewActionBar';

function renderBar(readOnly = false) {
  const handlers = {
    onStopXray: vi.fn(),
    onRestartXray: vi.fn(),
    onOpenLogs: vi.fn(),
    onOpenXrayLogs: vi.fn(),
    onOpenConfig: vi.fn(),
    onOpenBackup: vi.fn(),
    onOpenSystemHistory: vi.fn(),
    onOpenXrayMetrics: vi.fn(),
    onOpenPanelUpdate: vi.fn(),
    onOpenVersionSwitch: vi.fn(),
  };
  render(
    <OverviewActionBar
      status={new Status({ xray: { state: 'running', version: '26.6.27' } })}
      isMobile={false}
      accessLogEnable
      panelVersion="0.0.1"
      latestVersion="0.0.2"
      updateAvailable
      readOnly={readOnly}
      {...handlers}
    />,
  );
  return handlers;
}

test('remote read-only action bar suppresses local-only actions', () => {
  const handlers = renderBar(true);

  expect(screen.getByText('Read-only target')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Restart' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
  expect(screen.queryByText('Update v0.0.2')).toBeNull();

  fireEvent.click(screen.getByText('v26.6.27'));
  expect(handlers.onOpenVersionSwitch).not.toHaveBeenCalled();
  expect(handlers.onRestartXray).not.toHaveBeenCalled();
  expect(handlers.onOpenPanelUpdate).not.toHaveBeenCalled();
});

test('local action bar keeps local server controls', () => {
  const handlers = renderBar(false);

  fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
  expect(handlers.onRestartXray).toHaveBeenCalledTimes(1);
  expect(screen.getByText('Update v0.0.2')).toBeTruthy();
});

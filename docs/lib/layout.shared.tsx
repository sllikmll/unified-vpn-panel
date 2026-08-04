import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { Logo } from '@/components/logo';
import { appName, productRepoUrl } from './shared';
import { getSiteMessages } from './site-i18n';

// Build locale-aware shared layout options. With `hideLocale: 'default-locale'`,
// English URLs have no prefix while other locales are prefixed (`/fa`, `/ru`, `/zh`).
export function baseOptions(lang: string): BaseLayoutProps {
  const prefix = lang === 'en' ? '' : `/${lang}`;
  const m = getSiteMessages(lang);

  return {
    nav: {
      title: (
        <span className="inline-flex items-center gap-2 font-semibold">
          <Logo className="h-6" />
          {appName}
        </span>
      ),
      url: `${prefix}/`,
    },
    githubUrl: productRepoUrl,
    links: [
      {
        text: m.documentation,
        url: `${prefix}/docs`,
        active: 'nested-url',
      },
    ],
  };
}

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  ArrowRight01Icon,
  LinkSquare01Icon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { ProviderBadge } from '@/components/provider-badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { useSystemConfig } from '@/hooks/use-system-config'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

const MODEL_SIGNALS = ['OpenAI', 'Claude', 'Gemini', 'DeepSeek'] as const

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { systemName, logo } = useSystemConfig()
  // 账号由管理员创建，访客统一从「开始使用」进入登录页
  const primaryDestination = props.isAuthenticated ? '/wallet' : '/sign-in'
  const primaryLabel = props.isAuthenticated
    ? t('Open wallet')
    : t('Get Started')

  return (
    <section
      aria-labelledby='home-hero-title'
      className='niu-dali-hero relative isolate overflow-hidden px-5 pt-20 pb-16 sm:px-8 md:pt-24 md:pb-24 lg:px-10'
    >
      <div
        aria-hidden='true'
        className='bg-primary/5 pointer-events-none absolute inset-y-0 right-0 -z-10 w-1/2 [mask-image:linear-gradient(to_right,transparent,black)]'
      />

      <div
        data-slot='hero-layout'
        className='mx-auto grid max-w-6xl min-w-0 items-center gap-12 lg:grid-cols-[1.1fr_1fr] lg:gap-16'
      >
        <div className='min-w-0'>
          <div className='niu-dali-kicker mb-7 inline-flex max-w-full items-center gap-2.5 rounded-lg border px-3 py-2'>
            <img src={logo} alt='' className='size-5 shrink-0' />
            <span className='text-xs font-semibold'>Niu Dali</span>
            <span
              aria-hidden='true'
              className='h-4 w-px bg-current opacity-25'
            />
            <span className='text-xs opacity-70'>{t('Token access')}</span>
          </div>

          <p className='niu-dali-eyebrow mb-4 text-sm font-semibold'>
            {t('Token access, made straightforward')}
          </p>
          <h1
            id='home-hero-title'
            className='max-w-xl text-5xl leading-[1.08] font-semibold tracking-tight text-balance [overflow-wrap:anywhere] sm:text-6xl'
          >
            {systemName}
          </h1>
          <p className='mt-5 max-w-xl text-xl leading-8 font-medium text-balance sm:text-2xl sm:leading-9'>
            {t('One balance for the AI models you use.')}
          </p>
          <p className='niu-dali-hero-muted mt-4 max-w-xl text-sm leading-7 sm:text-base'>
            {t(
              'Buy flexible credit, create an API key, and see every request in one place.'
            )}
          </p>

          <div className='mt-8 flex flex-wrap items-center gap-3'>
            <Button
              size='lg'
              className='niu-dali-primary-action h-11 gap-2 px-5 shadow-sm'
              render={<Link to={primaryDestination} />}
            >
              <HugeiconsIcon icon={Wallet01Icon} data-icon='inline-start' />
              {primaryLabel}
              <HugeiconsIcon icon={ArrowRight01Icon} data-icon='inline-end' />
            </Button>
            <Button
              size='lg'
              variant='outline'
              className='niu-dali-secondary-action h-11 px-5'
              render={<Link to='/pricing' />}
            >
              {t('View Pricing')}
            </Button>
          </div>

          <div className='niu-dali-hero-muted mt-8 flex flex-wrap gap-x-5 gap-y-3 text-xs'>
            {[
              t('Pay as you go'),
              t('Clear model pricing'),
              t('Request-level usage records'),
            ].map((promise) => (
              <span className='flex items-center gap-2' key={promise}>
                <span
                  aria-hidden='true'
                  className='bg-primary/70 size-1 rounded-full'
                />
                {promise}
              </span>
            ))}
          </div>
        </div>

        <div aria-hidden='true' className='relative min-w-0 lg:py-6'>
          <div className='bg-primary/5 border-primary/10 absolute inset-4 -z-10 translate-x-3 translate-y-3 rounded-xl border sm:translate-x-4 sm:translate-y-4' />
          <Card className='border-border shadow-primary/5 min-w-0 gap-0 rounded-xl border py-0 shadow-xl ring-0'>
            <div className='border-border flex min-w-0 items-center justify-between gap-4 border-b px-5 py-4 sm:px-6'>
              <div className='flex min-w-0 items-center gap-3'>
                <div className='bg-primary/5 border-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg border'>
                  <img src={logo} alt='' className='size-6' />
                </div>
                <div className='min-w-0'>
                  <p className='truncate text-sm font-semibold'>{systemName}</p>
                  <p className='text-muted-foreground mt-0.5 text-xs'>
                    {t('Token access')}
                  </p>
                </div>
              </div>
              <span className='bg-primary/5 text-primary inline-flex shrink-0 items-center gap-2 rounded-md px-2.5 py-1.5 font-mono text-[11px]'>
                <span className='bg-primary size-1.5 rounded-full' />
                API
              </span>
            </div>

            <div className='p-5 sm:p-6'>
              <div className='bg-muted/40 border-border rounded-lg border p-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('One key, more models')}
                </p>
                <div className='mt-3 flex min-w-0 items-center justify-between gap-3'>
                  <code className='min-w-0 truncate text-xs tracking-wide sm:text-sm'>
                    nd_live_••••••••••••
                  </code>
                  <HugeiconsIcon
                    icon={LinkSquare01Icon}
                    className='text-primary size-4 shrink-0'
                  />
                </div>
              </div>

              <div className='border-primary/20 mx-auto h-6 w-px border-l border-dashed' />

              <div className='grid grid-cols-2 gap-3'>
                {MODEL_SIGNALS.map((model) => (
                  <div
                    className='border-border flex min-w-0 flex-col gap-4 rounded-lg border p-3.5'
                    key={model}
                  >
                    <ProviderBadge
                      label={model}
                      iconKey={model}
                      iconSize={20}
                      colorText={false}
                      copyable={false}
                      type='text'
                      className='gap-2'
                    />
                    <div className='flex items-center justify-between gap-2'>
                      <div className='flex flex-1 gap-1'>
                        <span className='bg-primary/30 h-1 w-6 rounded-full' />
                        <span className='bg-primary/15 h-1 w-4 rounded-full' />
                        <span className='bg-primary/10 h-1 w-8 rounded-full' />
                      </div>
                      <span className='text-muted-foreground font-mono text-[10px]'>
                        API
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className='border-border bg-muted/25 flex flex-wrap items-center justify-between gap-2 border-t px-5 py-3.5 text-xs sm:px-6'>
              <span className='text-muted-foreground'>
                {t('Shared balance')}
              </span>
              <span className='text-primary font-medium'>
                {t('Pay as you go')}
              </span>
            </div>
          </Card>
        </div>
      </div>
    </section>
  )
}

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
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

export function HowItWorks() {
  const { t } = useTranslation()
  const steps = [
    {
      number: '01',
      title: t('Add balance'),
      description: t(
        'Start with flexible credit and adjust as your workload grows.'
      ),
    },
    {
      number: '02',
      title: t('Create an API key'),
      description: t(
        'Use a standard key in the tools and SDKs you already know.'
      ),
    },
    {
      number: '03',
      title: t('Track every request'),
      description: t(
        'Review usage and remaining credit from the same console.'
      ),
    },
  ]

  return (
    <section className='border-border bg-muted/35 border-y px-5 py-20 sm:px-8 md:py-24 lg:px-10'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
          <div>
            <p className='text-primary text-sm font-medium'>
              {t('How It Works')}
            </p>
            <h2 className='mt-3 text-3xl font-semibold tracking-tight text-balance md:text-4xl'>
              {t('From balance to first request')}
            </h2>
          </div>
          <p className='text-muted-foreground max-w-md text-sm leading-7'>
            {t('Three clear steps, with no provider-specific setup to learn.')}
          </p>
        </AnimateInView>

        <ol className='mt-10 grid gap-5 md:grid-cols-3'>
          {steps.map((step, index) => (
            <AnimateInView
              as='li'
              className='border-border bg-card shadow-foreground/3 relative min-w-0 rounded-xl border p-6 shadow-sm'
              delay={index * 100}
              key={step.number}
            >
              <span className='bg-primary/8 text-primary inline-flex size-10 items-center justify-center rounded-lg text-sm font-semibold tabular-nums'>
                {step.number}
              </span>
              <h3 className='mt-6 text-lg font-semibold'>{step.title}</h3>
              <p className='text-muted-foreground mt-2 max-w-xs text-sm leading-6'>
                {step.description}
              </p>
            </AnimateInView>
          ))}
        </ol>
      </div>
    </section>
  )
}

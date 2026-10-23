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

interface StatsProps {
  className?: string
}

export function Stats(_props: StatsProps) {
  const { t } = useTranslation()
  const promises = [
    {
      title: t('Shared balance'),
      description: t('Across supported models'),
    },
    {
      title: t('Compatible API'),
      description: t('Works with familiar tools'),
    },
    {
      title: t('Visible usage'),
      description: t('Follow every request'),
    },
    {
      title: t('Top up as needed'),
      description: t('No long-term commitment'),
    },
  ]

  return (
    <section className='border-border bg-card border-y px-5 sm:px-8 lg:px-10'>
      <div className='divide-border mx-auto grid max-w-6xl grid-cols-2 py-3 lg:grid-cols-4 lg:py-4'>
        {promises.map((item) => (
          <div
            className='border-border flex min-h-24 min-w-0 flex-col justify-center border-b px-4 py-5 even:border-l nth-[n+3]:border-b-0 sm:px-6 lg:border-b-0 lg:not-first:border-l'
            key={item.title}
          >
            <p className='text-sm font-semibold [overflow-wrap:anywhere]'>
              {item.title}
            </p>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {item.description}
            </p>
          </div>
        ))}
      </div>
    </section>
  )
}

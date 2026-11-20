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

import { CopyButton } from '@/components/copy-button'

export function ApiCodeExample(props: { title: string; code: string }) {
  const { t } = useTranslation()

  return (
    <div className='my-4 min-w-0 overflow-hidden rounded-xl border'>
      <div className='bg-muted/50 flex min-w-0 items-center justify-between gap-3 border-b px-4 py-2'>
        <p className='min-w-0 text-sm font-medium break-words'>{props.title}</p>
        <CopyButton
          value={props.code}
          size='sm'
          aria-label={t('Copy {{example}}', { example: props.title })}
        >
          {t('Copy')}
        </CopyButton>
      </div>
      <pre
        aria-label={props.title}
        tabIndex={0}
        className='bg-muted/20 max-w-full overflow-x-auto p-4 text-xs leading-6 sm:text-sm'
      >
        <code>{props.code}</code>
      </pre>
    </div>
  )
}

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
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

export function NotFoundError() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  return (
    <div className='bg-background flex min-h-svh items-center justify-center p-4 sm:p-8'>
      <div className='bg-card m-auto flex w-full max-w-xl flex-col items-center justify-center gap-3 rounded-xl border px-6 py-12 text-center shadow-sm sm:px-10 sm:py-14'>
        <h1 className='text-primary text-[5rem] leading-none font-semibold tracking-tight sm:text-[6rem]'>
          404
        </h1>
        <span className='text-xl font-semibold tracking-tight'>
          {t('Oops! Page Not Found!')}
        </span>
        <p className='text-muted-foreground text-center text-sm leading-6'>
          {t("It seems like the page you're looking for")} <br />
          {t('does not exist or might have been removed.')}
        </p>
        <div className='mt-5 flex max-w-full flex-wrap justify-center gap-3'>
          <Button variant='outline' onClick={() => history.go(-1)}>
            {t('Go Back')}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>
            {t('Back to Home')}
          </Button>
        </div>
      </div>
    </div>
  )
}

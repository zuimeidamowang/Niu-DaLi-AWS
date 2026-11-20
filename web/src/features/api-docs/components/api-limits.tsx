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

export function ApiLimits() {
  const { t } = useTranslation()

  return (
    <section id='limits' className='scroll-mt-24 space-y-4'>
      <h2 className='text-2xl font-semibold'>{t('Limits and errors')}</h2>
      <p className='text-muted-foreground leading-7'>
        {t(
          'Default upload limits: 50 MiB per image, 500 MiB per video and 5 GiB of stored media per account. Administrators can change these limits.'
        )}
      </p>
      <p className='text-muted-foreground leading-7'>
        {t(
          'Original uploads are removed 24 hours after their last associated task finishes. Unused uploads expire after 24 hours. Re-upload expired media before creating another task.'
        )}
      </p>
      <p className='text-muted-foreground leading-7'>
        {t(
          'Upload image or video files, not local paths, browser blob URLs or Base64 strings in asset_id. Supported formats and file sizes are checked by the upload service; model-specific media requirements still apply.'
        )}
      </p>
      <ul className='list-disc space-y-2 pl-5 text-sm leading-7'>
        <li>
          <code>400</code> —{' '}
          {t('Check the request fields, roles and model settings.')}
        </li>
        <li>
          <code>401 / 403</code> —{' '}
          {t('Check your API key, account permissions and model access.')}
        </li>
        <li>
          <code>404</code> —{' '}
          {t(
            'The asset or task is unavailable, expired or belongs to another account.'
          )}
        </li>
        <li>
          <code>413</code> — {t('The upload exceeds the allowed file size.')}
        </li>
        <li>
          <code>429</code> —{' '}
          {t(
            'Check error.code: media_storage_quota_exceeded means your stored media quota is full; other rate limits require waiting before retrying.'
          )}
        </li>
        <li>
          <code>503</code> —{' '}
          {t('Media storage is not configured or temporarily unavailable.')}
        </li>
      </ul>
    </section>
  )
}

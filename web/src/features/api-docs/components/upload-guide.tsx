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

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { mediaUploadExample } from '../lib/examples'
import { ApiCodeExample } from './api-code-example'

export function UploadGuide() {
  const { t } = useTranslation()

  return (
    <section id='upload' className='scroll-mt-24 space-y-4'>
      <h2 className='text-2xl font-semibold'>{t('1. Upload your media')}</h2>
      <p className='text-muted-foreground leading-7'>
        {t(
          'Send one local file as multipart/form-data. Let your HTTP client set the Content-Type boundary. Keep the returned id for the next request.'
        )}
      </p>
      <p className='text-muted-foreground text-sm leading-7'>
        {t(
          'Supported uploads: JPEG, PNG and WebP images; MP4, WebM and QuickTime videos. The filename extension must match the file content.'
        )}
      </p>
      <Tabs defaultValue='image'>
        <TabsList aria-label={t('Upload examples')}>
          <TabsTrigger value='image'>{t('Image upload')}</TabsTrigger>
          <TabsTrigger value='video'>{t('Video upload')}</TabsTrigger>
        </TabsList>
        <TabsContent value='image'>
          <ApiCodeExample
            title={t('Image upload')}
            code={mediaUploadExample('image')}
          />
        </TabsContent>
        <TabsContent value='video'>
          <ApiCodeExample
            title={t('Video upload')}
            code={mediaUploadExample('video')}
          />
        </TabsContent>
      </Tabs>
      <ApiCodeExample
        title={t('Upload response')}
        code={JSON.stringify(
          {
            id: 'media_IMAGE_ID',
            object: 'media.asset',
            type: 'image',
            mime_type: 'image/jpeg',
            size: 245760,
            status: 'ready',
            expires_at: 1805760000,
          },
          null,
          2
        )}
      />
      <p className='text-muted-foreground text-sm leading-7'>
        {t(
          'asset_id is a private reference belonging to your account, not a public URL. Other accounts cannot use it. Upload each image or video separately.'
        )}
      </p>
      <p className='text-muted-foreground text-sm leading-7'>
        {t(
          'To preview an upload, use GET /v1/media/assets/ASSET_ID/content with your API key. Replace ASSET_ID with the returned id.'
        )}
      </p>
      <p className='text-muted-foreground text-sm leading-7'>
        {t(
          'expires_at is a Unix timestamp in seconds. A value of 0 means the asset is in use by an active task and its retention period has not started.'
        )}
      </p>
    </section>
  )
}

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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'

import { ApiCodeExample } from './components/api-code-example'
import { ApiLimits } from './components/api-limits'
import { UploadGuide } from './components/upload-guide'
import { VideoGuide } from './components/video-guide'
import {
  API_BASE_URL,
  CHAT_EXAMPLE,
  VIDEO_DOWNLOAD_EXAMPLE,
  VIDEO_QUERY_EXAMPLE,
} from './lib/examples'

export function ApiDocsContent() {
  const { t } = useTranslation()

  return (
    <div className='mx-auto max-w-6xl min-w-0 px-1 py-8 sm:px-4'>
      <header className='mb-10 max-w-3xl space-y-5'>
        <p className='text-primary text-sm font-semibold'>Niu Dali API</p>
        <h1 className='text-4xl font-semibold tracking-tight sm:text-5xl'>
          {t('API documentation')}
        </h1>
        <p className='text-muted-foreground text-lg leading-8'>
          {t(
            'One API key. One media format. Use your models from Infinite Canvas or any application.'
          )}
        </p>
        <div className='flex flex-wrap gap-3'>
          <Button render={<Link to='/keys' />}>{t('API Keys')}</Button>
          <Button variant='outline' render={<Link to='/pricing' />}>
            {t('Model Square')}
          </Button>
        </div>
      </header>
      <div className='grid min-w-0 gap-10 lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-14'>
        <aside>
          <nav
            aria-label={t('On this page')}
            className='flex flex-wrap gap-x-5 gap-y-3 text-sm lg:sticky lg:top-24 lg:flex-col lg:gap-4'
          >
            <a href='#connect' className='hover:text-primary'>
              {t('Connect')}
            </a>
            <a href='#upload' className='hover:text-primary'>
              {t('Upload media')}
            </a>
            <a href='#generate' className='hover:text-primary'>
              {t('Create a video')}
            </a>
            <a href='#result' className='hover:text-primary'>
              {t('Query and download')}
            </a>
            <a href='#chat' className='hover:text-primary'>
              {t('Chat models')}
            </a>
            <a href='#limits' className='hover:text-primary'>
              {t('Limits and errors')}
            </a>
          </nav>
        </aside>
        <div className='min-w-0 space-y-12'>
          <section id='connect' className='scroll-mt-24 space-y-4'>
            <h2 className='text-2xl font-semibold'>{t('Connect')}</h2>
            <p className='text-muted-foreground leading-7'>
              {t(
                'Use the API key created in your Niu Dali account. Keep it on your server or in a trusted application. Never include it in a file URL or public source code.'
              )}
            </p>
            <p className='text-muted-foreground text-sm leading-7'>
              {t(
                'External applications must support uploading to /v1/media/assets before creating a task with media[].asset_id. Applications that only accept a one-step input_reference upload need an adapter.'
              )}
            </p>
            <ApiCodeExample
              title={t('Base URL and authorization')}
              code={`BASE_URL=${API_BASE_URL}\nNIU_DALI_API_KEY=YOUR_API_KEY\n\nAuthorization: Bearer $NIU_DALI_API_KEY`}
            />
            <p className='text-muted-foreground text-sm leading-7'>
              {t(
                'The examples use curl. Set NIU_DALI_API_KEY in your environment before running them. Use the exact model name and protocol shown in Model Square.'
              )}
            </p>
          </section>
          <UploadGuide />
          <VideoGuide />
          <section id='result' className='scroll-mt-24 space-y-4'>
            <h2 className='text-2xl font-semibold'>
              {t('3. Query and download')}
            </h2>
            <p className='text-muted-foreground leading-7'>
              {t(
                'Save the video id returned by creation. Poll the same task about every 15 seconds; a queued response is not a finished video.'
              )}
            </p>
            <ApiCodeExample
              title={t('Query a video')}
              code={VIDEO_QUERY_EXAMPLE}
            />
            <p className='text-sm leading-7'>
              <code>queued / in_progress</code> — {t('Keep waiting.')}{' '}
              <code>completed</code> — {t('Download the result.')}{' '}
              <code>failed</code> — {t('Read error.message.')}
            </p>
            <ApiCodeExample
              title={t('Download a completed video')}
              code={VIDEO_DOWNLOAD_EXAMPLE}
            />
            <p className='text-muted-foreground text-sm leading-7'>
              {t(
                'Replace VIDEO_ID with the returned id. Query timeouts do not require a new generation request. Download through this API using the same account key.'
              )}
            </p>
          </section>
          <section id='chat' className='scroll-mt-24 space-y-4'>
            <h2 className='text-2xl font-semibold'>{t('Chat models')}</h2>
            <p className='text-muted-foreground leading-7'>
              {t(
                'For models with Chat Completions support, send messages to /v1/chat/completions. Replace YOUR_MODEL_NAME with an available model. Other protocols depend on model support.'
              )}
            </p>
            <ApiCodeExample
              title={t('Chat Completions request')}
              code={CHAT_EXAMPLE}
            />
          </section>
          <ApiLimits />
        </div>
      </div>
    </div>
  )
}

export function ApiDocs() {
  return (
    <PublicLayout>
      <ApiDocsContent />
    </PublicLayout>
  )
}

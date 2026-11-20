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

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { videoRequestExample, type VideoExample } from '../lib/examples'
import { ApiCodeExample } from './api-code-example'

const EXAMPLES: VideoExample[] = ['text', 'image', 'video', 'frames']

export function VideoGuide() {
  const { t } = useTranslation()
  const titles = {
    text: t('Text to video'),
    image: t('Image reference'),
    video: t('Video reference'),
    frames: t('First and last frames'),
  }
  const fields = [
    [
      'model',
      t(
        'Exact model name from Model Square. The API key must have permission to use it.'
      ),
    ],
    ['prompt', t('Describe the result and how each reference should be used.')],
    [
      'duration',
      t(
        'Output duration in seconds, as an integer supported by the selected model.'
      ),
    ],
    [
      'resolution / ratio',
      t('Output resolution and aspect ratio supported by the selected model.'),
    ],
    ['media[].asset_id', t('The id returned by the media upload API.')],
    ['media[].role', t('How the model should use this image or video.')],
  ]

  return (
    <section id='generate' className='scroll-mt-24 space-y-4'>
      <h2 className='text-2xl font-semibold'>{t('2. Create a video')}</h2>
      <p className='text-muted-foreground leading-7'>
        {t(
          'Use the same JSON structure from Infinite Canvas or your own application. Replace every example asset ID with the id returned by your upload.'
        )}
      </p>
      <Tabs defaultValue='image'>
        <TabsList
          aria-label={t('Video generation examples')}
          className='h-auto! w-full flex-wrap justify-start gap-y-1 sm:w-fit'
        >
          {EXAMPLES.map((example) => (
            <TabsTrigger key={example} value={example}>
              {titles[example]}
            </TabsTrigger>
          ))}
        </TabsList>
        {EXAMPLES.map((example) => (
          <TabsContent key={example} value={example}>
            <ApiCodeExample
              title={titles[example]}
              code={videoRequestExample(example)}
            />
          </TabsContent>
        ))}
      </Tabs>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Field')}</TableHead>
            <TableHead>{t('Meaning')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {fields.map(([field, description]) => (
            <TableRow key={field}>
              <TableCell className='align-top'>
                <code>{field}</code>
              </TableCell>
              <TableCell className='min-w-48 whitespace-normal'>
                {description}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <div className='rounded-xl border p-4 text-sm leading-7'>
        <p>
          <code>reference_image</code> —{' '}
          {t('Reference appearance, style or composition.')}
        </p>
        <p>
          <code>reference_video</code> —{' '}
          {t('Reference motion and camera movement.')}
        </p>
        <p>
          <code>first_frame / last_frame</code> —{' '}
          {t(
            'Set the opening and closing images. A last frame requires a first frame.'
          )}
        </p>
        <p className='text-muted-foreground mt-2'>
          {t(
            'Frame mode cannot be mixed with reference images or videos. Available roles, media counts and output settings depend on the selected model.'
          )}
        </p>
      </div>
    </section>
  )
}

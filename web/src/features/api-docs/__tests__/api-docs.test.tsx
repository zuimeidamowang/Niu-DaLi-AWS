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
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { ApiDocsContent } from '..'

function renderDocs() {
  const rootRoute = createRootRoute({ component: ApiDocsContent })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(<RouterProvider router={router} />)
}

afterEach(async () => {
  await act(() => i18next.changeLanguage('en'))
})

describe('public model API documentation', () => {
  it('lets a guest switch from image to video upload without a provider account or file URL', async () => {
    const user = userEvent.setup()
    renderDocs()

    expect(
      await screen.findByRole('heading', {
        name: 'API documentation',
        level: 1,
      })
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Video billing' })).toBeVisible()
    expect(
      screen.getByText(/Seedance prices use standard billable seconds/)
    ).toHaveTextContent('including reference video processing')
    expect(
      screen.getByText(/Creation reserves an estimated charge/)
    ).toHaveTextContent('additional charge or refund')
    expect(
      screen.getByText(/For example, a 6-second output/)
    ).toHaveTextContent(
      'not a claim that the reference video lasts 4.0416 seconds'
    )
    expect(
      screen.getByLabelText('Image upload', { selector: 'pre' })
    ).toHaveTextContent("-F 'file=@reference.jpg'")
    await user.click(screen.getByRole('tab', { name: 'Video upload' }))
    const example = screen.getByLabelText('Video upload', { selector: 'pre' })
    expect(example).toHaveTextContent('/v1/media/assets')
    expect(example).toHaveTextContent("-F 'file=@motion.mp4'")
    expect(screen.getByRole('tab', { name: 'Video upload' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })

  it('switches generation examples by keyboard and copies exactly the visible platform media contract', async () => {
    const user = userEvent.setup()
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue()
    renderDocs()

    const imageTab = await screen.findByRole('tab', { name: 'Image reference' })
    await user.click(imageTab)
    await user.keyboard('{ArrowRight}')
    const videoTab = screen.getByRole('tab', { name: 'Video reference' })
    expect(videoTab).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(videoTab).toHaveAttribute('aria-selected', 'true')
    const example = screen.getByLabelText('Video reference', {
      selector: 'pre',
    })
    const request = JSON.parse(
      example.textContent?.match(/-d '([\s\S]+)'$/)?.[1] ?? ''
    )
    expect(request.media).toEqual([
      { asset_id: 'media_VIDEO_ID', role: 'reference_video' },
    ])
    expect(request).not.toHaveProperty('content')
    await user.click(
      screen.getByRole('button', { name: 'Copy Video reference' })
    )
    expect(writeText).toHaveBeenCalledWith(example.textContent)
    expect(
      await screen.findByRole('button', { name: 'Copied' })
    ).toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: 'First and last frames' }))
    const frames = screen.getByLabelText('First and last frames', {
      selector: 'pre',
    })
    const frameRequest = JSON.parse(
      frames.textContent?.match(/-d '([\s\S]+)'$/)?.[1] ?? ''
    )
    expect(frameRequest.media).toEqual([
      { asset_id: 'media_FIRST_IMAGE_ID', role: 'first_frame' },
      { asset_id: 'media_LAST_IMAGE_ID', role: 'last_frame' },
    ])
  })

  it('keeps examples scrollable on small screens and updates guide text when the language changes', async () => {
    i18next.addResourceBundle('zh', 'translation', zh.translation, true, true)
    renderDocs()

    const heading = await screen.findByRole('heading', {
      name: 'API documentation',
      level: 1,
    })
    const sidebar = screen.getByRole('navigation', { name: 'On this page' })
    expect(sidebar).toHaveClass('flex-wrap')
    const example = screen.getByLabelText('Image reference', {
      selector: 'pre',
    })
    expect(example).toHaveClass('max-w-full', 'overflow-x-auto')
    expect(example).toHaveAttribute('tabindex', '0')

    await act(() => i18next.changeLanguage('zh'))
    expect(heading).toHaveTextContent('API 文档')
    expect(screen.getByRole('tab', { name: '视频参考' })).toBeInTheDocument()
  })
})

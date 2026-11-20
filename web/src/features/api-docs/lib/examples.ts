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
export type VideoExample = 'text' | 'image' | 'video' | 'frames'

export const API_BASE_URL = 'https://www.daliniuai.com'

export function mediaUploadExample(type: 'image' | 'video'): string {
  const filename = type === 'image' ? 'reference.jpg' : 'motion.mp4'
  return `curl --fail-with-body '${API_BASE_URL}/v1/media/assets' \\
  -H "Authorization: Bearer $NIU_DALI_API_KEY" \\
  -F 'file=@${filename}'`
}

export function videoRequestExample(example: VideoExample): string {
  const media: { asset_id: string; role: string }[] = []
  if (example === 'image') {
    media.push({ asset_id: 'media_IMAGE_ID', role: 'reference_image' })
  }
  if (example === 'video') {
    media.push({ asset_id: 'media_VIDEO_ID', role: 'reference_video' })
  }
  if (example === 'frames') {
    media.push(
      { asset_id: 'media_FIRST_IMAGE_ID', role: 'first_frame' },
      { asset_id: 'media_LAST_IMAGE_ID', role: 'last_frame' }
    )
  }
  const body = {
    model: 'doubao-seedance-2-5',
    prompt: 'A cinematic shot with gentle camera movement',
    duration: 6,
    resolution: '720p',
    ratio: '16:9',
    ...(media.length ? { media } : {}),
  }
  return `curl --fail-with-body '${API_BASE_URL}/v1/videos' \\
  -H "Authorization: Bearer $NIU_DALI_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '${JSON.stringify(body, null, 2)}'`
}

export const VIDEO_QUERY_EXAMPLE = `curl --fail-with-body '${API_BASE_URL}/v1/videos/VIDEO_ID' \\
  -H "Authorization: Bearer $NIU_DALI_API_KEY"`

export const VIDEO_DOWNLOAD_EXAMPLE = `curl --fail-with-body '${API_BASE_URL}/v1/videos/VIDEO_ID/content' \\
  -H "Authorization: Bearer $NIU_DALI_API_KEY" \\
  -o output.mp4`

export const CHAT_EXAMPLE = `curl --fail-with-body '${API_BASE_URL}/v1/chat/completions' \\
  -H "Authorization: Bearer $NIU_DALI_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "model": "YOUR_MODEL_NAME",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'`

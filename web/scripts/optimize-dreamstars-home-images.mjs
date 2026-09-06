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
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import sharp from 'sharp'

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const assetsDirectory = path.resolve(
  scriptDirectory,
  '../src/features/home/assets'
)
const qualityArgument = process.argv.find((argument) =>
  argument.startsWith('--quality=')
)
const quality = Number(qualityArgument?.split('=')[1] ?? '90')

if (!Number.isInteger(quality) || quality < 1 || quality > 100) {
  throw new Error('The --quality value must be an integer between 1 and 100.')
}

const illustrations = [
  {
    source: 'dreamstars-scene-research-gpt.png',
    outputBaseName: 'dreamstars-scene-research-gpt',
    width: 1280,
    height: 720,
  },
  {
    source: 'dreamstars-scene-data-deepseek.png',
    outputBaseName: 'dreamstars-scene-data-deepseek',
    width: 1280,
    height: 720,
  },
  {
    source: 'dreamstars-scene-presentation-gemini.png',
    outputBaseName: 'dreamstars-scene-presentation-gemini',
    width: 1280,
    height: 720,
  },
  {
    source: 'dreamstars-scene-learning-claude.png',
    outputBaseName: 'dreamstars-scene-learning-claude',
    width: 1280,
    height: 853,
  },
]

const renditionWidths = [640, 960, 1280]

for (const illustration of illustrations) {
  const sourcePath = path.join(assetsDirectory, 'source', illustration.source)
  for (const width of renditionWidths) {
    const height = Math.round(
      (illustration.height * width) / illustration.width
    )
    const suffix = width === illustration.width ? '' : `-${width}w`
    const outputName = `${illustration.outputBaseName}${suffix}.webp`
    const outputPath = path.join(assetsDirectory, outputName)

    await sharp(sourcePath)
      .resize({
        width,
        height,
        fit: 'contain',
        background: { r: 0, g: 0, b: 0, alpha: 0 },
      })
      .webp({
        quality,
        alphaQuality: 100,
        effort: 6,
        smartSubsample: true,
      })
      .toFile(outputPath)

    console.log(`${outputName}: ${width}×${height}`)
  }
}

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
import Claude from '@lobehub/icons/es/Claude'
import DeepSeek from '@lobehub/icons/es/DeepSeek'
import Gemini from '@lobehub/icons/es/Gemini'
import Grok from '@lobehub/icons/es/Grok'
import Kimi from '@lobehub/icons/es/Kimi'
import Minimax from '@lobehub/icons/es/Minimax'
import OpenAI from '@lobehub/icons/es/OpenAI'
import Qwen from '@lobehub/icons/es/Qwen'
import Zhipu from '@lobehub/icons/es/Zhipu'

import type { DREAMSTARS_MODEL_BRANDS } from '../constants'

type ModelBrandIconName = (typeof DREAMSTARS_MODEL_BRANDS)[number]['iconName']

interface ModelBrandIconProps {
  iconName: ModelBrandIconName
  size: number
}

export function ModelBrandIcon(props: ModelBrandIconProps) {
  switch (props.iconName) {
    case 'OpenAI':
      return <OpenAI size={props.size} />
    case 'Gemini.Color':
      return <Gemini.Color size={props.size} />
    case 'Claude.Color':
      return <Claude.Color size={props.size} />
    case 'Grok':
      return <Grok size={props.size} />
    case 'DeepSeek.Color':
      return <DeepSeek.Color size={props.size} />
    case 'Kimi.Color':
      return <Kimi.Color size={props.size} />
    case 'Zhipu.Color':
      return <Zhipu.Color size={props.size} />
    case 'Qwen.Color':
      return <Qwen.Color size={props.size} />
    case 'Minimax.Color':
      return <Minimax.Color size={props.size} />
  }
}

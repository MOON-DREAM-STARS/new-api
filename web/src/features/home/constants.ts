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
/**
 * Home page constants
 * All hardcoded data for home page sections
 */
import type { TFunction } from 'i18next'

export const DREAMSTARS_SCENES = [
  {
    id: 'research',
    title: 'Research and Literature',
    description: 'Use GPT to read academic literature faster.',
  },
  {
    id: 'data',
    title: 'Data and Code',
    description: 'Use DeepSeek to work through data and code more efficiently.',
  },
  {
    id: 'presentation',
    title: 'Presentation and Communication',
    description:
      'Use Gemini to organize key points and communicate your findings.',
  },
  {
    id: 'learning',
    title: 'Learning and Writing',
    description:
      'Use Claude to understand concepts and support your learning and writing.',
  },
] as const

export const DREAMSTARS_MODEL_BRANDS = [
  { name: 'OpenAI · GPT', iconName: 'OpenAI', tier: 'primary' },
  { name: 'Gemini', iconName: 'Gemini.Color', tier: 'primary' },
  { name: 'Claude', iconName: 'Claude.Color', tier: 'primary' },
  { name: 'Grok', iconName: 'Grok', tier: 'primary' },
  { name: 'DeepSeek', iconName: 'DeepSeek.Color', tier: 'primary' },
  { name: 'Kimi', iconName: 'Kimi.Color', tier: 'primary' },
  { name: 'Zhipu GLM', iconName: 'Zhipu.Color', tier: 'primary' },
  { name: 'Tongyi Qwen', iconName: 'Qwen.Color', tier: 'primary' },
  { name: 'MiniMax', iconName: 'Minimax.Color', tier: 'primary' },
] as const

export const DREAMSTARS_STEPS = [
  {
    number: '01',
    title: 'Sign in to the platform',
    description:
      'Use the account assigned by the administrator to enter the platform.',
    icon: 'user',
  },
  {
    number: '02',
    title: 'Choose a model',
    description: 'Choose a suitable model by task, capability, and CNY price.',
    icon: 'sparkles',
  },
  {
    number: '03',
    title: 'Start using AI',
    description:
      'Create an API Key and connect a familiar client or compatible endpoint.',
    icon: 'arrow',
  },
] as const

// Layout - Main base classes
export const MAIN_BASE_CLASSES = 'bg-background text-foreground w-full'

// Hero section - AI Applications (Left side)
export const AI_APPLICATIONS = [
  'LobeHub.Color',
  'Dify.Color',
  'OpenWebUI',
  'Cline',
] as const

// Hero section - AI Models (Right side)
export const AI_MODELS = [
  'Qwen.Color',
  'DeepSeek.Color',
  'Doubao.Color',
  'OpenAI',
  'Claude.Color',
  'Gemini.Color',
] as const

// Hero section - Gateway Features
export const GATEWAY_FEATURES = [
  'Cost Tracking',
  'Model Access',
  'Guardrails',
  'Observability',
  'Budgets',
  'Load Balancing',
  'Rate Limiting',
  'Token Mgmt',
  'Prompt Caching',
  'Pass-Through',
] as const

// Stats section - Default statistics
export const DEFAULT_STATS = [
  {
    value: '50',
    suffix: '+',
    description: 'upstream services integrated',
  },
  {
    value: '100',
    suffix: '+',
    description: 'model billing support',
  },
  {
    value: '50',
    suffix: '+',
    description: 'compatible API routes',
  },
  {
    value: '10',
    suffix: '+',
    description: 'scheduling controls',
  },
] as const

// Features section - Default features
export const DEFAULT_FEATURES = [
  {
    title: 'Lightning Fast',
    description:
      'Optimized network architecture ensures millisecond response times',
    iconName: 'Zap',
  },
  {
    title: 'Secure & Reliable',
    description:
      'Enterprise-grade security with comprehensive permission management',
    iconName: 'Shield',
  },
  {
    title: 'Global Coverage',
    description: 'Multi-region deployment for stable global access',
    iconName: 'Globe',
  },
  {
    title: 'Developer Friendly',
    description: 'Compatible API routes for common AI application workflows',
    iconName: 'Code',
  },
  {
    title: 'High Performance',
    description: 'Support for high concurrency with automatic load balancing',
    iconName: 'Gauge',
  },
  {
    title: 'Transparent Billing',
    description: 'Pay-as-you-go with real-time usage monitoring',
    iconName: 'DollarSign',
  },
  {
    title: 'Team Collaboration',
    description: 'Multi-user management with flexible permission allocation',
    iconName: 'Users',
  },
  {
    title: 'Open Source',
    description: 'Community driven, self-hosted, and extensible',
    iconName: 'HeartHandshake',
  },
] as const

export function getGatewayFeatures(t: TFunction) {
  return GATEWAY_FEATURES.map((feature) => t(feature))
}

export function getDefaultStats(t: TFunction) {
  return DEFAULT_STATS.map((stat) => ({
    ...stat,
    description: stat.description ? t(stat.description) : undefined,
  }))
}

export function getDefaultFeatures(t: TFunction) {
  return DEFAULT_FEATURES.map((feature) => ({
    ...feature,
    title: t(feature.title),
    description: t(feature.description),
  }))
}

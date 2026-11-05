import { api } from './client'
import type { Accent, Density, ThemeStyle } from '../theme/themes'
import type { ThemeMode } from '../theme'

export interface AppearanceInput {
  style: ThemeStyle | null
  mode: ThemeMode
  accent: Accent
  density: Density
}

export interface AppearanceResponse extends AppearanceInput {
  profile: string
  default_style: ThemeStyle
  effective_style: ThemeStyle
}

export const appearanceApi = {
  get: () => api.get<AppearanceResponse>('/api/v1/auth/appearance'),
  save: (input: AppearanceInput) => api.put<AppearanceResponse>('/api/v1/auth/appearance', input),
}

import { createContext, useContext } from 'react'

export type ThemePreference = 'light' | 'dark' | null
export type ResolvedTheme = 'light' | 'dark'

export type ThemeModeContextValue = {
  preference: ThemePreference
  resolvedTheme: ResolvedTheme
  setThemePreference: (value: ThemePreference) => void
  toggleTheme: () => void
}

export const ThemeModeContext = createContext<ThemeModeContextValue | null>(null)

export function useThemeMode() {
  const context = useContext(ThemeModeContext)
  if (!context) {
    throw new Error('useThemeMode must be used within ThemeProvider')
  }
  return context
}

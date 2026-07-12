import {
  ConfigProvider,
  theme as antdTheme,
  type ThemeConfig,
} from 'antd'
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'

import {
  ThemeModeContext,
  type ResolvedTheme,
  type ThemeModeContextValue,
  type ThemePreference,
} from './themeMode'

const STORAGE_KEY = 'chatapi.theme.preference'
const MEDIA_QUERY = '(prefers-color-scheme: dark)'

function getStoredPreference(): ThemePreference {
  if (typeof window === 'undefined') return null
  const value = window.localStorage.getItem(STORAGE_KEY)
  return value === 'light' || value === 'dark' ? value : null
}

function getSystemTheme(): ResolvedTheme {
  if (typeof window === 'undefined') return 'dark'
  return window.matchMedia(MEDIA_QUERY).matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<ThemePreference>(getStoredPreference)
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(getSystemTheme)
  const previousResolvedThemeRef = useRef<ResolvedTheme | null>(null)
  const resolvedTheme = preference ?? systemTheme

  useEffect(() => {
    if (typeof window === 'undefined') return
    const mediaQuery = window.matchMedia(MEDIA_QUERY)

    function handleChange(event: MediaQueryListEvent) {
      setSystemTheme(event.matches ? 'dark' : 'light')
    }

    mediaQuery.addEventListener('change', handleChange)
    return () => {
      mediaQuery.removeEventListener('change', handleChange)
    }
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (preference) {
      window.localStorage.setItem(STORAGE_KEY, preference)
    } else {
      window.localStorage.removeItem(STORAGE_KEY)
    }
  }, [preference])

  useEffect(() => {
    const root = document.documentElement
    root.dataset.theme = resolvedTheme
    root.dataset.themePreference = preference ?? 'system'
    root.style.colorScheme = resolvedTheme

    const previousTheme = previousResolvedThemeRef.current
    previousResolvedThemeRef.current = resolvedTheme
    if (!previousTheme || previousTheme === resolvedTheme) return

    document.body.classList.add('theme-animating')
    const timer = window.setTimeout(() => {
      document.body.classList.remove('theme-animating')
    }, 520)

    return () => {
      window.clearTimeout(timer)
      document.body.classList.remove('theme-animating')
    }
  }, [preference, resolvedTheme])

  const config = useMemo<ThemeConfig>(() => {
    const isDark = resolvedTheme === 'dark'

    return {
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: {
        borderRadius: 14,
        colorPrimary: isDark ? '#6bc1ff' : '#1677ff',
        colorInfo: isDark ? '#6bc1ff' : '#1677ff',
        colorSuccess: isDark ? '#3dd9a1' : '#16a34a',
        colorWarning: isDark ? '#ffb55e' : '#d97706',
        colorError: isDark ? '#ff7875' : '#dc2626',
        colorTextBase: isDark ? '#eef4ff' : '#0f172a',
        colorBgBase: isDark ? '#08111f' : '#f3f7fc',
        boxShadowSecondary: isDark
          ? '0 18px 48px rgba(0, 0, 0, 0.32)'
          : '0 18px 48px rgba(15, 23, 42, 0.12)',
      },
      components: {
        Layout: {
          bodyBg: 'transparent',
          siderBg: 'transparent',
          headerBg: 'transparent',
        },
        Drawer: {
          colorBgElevated: isDark ? '#0d1728' : '#ffffff',
        },
        Modal: {
          contentBg: isDark ? '#0f1728' : '#ffffff',
          headerBg: 'transparent',
        },
        Card: {
          colorBgContainer: isDark ? '#101c31' : '#ffffff',
        },
        Input: {
          activeBorderColor: isDark ? '#6bc1ff' : '#1677ff',
        },
        Button: {
          primaryShadow: 'none',
        },
      },
    }
  }, [resolvedTheme])

  const value = useMemo<ThemeModeContextValue>(
    () => ({
      preference,
      resolvedTheme,
      setThemePreference: setPreference,
      toggleTheme: () => {
        setPreference((current) => {
          const activeTheme = current ?? resolvedTheme
          return activeTheme === 'dark' ? 'light' : 'dark'
        })
      },
    }),
    [preference, resolvedTheme],
  )

  return (
    <ThemeModeContext.Provider value={value}>
      <ConfigProvider theme={config}>{children}</ConfigProvider>
    </ThemeModeContext.Provider>
  )
}

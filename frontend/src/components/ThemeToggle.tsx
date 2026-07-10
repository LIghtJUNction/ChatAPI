import type { ButtonHTMLAttributes } from 'react'

import { useThemeMode } from '../theme/themeMode'

type ThemeToggleProps = {
  className?: string
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'>

export function ThemeToggle({ className = '', type = 'button', ...props }: ThemeToggleProps) {
  const { preference, resolvedTheme, toggleTheme } = useThemeMode()
  const isDark = resolvedTheme === 'dark'
  const title = `切换到${isDark ? '浅色' : '深色'}模式${preference ? '（已保存）' : '（当前跟随系统）'}`

  return (
    <button
      {...props}
      type={type}
      className={`theme-toggle ${className}`.trim()}
      aria-label={title}
      title={title}
      onClick={() => toggleTheme()}
    >
      <span className={`theme-toggle-glyph ${isDark ? 'is-dark' : 'is-light'}`} aria-hidden="true">
        <span className="theme-toggle-sun">
          <svg viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="4.25" />
            <path d="M12 2.75v2.5" />
            <path d="M12 18.75v2.5" />
            <path d="M21.25 12h-2.5" />
            <path d="M5.25 12h-2.5" />
            <path d="M18.54 5.46l-1.77 1.77" />
            <path d="M7.23 16.77l-1.77 1.77" />
            <path d="M18.54 18.54l-1.77-1.77" />
            <path d="M7.23 7.23L5.46 5.46" />
          </svg>
        </span>
        <span className="theme-toggle-moon">
          <svg viewBox="0 0 24 24" fill="none">
            <path d="M20.2 14.1A7.95 7.95 0 0 1 9.9 3.8a8.7 8.7 0 1 0 10.3 10.3Z" />
          </svg>
        </span>
      </span>
    </button>
  )
}

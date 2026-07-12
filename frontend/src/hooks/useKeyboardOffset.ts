import { useEffect, useState } from 'react'

function getKeyboardOffset() {
  if (typeof window === 'undefined' || !window.visualViewport) return 0
  const viewport = window.visualViewport
  const rawOffset = Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop)
  return rawOffset > 80 ? Math.round(rawOffset) : 0
}

export function useKeyboardOffset() {
  const [keyboardOffset, setKeyboardOffset] = useState(getKeyboardOffset)

  useEffect(() => {
    if (typeof window === 'undefined' || !window.visualViewport) return

    const viewport = window.visualViewport
    const updateKeyboardOffset = () => {
      setKeyboardOffset(getKeyboardOffset())
    }

    viewport.addEventListener('resize', updateKeyboardOffset)
    viewport.addEventListener('scroll', updateKeyboardOffset)

    return () => {
      viewport.removeEventListener('resize', updateKeyboardOffset)
      viewport.removeEventListener('scroll', updateKeyboardOffset)
    }
  }, [])

  return keyboardOffset
}

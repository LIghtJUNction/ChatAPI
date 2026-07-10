import { App } from 'antd'
import { useEffect } from 'react'

import { bindMessageApi } from './antdMessage'

export function AntdAppBridge() {
  const { message } = App.useApp()

  useEffect(() => {
    return bindMessageApi(message)
  }, [message])

  return null
}

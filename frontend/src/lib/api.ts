type RequestError = Error & {
  responseBody?: unknown
}

function resolveRequestUrl(url: string): string {
  if (/^(?:[a-z]+:)?\/\//i.test(url)) {
    return url
  }
  const baseUrl = import.meta.env.BASE_URL || '/'
  const normalizedBase = baseUrl.endsWith('/') ? baseUrl : `${baseUrl}/`
  const normalizedPath = url.replace(/^\/+/, '')
  return `${normalizedBase}${normalizedPath}`
}

export async function requestJson<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(resolveRequestUrl(url), {
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        ...(init?.headers ?? {}),
      },
      ...init,
    })
  } catch {
    throw new Error('无法连接到后端')
  }

  const body = await response.json().catch(() => null)
  if (!response.ok) {
    const fallback =
      body?.error?.message ?? body?.error ?? body?.message ?? '请求失败'
    const errorMessage = typeof fallback === 'string' ? fallback : '请求失败'
    const error: RequestError = new Error(errorMessage)
    // Attach additional error details from the response body
    if (body && typeof body === 'object') {
      error.responseBody = body
    }
    throw error
  }
  return body as T
}

export async function requestFormJson<T>(url: string, form: FormData): Promise<T> {
	let response: Response
	try {
		response = await fetch(resolveRequestUrl(url), {
			method: 'POST',
			credentials: 'include',
			body: form,
		})
	} catch {
		throw new Error('无法连接到后端')
	}
	const body = await response.json().catch(() => null)
	if (!response.ok) {
		const fallback = body?.error?.message ?? body?.error ?? body?.message ?? '请求失败'
		throw new Error(typeof fallback === 'string' ? fallback : '请求失败')
	}
	return body as T
}

export function resolveWebSocketUrl(url: string): string {
  const resolved = resolveRequestUrl(url)
  const target = new URL(resolved, window.location.origin)
  target.protocol = target.protocol === 'https:' ? 'wss:' : 'ws:'
  return target.toString()
}

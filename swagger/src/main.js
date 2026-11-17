import swagger from 'swagger-ui-dist'
import 'swagger-ui-dist/swagger-ui.css'

const BASE_PATH = import.meta.env.BASE_URL ?? '/'
const API_URL = process.env.NODE_ENV === 'production' ? BASE_PATH.replace(/\/+$/, '') : 'http://localhost:3000/api/v1'
const APP_PATH = BASE_PATH.replace(/api\/v1\/?$/, '')

function authStatus(config) {
  if (!config.authEnabled) {
    return 'Authentication disabled - write access available.'
  }
  if (!config.authUser) {
    return 'Not signed in - read-only access.'
  }
  const identity = config.authEmail || config.authUser
  return `Signed in as ${identity} - ${config.authWriteAccess ? 'write access' : 'read-only access'}.`
}

function createCopyButton(value, input) {
  const button = document.createElement('button')
  button.type = 'button'
  button.className = 'copy-api-key'
  button.title = 'Copy API key'
  button.setAttribute('aria-label', 'Copy API key')
  button.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'
  button.addEventListener('click', async () => {
    input.select()
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      document.execCommand('copy')
    }
    button.classList.add('copied')
    button.title = 'Copied'
    button.setAttribute('aria-label', 'Copied')
    setTimeout(() => {
      button.classList.remove('copied')
      button.title = 'Copy API key'
      button.setAttribute('aria-label', 'Copy API key')
    }, 1500)
  })
  return button
}

function addAPIKeyControls(status, config) {
  if (!config.apiKeyEnabled || !config.authUser || !config.authWriteAccess) {
    return
  }
  const button = document.createElement('button')
  button.type = 'button'
  button.textContent = 'Generate API key'
  button.addEventListener('click', async () => {
    button.disabled = true
    try {
      const response = await fetch(`${API_URL}/api-key`, { method: 'POST', credentials: 'include' })
      if (!response.ok) {
        throw new Error(`api key request failed with ${response.status}`)
      }
      const data = await response.json()
      document.getElementById('api-key-result')?.remove()
      const result = document.createElement('div')
      result.id = 'api-key-result'
      const row = document.createElement('div')
      row.className = 'api-key-row'
      const label = document.createElement('span')
      label.textContent = 'API key (shown once):'
      const input = document.createElement('input')
      input.readOnly = true
      input.setAttribute('readonly', '')
      input.value = data.apiKey
      input.addEventListener('click', () => input.select())
      row.append(label, input, createCopyButton(data.apiKey, input))
      const meta = document.createElement('div')
      meta.className = 'api-key-meta'
      meta.textContent = `Expires ${new Date(data.expiresAt).toLocaleString()}. The expiry is extended when you log in to the frontend.`
      const namespace = (config.rdfNamespace || 'http://example.org/').replace(/\/+$/, '')
      const resourceId = `${namespace}/example-resource`
      const resourceUrl = new URL(`${API_URL}/resource/${encodeURIComponent(resourceId)}`, window.location.origin).href
      const curl = document.createElement('pre')
      curl.className = 'api-key-curl'
      curl.textContent = [
        'curl -X PUT \\',
        `  -H "X-API-Key: ${data.apiKey}" \\`,
        '  --data-urlencode "ttl@resource.ttl" \\',
        `  '${resourceUrl}'`
      ].join('\n')
      const example = document.createElement('fieldset')
      example.className = 'api-key-example'
      const legend = document.createElement('legend')
      legend.textContent = 'Example usage'
      example.append(legend, curl)
      result.append(row, meta, example)
      status.append(result)
    } catch {
      document.getElementById('api-key-result')?.remove()
      const result = document.createElement('div')
      result.id = 'api-key-result'
      result.textContent = 'Could not generate API key.'
      status.append(result)
    } finally {
      button.disabled = false
    }
  })
  status.append(' ', button)
}

async function showAuthStatus() {
  const status = document.getElementById('auth-status')
  try {
    const response = await fetch(`${API_URL}/config`, { credentials: 'include' })
    if (!response.ok) {
      throw new Error(`config request failed with ${response.status}`)
    }
    const config = await response.json()
    status.textContent = authStatus(config)
    addAPIKeyControls(status, config)
    if (config.authEnabled && !config.authUser) {
      const link = document.createElement('a')
      link.href = `${APP_PATH}oauth2/sign_in?rd=${encodeURIComponent(window.location.pathname)}`
      link.textContent = 'Sign in'
      status.append(' ', link)
    }
  } catch {
    status.textContent = 'Authentication status unavailable.'
  }
}

showAuthStatus()

swagger.SwaggerUIBundle({
  dom_id: '#app',
  url: `${API_URL}/openapi.json`
})

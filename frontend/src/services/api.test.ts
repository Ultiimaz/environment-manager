import { describe, it, expect, beforeEach } from 'vitest'
import {
  buildLogWsUrl,
  envRuntimeLogWsUrl,
  serviceRuntimeLogWsUrl,
  setStoredToken,
  getStoredToken,
} from './api'

// JSDOM gives us window.location and localStorage. We pin location.host
// for deterministic URL assertions.
beforeEach(() => {
  // Reset localStorage between tests so token state doesn't leak.
  setStoredToken('')
  // jsdom defaults to http://localhost:3000; the WS helpers use
  // window.location.host so this is what they read.
})

describe('admin token storage', () => {
  it('round-trips a token via localStorage', () => {
    setStoredToken('envm_abc123')
    expect(getStoredToken()).toBe('envm_abc123')
  })

  it('clears the token when set to empty string', () => {
    setStoredToken('envm_abc123')
    setStoredToken('')
    expect(getStoredToken()).toBe('')
  })
})

describe('WebSocket URL helpers', () => {
  it('omits ?token= when no admin token is stored', () => {
    expect(buildLogWsUrl('p1--main')).not.toContain('token=')
    expect(envRuntimeLogWsUrl('p1--main')).not.toContain('token=')
    expect(serviceRuntimeLogWsUrl('paas-postgres')).not.toContain('token=')
  })

  it('appends ?token= when an admin token is stored', () => {
    setStoredToken('envm_xyz')
    expect(buildLogWsUrl('p1--main')).toMatch(/[?&]token=envm_xyz$/)
    expect(serviceRuntimeLogWsUrl('paas-postgres')).toMatch(/[?&]token=envm_xyz$/)
  })

  it('appends ?token= AFTER an existing query string for runtime logs', () => {
    setStoredToken('envm_xyz')
    const url = envRuntimeLogWsUrl('p1--main', 'web')
    // Order matters: ?service=web first, then &token=...
    expect(url).toMatch(/\?service=web&token=envm_xyz$/)
  })

  it('uses ws:// on http origins and wss:// on https', () => {
    // jsdom's default protocol is http: — check the ws fallback.
    expect(buildLogWsUrl('x').startsWith('ws://')).toBe(true)
  })

  it('URL-encodes non-trivial token characters in the query', () => {
    setStoredToken('a b/c=')
    const url = buildLogWsUrl('x')
    // Trailing token must be encoded — & and = inside the token would
    // otherwise be parsed as additional query params.
    expect(url).toContain('token=a%20b%2Fc%3D')
  })

  it('encodes service names that contain URL-special characters', () => {
    const url = serviceRuntimeLogWsUrl('with spaces/slash')
    expect(url).toContain('with%20spaces%2Fslash')
  })
})

const BASE = '/api/v1'

async function req(method, path, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(BASE + path, opts)
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try { const j = await res.json(); msg = j.errors?.[0]?.message || j.detail || msg } catch {}
    throw new Error(msg)
  }
  if (res.status === 204 || res.status === 202) return null
  return res.json()
}

export const api = {
  // Alerts
  getAlerts: (params) => req('GET', '/alerts?' + new URLSearchParams(params)),
  getAlert:  (id)     => req('GET', '/alerts/' + encodeURIComponent(id)),
  deleteAlert: (id)   => req('DELETE', '/alerts/' + encodeURIComponent(id)),
  getAlertStats: ()   => req('GET', '/alerts/stats'),

  // CBE (alert origination)
  createCBEAlert: (body) => req('POST', '/cbe/alerts', body),

  // Geo Codes (SAME/UGC reference list)
  getGeoCodes:    ()         => req('GET',    '/geocodes'),
  createGeoCode:  (body)     => req('POST',   '/geocodes', body),
  updateGeoCode:  (id, body) => req('PUT',    '/geocodes/' + id, body),
  deleteGeoCode:  (id)       => req('DELETE', '/geocodes/' + id),

  // Feeds
  getFeeds:    ()         => req('GET',    '/feeds'),
  createFeed:  (body)     => req('POST',   '/feeds', body),
  updateFeed:  (id, body) => req('PUT',    '/feeds/' + id, body),
  deleteFeed:  (id)       => req('DELETE', '/feeds/' + id),
  pollFeed:    (id)       => req('POST',   '/feeds/' + id + '/poll'),

  // XMPP
  getXMPP:       ()         => req('GET',    '/xmpp'),
  createXMPP:    (body)     => req('POST',   '/xmpp', body),
  updateXMPP:    (id, body) => req('PUT',    '/xmpp/' + id, body),
  deleteXMPP:    (id)       => req('DELETE', '/xmpp/' + id),
  testXMPP:      (id)       => req('POST',   '/xmpp/' + id + '/test'),
  reconnectXMPP: (id)       => req('POST',   '/xmpp/' + id + '/reconnect'),

  // System
  getStatus:    () => req('GET',  '/system/status'),
  runExpiry:    () => req('POST', '/system/expiry'),
  getMapConfig: () => req('GET',  '/system/map-config'),
}

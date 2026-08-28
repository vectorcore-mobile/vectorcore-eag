import React, { useState, useCallback, useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api.js'
import { SeverityBadge, SeverityDot, fmtTime, fmtRel } from './shared.jsx'
import AlertMap from './AlertMap.jsx'

const SEVERITIES = ['Extreme','Severe','Moderate','Minor','Unknown']
const URGENCIES  = ['Immediate','Expected','Future','Past','Unknown']
const STATUSES   = ['Actual','Exercise','Test','Draft']
const MSG_TYPES  = ['Alert','Update','Cancel']

export default function Alerts() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [alerts, setAlerts]       = useState([])
  const [total, setTotal]         = useState(0)
  const [loading, setLoading]     = useState(false)
  const [error, setError]         = useState(null)
  const [lastUpdate, setLastUpdate] = useState(null)
  const [selected, setSelected]   = useState(null)
  const [checked, setChecked]     = useState(new Set())
  const [showExpired, setShowExpired] = useState(false)

  // Filters
  const [q, setQ]           = useState(searchParams.get('q') || '')
  const [severity, setSev]  = useState(searchParams.get('severity') || '')
  const [urgency, setUrg]   = useState('')
  const [status, setSt]     = useState('')
  const [msgType, setMsgType] = useState('')
  const [feedSrc, setFeedSrc] = useState(searchParams.get('feed_source') || '')
  const [fromDate, setFrom] = useState('')
  const [toDate, setTo]     = useState('')

  const [page, setPage]   = useState(1)
  const [limit]           = useState(50)
  const [sort, setSort]   = useState('sent')
  const [order, setOrder] = useState('desc')

  const [feedNames, setFeedNames] = useState([])

  useEffect(() => {
    api.getFeeds().then(r => setFeedNames((r || []).map(f => f.name))).catch(() => {})
  }, [])

  const buildParams = useCallback(() => {
    const p = { page, limit, sort, order }
    if (q)          p.q = q
    if (severity)   p.severity = severity
    if (urgency)    p.urgency = urgency
    if (status)     p.status = status
    if (msgType)    p.msg_type = msgType
    if (feedSrc)    p.feed_source = feedSrc
    if (fromDate)   p.from = fromDate
    if (toDate)     p.to = toDate
    if (showExpired) p.include_expired = true
    return p
  }, [q, severity, urgency, status, msgType, feedSrc, fromDate, toDate, showExpired, page, limit, sort, order])

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.getAlerts(buildParams())
      setAlerts(res?.alerts || [])
      setTotal(res?.total || 0)
      setLastUpdate(new Date())
      setError(null)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [buildParams])

  // Fire on filter / page / sort changes — debounced 300ms so typing in the
  // search box doesn't hammer the API on every keystroke.
  const debounceRef = useRef(null)
  useEffect(() => {
    clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => refresh(), 300)
    return () => clearTimeout(debounceRef.current)
  }, [q, severity, urgency, status, msgType, feedSrc, fromDate, toDate, showExpired, page, sort, order])

  // 30-second background auto-refresh (paused when tab hidden)
  useEffect(() => {
    const id = setInterval(() => {
      if (document.visibilityState !== 'hidden') refresh()
    }, 30000)
    return () => clearInterval(id)
  }, [refresh])

  const totalPages = Math.max(1, Math.ceil(total / limit))

  const handleSort = (field) => {
    if (sort === field) setOrder(o => o === 'asc' ? 'desc' : 'asc')
    else { setSort(field); setOrder('desc') }
    setPage(1)
  }

  const SortTh = ({ field, children }) => (
    <th style={{ cursor:'pointer', userSelect:'none' }} onClick={() => handleSort(field)}>
      {children} {sort === field ? (order === 'desc' ? '↓' : '↑') : ''}
    </th>
  )

  const toggleCheck = (id) => {
    setChecked(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s })
  }
  const toggleAll = () => {
    if (checked.size === alerts.length) setChecked(new Set())
    else setChecked(new Set(alerts.map(a => a.id)))
  }

  const bulkDelete = async () => {
    if (!window.confirm(`Delete ${checked.size} alert(s)?`)) return
    await Promise.all([...checked].map(id => api.deleteAlert(id).catch(() => {})))
    setChecked(new Set())
    refresh()
  }

  const resetFilters = () => {
    setQ(''); setSev(''); setUrg(''); setSt(''); setMsgType('')
    setFeedSrc(''); setFrom(''); setTo(''); setShowExpired(false)
    setPage(1)
  }

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Alerts {total > 0 && <span style={{color:'var(--muted)',fontSize:16}}>({total.toLocaleString()})</span>}</div>
        <div className="page-actions">
          {lastUpdate && <span className="last-updated">Updated {fmtTime(lastUpdate)}</span>}
          {checked.size > 0 && <button className="danger" onClick={bulkDelete}>Delete {checked.size}</button>}
          <button onClick={refresh}>Refresh</button>
        </div>
      </div>

      {/* Filters */}
      <div className="card" style={{ marginBottom:16, display:'flex', flexWrap:'wrap', gap:10, alignItems:'flex-end' }}>
        <div style={{ flex:'1 1 200px' }}>
          <label>Search</label>
          <input value={q} onChange={e => { setQ(e.target.value); setPage(1) }} placeholder="headline, event, area…" />
        </div>
        <div style={{ flex:'0 0 130px' }}>
          <label>Severity</label>
          <select value={severity} onChange={e => { setSev(e.target.value); setPage(1) }}>
            <option value="">All</option>
            {SEVERITIES.map(s => <option key={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ flex:'0 0 130px' }}>
          <label>Urgency</label>
          <select value={urgency} onChange={e => { setUrg(e.target.value); setPage(1) }}>
            <option value="">All</option>
            {URGENCIES.map(s => <option key={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ flex:'0 0 120px' }}>
          <label>Status</label>
          <select value={status} onChange={e => { setSt(e.target.value); setPage(1) }}>
            <option value="">All</option>
            {STATUSES.map(s => <option key={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ flex:'0 0 110px' }}>
          <label>Msg Type</label>
          <select value={msgType} onChange={e => { setMsgType(e.target.value); setPage(1) }}>
            <option value="">All</option>
            {MSG_TYPES.map(s => <option key={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ flex:'0 0 160px' }}>
          <label>Feed Source</label>
          <select value={feedSrc} onChange={e => { setFeedSrc(e.target.value); setPage(1) }}>
            <option value="">All</option>
            {/* Local CBE alerts aren't a configured FeedSource row, but they're
                real feed_source values on Alert rows same as any polled feed. */}
            {(feedNames.includes('Local CBE') ? feedNames : [...feedNames, 'Local CBE']).map(n => <option key={n}>{n}</option>)}
          </select>
        </div>
        <div style={{ flex:'0 0 160px' }}>
          <label>From</label>
          <input type="datetime-local" value={fromDate} onChange={e => { setFrom(e.target.value ? new Date(e.target.value).toISOString() : ''); setPage(1) }} />
        </div>
        <div style={{ flex:'0 0 160px' }}>
          <label>To</label>
          <input type="datetime-local" value={toDate} onChange={e => { setTo(e.target.value ? new Date(e.target.value).toISOString() : ''); setPage(1) }} />
        </div>
        <div style={{ display:'flex', alignItems:'center', gap:6, paddingBottom:2 }}>
          <input type="checkbox" id="showExp" checked={showExpired} onChange={e => { setShowExpired(e.target.checked); setPage(1) }} style={{ width:'auto' }} />
          <label htmlFor="showExp" style={{ margin:0, textTransform:'none', fontSize:12 }}>Show expired</label>
        </div>
        <button onClick={resetFilters} style={{ marginBottom:0 }}>Reset</button>
      </div>

      {error && <div className="error-msg">{error}</div>}
      {loading && <div className="loading">Loading…</div>}

      <div className="table-wrap card">
        <table>
          <thead><tr>
            <th><input type="checkbox" checked={checked.size === alerts.length && alerts.length > 0} onChange={toggleAll} style={{ width:'auto' }} /></th>
            <th>Sev</th>
            <SortTh field="event">Event</SortTh>
            <th>Headline</th>
            <SortTh field="area_desc">Area</SortTh>
            <SortTh field="sent">Sent</SortTh>
            <SortTh field="expires">Expires</SortTh>
            <th>Source</th>
            <th>Fwd</th>
          </tr></thead>
          <tbody>
            {alerts.map(a => (
              <tr key={a.id}
                style={{ cursor:'pointer', opacity: a.deleted_at ? 0.45 : 1, textDecoration: a.deleted_at ? 'line-through' : 'none' }}
                onClick={() => setSelected(a)}>
                <td onClick={e => { e.stopPropagation(); toggleCheck(a.id) }}>
                  <input type="checkbox" checked={checked.has(a.id)} onChange={() => toggleCheck(a.id)} style={{ width:'auto' }} />
                </td>
                <td><SeverityBadge severity={a.severity} /></td>
                <td style={{ maxWidth:160, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', fontFamily:'var(--font-ui)', fontWeight:600 }}>{a.event}</td>
                <td style={{ maxWidth:240, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', fontSize:12 }}>{a.headline}</td>
                <td style={{ maxWidth:160, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', fontSize:12 }}>{a.area_desc}</td>
                <td style={{ fontFamily:'var(--font-mono)', fontSize:11, whiteSpace:'nowrap' }}>{fmtRel(a.sent)}</td>
                <td style={{ fontFamily:'var(--font-mono)', fontSize:11, whiteSpace:'nowrap', color: new Date(a.expires) < new Date() ? 'var(--muted)' : 'var(--text)' }}>{fmtRel(a.expires)}</td>
                <td style={{ fontSize:11, color:'var(--muted)' }}>{a.feed_source}</td>
                <td>{a.forwarded ? <span style={{color:'var(--ok)'}}>✓</span> : <span style={{color:'var(--border)'}}>—</span>}</td>
              </tr>
            ))}
            {!loading && !alerts.length && (
              <tr><td colSpan={9} style={{color:'var(--muted)',textAlign:'center',padding:'20px 0'}}>No alerts match current filters.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div style={{ display:'flex', alignItems:'center', gap:10, marginTop:12, justifyContent:'center' }}>
          <button onClick={() => setPage(1)} disabled={page === 1}>«</button>
          <button onClick={() => setPage(p => Math.max(1, p-1))} disabled={page === 1}>‹</button>
          <span style={{ fontFamily:'var(--font-mono)', fontSize:13 }}>Page {page} / {totalPages}</span>
          <button onClick={() => setPage(p => Math.min(totalPages, p+1))} disabled={page === totalPages}>›</button>
          <button onClick={() => setPage(totalPages)} disabled={page === totalPages}>»</button>
        </div>
      )}

      {/* Detail Panel — key forces remount when a different alert is selected */}
      {selected && <AlertDetail key={selected.id} alert={selected} onClose={() => setSelected(null)} onDelete={() => { refresh(); setSelected(null) }} />}
    </div>
  )
}

function AlertDetail({ alert: initial, onClose, onDelete }) {
  const [alert, setAlert] = useState(initial)
  const [rawOpen, setRawOpen] = useState(false)
  const [mapOpen, setMapOpen] = useState(true)

  const handleDelete = async () => {
    if (!window.confirm('Soft-delete this alert?')) return
    await api.deleteAlert(alert.id)
    onDelete()
  }

  const hasGeometry = !!alert.geometry

  let rawPretty = alert.raw_cap
  try { rawPretty = JSON.stringify(JSON.parse(alert.raw_cap), null, 2) } catch {}

  return (
    <div className="detail-panel">
      <div className="detail-panel-header">
        <div style={{ fontFamily:'var(--font-ui)', fontWeight:700, letterSpacing:'0.05em', textTransform:'uppercase' }}>
          Alert Detail
        </div>
        <div style={{ display:'flex', gap:8 }}>
          <button className="danger" onClick={handleDelete}>Delete</button>
          <button onClick={onClose}>✕ Close</button>
        </div>
      </div>
      <div className="detail-panel-body">
        <Field label="ID"><code style={{fontSize:11,wordBreak:'break-all'}}>{alert.id}</code></Field>
        <Field label="Event"><strong>{alert.event}</strong></Field>
        <Field label="Severity / Urgency / Certainty">
          <SeverityBadge severity={alert.severity} />
          {' '}<span style={{color:'var(--muted)'}}>{alert.urgency} / {alert.certainty}</span>
        </Field>
        <Field label="Headline">{alert.headline}</Field>
        <Field label="Area">{alert.area_desc}</Field>
        {alert.geocodes && <Field label="Geocodes"><GeoCodeChips geocodes={alert.geocodes} /></Field>}
        <Field label="Status / Msg Type">
          <span className="badge" style={{background:'var(--surface2)',color:'var(--muted)',border:'1px solid var(--border)'}}>{alert.status}</span>
          {' '}<span className="badge" style={{background:'var(--surface2)',color:'var(--muted)',border:'1px solid var(--border)'}}>{alert.msg_type}</span>
        </Field>
        <div className="form-2col">
          <Field label="Sent"><mono>{fmtTime(alert.sent)}</mono></Field>
          <Field label="Expires"><mono style={{color: new Date(alert.expires)<new Date() ? 'var(--muted)':undefined}}>{fmtTime(alert.expires)}</mono></Field>
        </div>
        <div className="form-2col">
          <Field label="Effective"><mono>{fmtTime(alert.effective)}</mono></Field>
          <Field label="Onset"><mono>{fmtTime(alert.onset)}</mono></Field>
        </div>
        <Field label="Sender"><mono>{alert.sender}</mono></Field>
        <Field label="Feed Source"><mono>{alert.feed_source}</mono></Field>
        {alert.signature_status && <Field label="CAP Signature"><SignatureBadge status={alert.signature_status} /></Field>}
        <Field label="Forwarded">{alert.forwarded ? <span style={{color:'var(--ok)'}}>Yes</span> : 'No'}</Field>
        {alert.description && <Field label="Description"><div style={{whiteSpace:'pre-wrap',fontSize:13}}>{alert.description}</div></Field>}
        {alert.references && <Field label="References"><mono style={{fontSize:11,wordBreak:'break-all'}}>{alert.references}</mono></Field>}

        {/* Map */}
        {hasGeometry && (
          <div style={{ marginTop:16 }}>
            <button onClick={() => setMapOpen(o => !o)} style={{ marginBottom: mapOpen ? 0 : 8 }}>
              {mapOpen ? '▾' : '▸'} Alert Area Map
            </button>
            {mapOpen && (
              <AlertMap
                geometry={alert.geometry}
                severity={alert.severity}
                areaDesc={alert.area_desc}
              />
            )}
          </div>
        )}

        <div style={{ marginTop:16 }}>
          <button onClick={() => setRawOpen(o => !o)} style={{ marginBottom:8 }}>
            {rawOpen ? '▾' : '▸'} Raw CAP
          </button>
          {rawOpen && <pre className="raw-cap">{rawPretty || '(empty)'}</pre>}
        </div>
      </div>
    </div>
  )
}

const SIGNATURE_STYLES = {
  verified: { color:'var(--success, #3ac97a)', border:'rgba(58,201,122,0.3)' },
  invalid:  { color:'var(--danger, #e05a5a)',  border:'rgba(224,90,90,0.3)' },
  revoked:  { color:'var(--danger, #e05a5a)',  border:'rgba(224,90,90,0.3)' },
  unsigned: { color:'var(--muted)',            border:'var(--border)' },
}

function SignatureBadge({ status }) {
  const s = SIGNATURE_STYLES[status] || SIGNATURE_STYLES.unsigned
  return (
    <span className="badge" style={{ background:'var(--surface2)', color:s.color, border:`1px solid ${s.border}` }}>
      {status}
    </span>
  )
}

function GeoCodeChips({ geocodes }) {
  let entries = []
  try { entries = JSON.parse(geocodes) } catch { return null }
  if (!Array.isArray(entries) || !entries.length) return null
  return (
    <div style={{ display:'flex', gap:6, flexWrap:'wrap' }}>
      {entries.map((gc, i) => (
        <span key={i} className="chip">
          <code style={{fontSize:11, fontWeight:700}}>{gc.type}</code>
          <span style={{color:'var(--muted)', margin:'0 4px'}}>=</span>
          <code style={{fontSize:11}}>{gc.value}</code>
        </span>
      ))}
    </div>
  )
}

function Field({ label, children }) {
  return (
    <div className="detail-field">
      <div className="detail-field-label">{label}</div>
      <div className="detail-field-value">{children}</div>
    </div>
  )
}

function mono({ children, style }) {
  return <span style={{ fontFamily:'var(--font-mono)', fontSize:12, ...style }}>{children}</span>
}

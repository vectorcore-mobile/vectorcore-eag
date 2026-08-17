import React from 'react'

// Standard NWS/CAP event type names — used both as the XMPP peer event
// filter options and as the CBE compose-form Event dropdown.
export const EVENT_TYPES = [
  'Tornado Warning','Tornado Watch',
  'Severe Thunderstorm Warning','Severe Thunderstorm Watch',
  'Flash Flood Warning','Flash Flood Watch','Flash Flood Statement',
  'Flood Warning','Flood Watch','Flood Statement','Flood Advisory',
  'Areal Flood Warning','Areal Flood Watch','Areal Flood Advisory',
  'Special Marine Warning',
  'Winter Storm Warning','Winter Storm Watch',
  'Blizzard Warning','Blizzard Watch',
  'Ice Storm Warning','Ice Storm Watch',
  'Freezing Rain Advisory','Sleet Advisory',
  'Snow Squall Warning',
  'Wind Chill Warning','Wind Chill Watch','Wind Chill Advisory',
  'High Wind Warning','High Wind Watch','Wind Advisory',
  'Lake Effect Snow Warning','Lake Effect Snow Watch','Lake Effect Snow Advisory',
  'Dense Fog Advisory','Dense Smoke Advisory',
  'Freeze Warning','Freeze Watch','Frost Advisory',
  'Hard Freeze Warning','Hard Freeze Watch',
  'Heat Advisory','Excessive Heat Warning','Excessive Heat Watch',
  'Tropical Storm Warning','Tropical Storm Watch',
  'Hurricane Warning','Hurricane Watch',
  'Storm Surge Warning','Storm Surge Watch',
  'Tsunami Warning','Tsunami Watch','Tsunami Advisory','Tsunami Statement',
  'Coastal Flood Warning','Coastal Flood Watch','Coastal Flood Advisory','Coastal Flood Statement',
  'High Surf Warning','High Surf Advisory','Rip Current Statement',
  'Beach Hazards Statement',
  'Lakeshore Flood Warning','Lakeshore Flood Watch','Lakeshore Flood Advisory',
  'Fire Weather Watch','Red Flag Warning',
  'Dust Storm Warning','Dust Advisory','Blowing Dust Advisory',
  'Air Quality Alert','Air Stagnation Advisory',
  'Ashfall Warning','Ashfall Advisory',
  'Avalanche Warning','Avalanche Watch','Avalanche Advisory',
  'Earthquake Warning','Volcano Warning',
  'Civil Emergency Message','Civil Danger Warning',
  'Evacuation Immediate','Shelter In Place Warning',
  'Law Enforcement Warning','Nuclear Power Plant Warning',
  'Radiological Hazard Warning','Hazmat Warning',
  'Child Abduction Emergency','Blue Alert',
  '911 Telephone Outage Emergency','Local Area Emergency',
  'Special Weather Statement','Hazardous Weather Outlook',
  'Short Term Forecast','Administrative Message','Test','Demo Warning',
]

export function SeverityBadge({ severity }) {
  const s = (severity || 'Unknown').toLowerCase()
  const cls = s === 'extreme' ? 'sev-extreme'
            : s === 'severe'  ? 'sev-severe'
            : s === 'moderate' ? 'sev-moderate'
            : s === 'minor'   ? 'sev-minor'
            : 'sev-unknown'
  return <span className={`badge ${cls}`}>{severity || 'Unknown'}</span>
}

export function SeverityDot({ severity }) {
  const s = (severity || '').toLowerCase()
  const color = s === 'extreme' ? 'var(--critical)'
              : s === 'severe'  ? 'var(--warning)'
              : s === 'moderate' ? 'var(--caution)'
              : s === 'minor'   ? 'var(--info)'
              : 'var(--muted)'
  return <span style={{ display:'inline-block', width:8, height:8, borderRadius:'50%', background:color, boxShadow: s==='extreme'?'0 0 5px var(--critical)':undefined }} />
}

export function StatusBadge({ status }) {
  const ok       = status === 'ok'
  const disabled = status === 'disabled'
  const style = ok
    ? { background:'rgba(0,217,126,0.12)', color:'var(--ok)', border:'1px solid rgba(0,217,126,0.3)' }
    : disabled
      ? { background:'var(--surface2)', color:'var(--muted)', border:'1px solid var(--border)' }
      : {}
  return <span className={`badge ${!ok && !disabled ? 'sev-extreme' : ''}`} style={style}>
    {status || '—'}
  </span>
}

export function ConnDot({ connected }) {
  return <span className={`dot ${connected ? 'dot-ok' : 'dot-err'}`} title={connected ? 'Connected' : 'Disconnected'} />
}

export function Toggle({ checked, onChange }) {
  return (
    <label className="toggle">
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      <span className="toggle-slider" />
    </label>
  )
}

function isEditableTarget(el) {
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

export function Modal({ title, onClose, children, footer, closeOnBackdrop = true, closeOnEscape = true }) {
  const onCloseRef = React.useRef(onClose)
  const closeOnEscapeRef = React.useRef(closeOnEscape)
  React.useEffect(() => { onCloseRef.current = onClose })
  React.useEffect(() => { closeOnEscapeRef.current = closeOnEscape })

  React.useEffect(() => {
    const handleKey = (e) => {
      if (e.key === 'Escape') {
        if (closeOnEscapeRef.current) onCloseRef.current()
        return
      }
      // The browser's legacy "Backspace navigates back" behavior fires whenever
      // focus sits on a non-editable element — silently unmounting the dialog
      // via history navigation. Suppress it outside editable controls so
      // Backspace only ever does its normal thing inside inputs.
      if (e.key === 'Backspace' && !isEditableTarget(e.target)) {
        e.preventDefault()
      }
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [])

  return (
    <div className="modal-backdrop" onClick={e => e.target === e.currentTarget && closeOnBackdrop && onClose()}>
      <div className="modal">
        <div className="modal-title">{title}</div>
        {children}
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  )
}

export function ChipInput({ values, onChange, placeholder }) {
  const [input, setInput] = React.useState('')
  const add = () => {
    const v = input.trim()
    if (v && !values.includes(v)) onChange([...values, v])
    setInput('')
  }
  return (
    <div>
      <div style={{ display:'flex', gap:6 }}>
        <input value={input} onChange={e => setInput(e.target.value)}
          onKeyDown={e => e.key==='Enter' && (e.preventDefault(), add())}
          placeholder={placeholder || 'Add value…'} style={{ flex:1 }} />
        <button type="button" onClick={add} style={{ minWidth:40 }}>+</button>
      </div>
      {values.length > 0 && (
        <div className="chips" style={{ marginTop:6 }}>
          {values.map(v => (
            <span key={v} className="chip">
              {v}
              <span className="chip-remove" onClick={() => onChange(values.filter(x => x !== v))}>×</span>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

// SelectChipInput — dropdown of predefined options + removable chip display.
// Mirrors the APNs selector pattern from vectorcore-hss.
export function SelectChipInput({ values, onChange, options, placeholder }) {
  const [picked, setPicked] = React.useState('')
  const available = options.filter(o => !values.includes(o))

  const add = () => {
    if (picked && !values.includes(picked)) {
      onChange([...values, picked])
      setPicked('')
    }
  }

  return (
    <div>
      <div style={{ display:'flex', gap:6 }}>
        <select value={picked} onChange={e => setPicked(e.target.value)} style={{ flex:1 }}
          disabled={available.length === 0}>
          <option value="">{available.length === 0 ? '(all selected)' : placeholder || '— Select —'}</option>
          {available.map(o => <option key={o} value={o}>{o}</option>)}
        </select>
        <button type="button" onClick={add} disabled={!picked} style={{ minWidth:40 }}>+</button>
      </div>
      {values.length > 0 && (
        <div className="chips" style={{ marginTop:6 }}>
          {values.map(v => (
            <span key={v} className="chip">
              {v}
              <span className="chip-remove" onClick={() => onChange(values.filter(x => x !== v))}>×</span>
            </span>
          ))}
        </div>
      )}
      {values.length === 0 && (
        <div style={{ fontSize:11, color:'var(--muted)', marginTop:4 }}>Empty = match all</div>
      )}
    </div>
  )
}

export function fmtTime(ts) {
  if (!ts) return '—'
  try { return new Date(ts).toLocaleString() } catch { return ts }
}

export function fmtRel(ts) {
  if (!ts) return '—'
  const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (diff < 60)   return `${diff}s ago`
  const mins = Math.floor(diff / 60)
  if (mins < 60)   return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24)    return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

export function useAutoRefresh(fn, interval, pause) {
  React.useEffect(() => {
    if (pause) return
    fn()
    const id = setInterval(() => {
      if (document.visibilityState !== 'hidden') fn()
    }, interval)
    return () => clearInterval(id)
  }, [pause])
}

// Subscribes to the server's SSE event stream and calls fn whenever a peer
// connects or disconnects. Reconnects automatically on error.
export function usePeerEvents(fn) {
  React.useEffect(() => {
    let es
    let retryTimer

    const connect = () => {
      es = new EventSource('/api/v1/system/events')
      es.addEventListener('peer-change', () => {
        if (document.visibilityState !== 'hidden') fn()
      })
      es.onerror = () => {
        es.close()
        retryTimer = setTimeout(connect, 3000)
      }
    }

    connect()
    return () => {
      clearTimeout(retryTimer)
      es?.close()
    }
  }, []) // fn is stable (useCallback at call site)
}

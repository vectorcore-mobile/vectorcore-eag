import React, { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import 'leaflet-draw'
import 'leaflet-draw/dist/leaflet.draw.css'
import { api } from '../api.js'

const CUSTOM_EVENT = '__custom__'

// WEA alert classes (imminent threat / AMBER / public safety / test /
// presidential) — distinct from the specific NWS event names used for the
// XMPP peer event filter (shared.jsx's EVENT_TYPES); those describe the
// actual hazard ("Tornado Warning"), these describe the WEA delivery class.
const WEA_EVENT_TYPES = ['Imminent Threat', 'AMBER Alert', 'Public Safety', 'Test Message', 'Presidential Alert']

// Mirrors internal/api/handlers_cbe.go's weaSAMEEventCode — the CBC
// classifies alerts by machine-readable SAME eventCode, not the free-text
// <event> field (see cbe-cap-classification-corrections.md). "Imminent
// Threat" and "Public Safety" are intentionally absent: Imminent Threat
// already classifies correctly from severity/urgency/certainty, and Public
// Safety has no confirmed standard Message ID (a CBC-side config item).
const WEA_SAME_EVENT_CODE = {
  'Presidential Alert': 'EAN',
  'AMBER Alert': 'CAE',
  'Test Message': 'RMT',
}

const CATEGORIES = ['Geo','Met','Safety','Security','Rescue','Fire','Health','Env','Transport','Infra','CBRNE','Other']
const SEVERITIES  = ['Extreme','Severe','Moderate','Minor','Unknown']
const URGENCIES   = ['Immediate','Expected','Future','Past','Unknown']
const CERTAINTIES = ['Observed','Likely','Possible','Unlikely','Unknown']
const STATUSES    = ['Actual','Exercise','System','Test','Draft']
const MSG_TYPES   = ['Alert','Update','Cancel']
const SCOPES      = ['Public','Restricted','Private']

const EMPTY_FORM = {
  sender: '', sender_name: '', category: 'Other', event: '',
  headline: '', description: '', instruction: '',
  severity: 'Severe', urgency: 'Immediate', certainty: 'Likely',
  status: 'Actual', msg_type: 'Alert', scope: 'Public', references: '',
  effective: '', onset: '', expires: '', area_desc: '',
}

// datetime-local input value ("YYYY-MM-DDTHH:mm") → ISO 8601 for the API.
function localToISO(local) {
  return local ? new Date(local).toISOString() : ''
}

// ISO 8601 → datetime-local input value, for populating the form on import.
function isoToLocalInput(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function xmlEscape(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;').replaceAll("'", '&apos;')
}
function xmlTag(name, value) {
  return `<${name}>${xmlEscape(value)}</${name}>`
}

// CAP 1.2 requires dateTime values as "YYYY-MM-DDThh:mm:ssXzh:zm" and
// explicitly prohibits the "Z" designator: "Alphabetic timezone designators
// such as 'Z' MUST NOT be used. The timezone for UTC MUST be represented as
// '-00:00'" (CAP-v1.2-os §3.3.2). Date.toISOString() always emits "Z" (and
// millisecond precision CAP's format has no room for), so both need fixing
// up for an exported CAP document.
function capDateTime(d) {
  return d.toISOString().replace(/\.\d{3}Z$/, '-00:00')
}

// Mirrors internal/api/handlers_cbe.go's buildCBECAPXML — client-side so
// Export works on the current unsaved form state without a round trip.
function buildExportCAPXML(form, polygons, circles, geocodes) {
  const id = 'CBE-' + (crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(16).slice(2)}`)
  let xml = '<?xml version="1.0" encoding="UTF-8"?>'
  xml += '<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">'
  xml += xmlTag('identifier', id)
  xml += xmlTag('sender', form.sender)
  xml += xmlTag('sent', capDateTime(new Date()))
  xml += xmlTag('status', form.status || 'Actual')
  xml += xmlTag('msgType', form.msg_type || 'Alert')
  xml += xmlTag('scope', form.scope || 'Public')
  if (form.references) xml += xmlTag('references', form.references)
  xml += '<info>'
  xml += xmlTag('language', 'en-US')
  xml += xmlTag('category', form.category || 'Other')
  xml += xmlTag('event', form.event)
  xml += xmlTag('urgency', form.urgency || 'Immediate')
  xml += xmlTag('severity', form.severity || 'Severe')
  xml += xmlTag('certainty', form.certainty || 'Likely')
  if (WEA_SAME_EVENT_CODE[form.event]) {
    xml += '<eventCode>' + xmlTag('valueName', 'SAME') + xmlTag('value', WEA_SAME_EVENT_CODE[form.event]) + '</eventCode>'
  }
  if (form.effective) xml += xmlTag('effective', capDateTime(new Date(form.effective)))
  if (form.onset) xml += xmlTag('onset', capDateTime(new Date(form.onset)))
  if (form.expires) xml += xmlTag('expires', capDateTime(new Date(form.expires)))
  if (form.sender_name) xml += xmlTag('senderName', form.sender_name)
  if (form.headline) xml += xmlTag('headline', form.headline)
  if (form.description) xml += xmlTag('description', form.description)
  if (form.instruction) xml += xmlTag('instruction', form.instruction)
  xml += '<area>'
  xml += xmlTag('areaDesc', form.area_desc || '')
  for (const poly of polygons) xml += xmlTag('polygon', poly)
  for (const c of circles) xml += xmlTag('circle', c)
  for (const gc of geocodes) {
    xml += '<geocode>' + xmlTag('valueName', gc.type) + xmlTag('value', gc.code) + '</geocode>'
  }
  xml += '</area>'
  xml += '</info>'
  xml += '</alert>'
  return xml
}

// Drawn shapes → CAP <polygon> ring strings ("lat,lon lat,lon ...") and
// <circle> strings ("lat,lon radius_km"). Mirrors the lon/lat swap and
// meters→km conversion done server-side.
function shapesToCAPGeometry(featureGroup) {
  const polygons = []
  const circles = []
  if (!featureGroup) return { polygons, circles }
  featureGroup.eachLayer(layer => {
    if (layer instanceof L.Circle) {
      const { lat, lng } = layer.getLatLng()
      circles.push(`${lat},${lng} ${layer.getRadius() / 1000}`)
      return
    }
    const ring = layer.toGeoJSON().geometry?.coordinates?.[0]
    if (!ring) return
    polygons.push(ring.map(([lon, lat]) => `${lat},${lon}`).join(' '))
  })
  return { polygons, circles }
}

// GeoJSON has no native circle type, so a drawn L.Circle is serialized as a
// Point Feature carrying a "radius" property in meters (Leaflet's native
// unit) — mirrors internal/api/handlers_cbe.go's extractCircles, which reads
// that same shape back out. Polygons/rectangles use Leaflet's own toGeoJSON.
function featureGroupToGeoJSON(featureGroup) {
  const features = []
  featureGroup.eachLayer(layer => {
    if (layer instanceof L.Circle) {
      const { lat, lng } = layer.getLatLng()
      features.push({
        type: 'Feature',
        properties: { radius: layer.getRadius() },
        geometry: { type: 'Point', coordinates: [lng, lat] },
      })
      return
    }
    features.push(layer.toGeoJSON())
  })
  return { type: 'FeatureCollection', features }
}

function downloadFile(content, filename, mimeType) {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function tagText(doc, name) {
  const el = doc.getElementsByTagName(name)[0]
  return el ? el.textContent.trim() : ''
}

// Multi-select of reference GeoCode records (id-keyed, richer display than
// shared.jsx's flat-string SelectChipInput) — additive to the drawn polygon.
function GeoCodeChipSelect({ geoCodes, selectedIds, onChange }) {
  const [picked, setPicked] = useState('')
  const available = geoCodes.filter(g => !selectedIds.includes(g.id))

  const add = () => {
    if (!picked) return
    onChange([...selectedIds, Number(picked)])
    setPicked('')
  }
  const remove = (id) => onChange(selectedIds.filter(x => x !== id))

  return (
    <div>
      <div style={{ display:'flex', gap:6 }}>
        <select value={picked} onChange={e => setPicked(e.target.value)} style={{ flex:1 }}
          disabled={available.length === 0}>
          <option value="">{available.length === 0 ? '(none available)' : '— Select geo code —'}</option>
          {available.map(g => <option key={g.id} value={g.id}>{g.type} {g.code}{g.description ? ` — ${g.description}` : ''}</option>)}
        </select>
        <button type="button" onClick={add} disabled={!picked} style={{ minWidth:40 }}>+</button>
      </div>
      {selectedIds.length > 0 && (
        <div className="chips" style={{ marginTop:6 }}>
          {selectedIds.map(id => {
            const g = geoCodes.find(x => x.id === id)
            if (!g) return null
            return (
              <span key={id} className="chip">
                {g.type} {g.code}
                <span className="chip-remove" onClick={() => remove(id)}>×</span>
              </span>
            )
          })}
        </div>
      )}
    </div>
  )
}

export default function CBE() {
  const mapRef          = useRef(null)
  const containerRef    = useRef(null)
  const featureGroupRef = useRef(null)
  const drawControlRef  = useRef(null)
  const fileInputRef    = useRef(null)

  const [form, setForm]         = useState(EMPTY_FORM)
  const [eventChoice, setEventChoice] = useState('')
  const [mapOpen, setMapOpen]   = useState(false)
  const [shapeCount, setShapeCount] = useState(0)
  const [priorAlerts, setPriorAlerts] = useState([])
  const [refPick, setRefPick]   = useState('')
  const [geoCodes, setGeoCodes] = useState([])
  const [selectedGeoCodeIds, setSelectedGeoCodeIds] = useState([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError]       = useState(null)
  const [result, setResult]     = useState(null)

  const setF = (k, v) => setForm(f => ({ ...f, [k]: v }))

  // Init map + draw controls once.
  useEffect(() => {
    if (!containerRef.current || mapRef.current) return

    const map = L.map(containerRef.current, { zoomControl: true, attributionControl: true })
    map.setView([38, -96], 4)

    // Light basemap — dark tiles made it hard to see terrain/roads while
    // drawing an area polygon; a light basemap gives the orange draw
    // overlay much better contrast.
    L.tileLayer('https://{s}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/">CARTO</a>',
      subdomains: 'abcd',
      maxZoom: 19,
    }).addTo(map)

    const featureGroup = new L.FeatureGroup().addTo(map)
    featureGroupRef.current = featureGroup

    const drawControl = new L.Control.Draw({
      position: 'topright',
      draw: {
        // showArea left off — leaflet-draw 1.0.4's readableArea() throws
        // ("type is not defined") against this Leaflet version's GeometryUtil.
        polygon: { allowIntersection: false, showArea: false, shapeOptions: { color: '#f5a623' } },
        rectangle: { showArea: false, shapeOptions: { color: '#f5a623' } },
        circle: { shapeOptions: { color: '#f5a623' } },
        circlemarker: false,
        marker: false,
        polyline: false,
      },
      edit: { featureGroup, remove: true },
    })
    map.addControl(drawControl)
    drawControlRef.current = drawControl

    const updateCount = () => setShapeCount(featureGroup.getLayers().length)

    map.on(L.Draw.Event.CREATED, (e) => {
      featureGroup.addLayer(e.layer)
      updateCount()
    })
    map.on(L.Draw.Event.EDITED, updateCount)
    map.on(L.Draw.Event.DELETED, updateCount)

    mapRef.current = map

    return () => {
      map.remove()
      mapRef.current = null
    }
  }, [])

  // The map container stays mounted (so drawn shapes and the Leaflet
  // instance survive between opens) but sits behind display:none while the
  // popup is closed, which leaves it sized 0x0. Recalculate tile layout
  // once it becomes visible.
  useEffect(() => {
    if (!mapOpen || !mapRef.current) return
    const id = setTimeout(() => mapRef.current.invalidateSize(), 50)
    return () => clearTimeout(id)
  }, [mapOpen])

  // Load prior CBE alerts for the reference picker once an Update/Cancel is selected.
  useEffect(() => {
    if (form.msg_type === 'Alert') return
    api.getAlerts({ feed_source: 'Local CBE', limit: 50, sort: 'sent', order: 'desc' })
      .then(r => setPriorAlerts(r?.alerts || []))
      .catch(() => {})
  }, [form.msg_type])

  // Load the curated geo code reference list (managed on the Geo Codes page).
  useEffect(() => {
    api.getGeoCodes()
      .then(r => setGeoCodes((r || []).filter(g => g.enabled)))
      .catch(() => {})
  }, [])

  const pickReference = (id) => {
    setRefPick(id)
    const a = priorAlerts.find(x => x.id === id)
    if (!a) { setF('references', ''); return }
    setF('references', `${a.sender},${a.id},${a.sent}`)
  }

  const clearShapes = () => {
    featureGroupRef.current?.clearLayers()
    setShapeCount(0)
  }

  const resetAll = () => {
    setForm(EMPTY_FORM)
    setRefPick('')
    setEventChoice('')
    setSelectedGeoCodeIds([])
    clearShapes()
    setError(null)
  }

  const pickEvent = (v) => {
    setEventChoice(v)
    if (v !== CUSTOM_EVENT) setF('event', v)
    else setF('event', '')
  }

  const selectedGeoCodePayload = () => selectedGeoCodeIds
    .map(id => geoCodes.find(g => g.id === id))
    .filter(Boolean)
    .map(g => ({ type: g.type, code: g.code }))

  const exportCAP = () => {
    const { polygons, circles } = shapesToCAPGeometry(featureGroupRef.current)
    const xml = buildExportCAPXML(form, polygons, circles, selectedGeoCodePayload())
    const slug = (form.event || 'draft').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
    downloadFile(xml, `cap-alert-${slug}-${Date.now()}.xml`, 'application/xml')
  }

  const triggerImport = () => fileInputRef.current?.click()

  const handleImportFile = async (e) => {
    const file = e.target.files?.[0]
    e.target.value = '' // allow re-importing the same file later
    if (!file) return

    setError(null)
    let text
    try {
      text = await file.text()
    } catch (err) {
      setError('Could not read file: ' + err.message)
      return
    }

    const doc = new DOMParser().parseFromString(text, 'application/xml')
    if (doc.getElementsByTagName('parsererror').length > 0) {
      setError('Invalid CAP XML — could not parse the file.')
      return
    }

    const imported = {
      sender:      tagText(doc, 'sender'),
      sender_name: tagText(doc, 'senderName'),
      category:    tagText(doc, 'category'),
      event:       tagText(doc, 'event'),
      urgency:     tagText(doc, 'urgency'),
      severity:    tagText(doc, 'severity'),
      certainty:   tagText(doc, 'certainty'),
      status:      tagText(doc, 'status'),
      msg_type:    tagText(doc, 'msgType'),
      scope:       tagText(doc, 'scope'),
      references:  tagText(doc, 'references'),
      effective:   isoToLocalInput(tagText(doc, 'effective')),
      onset:       isoToLocalInput(tagText(doc, 'onset')),
      expires:     isoToLocalInput(tagText(doc, 'expires')),
      headline:    tagText(doc, 'headline'),
      description: tagText(doc, 'description'),
      instruction: tagText(doc, 'instruction'),
      area_desc:   tagText(doc, 'areaDesc'),
    }

    if (!imported.event && !imported.sender) {
      setError('That file doesn\'t look like a CAP alert — no <sender> or <event> found.')
      return
    }

    setForm(f => ({
      ...f,
      ...Object.fromEntries(Object.entries(imported).filter(([, v]) => v !== '')),
    }))
    setEventChoice(WEA_EVENT_TYPES.includes(imported.event) ? imported.event : (imported.event ? CUSTOM_EVENT : ''))
    setRefPick('')

    // Match imported <geocode> entries against the reference list by
    // type+code. Anything not already in the curated list is dropped —
    // there's no ID to select for a code we don't have on file.
    const geocodeEls = Array.from(doc.getElementsByTagName('geocode'))
    const matchedIds = []
    for (const el of geocodeEls) {
      const type = el.getElementsByTagName('valueName')[0]?.textContent.trim()
      const code = el.getElementsByTagName('value')[0]?.textContent.trim()
      const match = geoCodes.find(g => g.type === type && g.code === code)
      if (match) matchedIds.push(match.id)
    }
    setSelectedGeoCodeIds(matchedIds)

    // Import <polygon> and <circle> shapes onto the map, replacing whatever's
    // drawn now.
    const polygonEls = Array.from(doc.getElementsByTagName('polygon'))
    const circleEls = Array.from(doc.getElementsByTagName('circle'))
    if ((polygonEls.length || circleEls.length) && featureGroupRef.current) {
      featureGroupRef.current.clearLayers()
      for (const el of polygonEls) {
        const text = el.textContent.trim()
        if (!text) continue
        let points = text.split(/\s+/).map(pair => {
          const [lat, lon] = pair.split(',').map(Number)
          return [lat, lon]
        }).filter(([lat, lon]) => Number.isFinite(lat) && Number.isFinite(lon))
        // drop a duplicated closing point so it's clean to re-edit
        if (points.length > 1) {
          const [flat, flon] = points[0]
          const [llat, llon] = points[points.length - 1]
          if (flat === llat && flon === llon) points = points.slice(0, -1)
        }
        if (points.length >= 3) {
          featureGroupRef.current.addLayer(L.polygon(points, { color: '#f5a623' }))
        }
      }
      for (const el of circleEls) {
        const text = el.textContent.trim()
        if (!text) continue
        const [center, radiusKm] = text.split(/\s+/)
        const [lat, lon] = (center || '').split(',').map(Number)
        const radius = Number(radiusKm) * 1000
        if (Number.isFinite(lat) && Number.isFinite(lon) && Number.isFinite(radius) && radius > 0) {
          featureGroupRef.current.addLayer(L.circle([lat, lon], { radius, color: '#f5a623' }))
        }
      }
      setShapeCount(featureGroupRef.current.getLayers().length)
    }
  }

  const submit = async (e) => {
    e.preventDefault()
    setError(null)

    const hasShapes = featureGroupRef.current && featureGroupRef.current.getLayers().length > 0
    const hasGeoCodes = selectedGeoCodeIds.length > 0
    if (!hasShapes && !hasGeoCodes) {
      setError('Click "Create Shapes" and draw at least one polygon or rectangle, or select a Geo Code, before submitting.')
      return
    }
    if (!form.sender || !form.event || !form.expires || !form.area_desc) {
      setError('Sender, Event, Expires, and Area Description are required.')
      return
    }

    const body = {
      ...form,
      effective: localToISO(form.effective),
      onset:     localToISO(form.onset),
      expires:   localToISO(form.expires),
      geometry:  hasShapes ? JSON.stringify(featureGroupToGeoJSON(featureGroupRef.current)) : '',
      geocodes:  selectedGeoCodePayload(),
    }

    setSubmitting(true)
    try {
      const created = await api.createCBEAlert(body)
      setResult(created)
      resetAll()
    } catch (err) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div className="page-title">CBE <span style={{color:'var(--muted)',fontSize:16}}>— Alert Origination</span></div>
      </div>

      {result && (
        <div className="card" style={{ marginBottom:16, borderColor:'var(--ok)' }}>
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', flexWrap:'wrap', gap:8 }}>
            <div>
              <div style={{ fontFamily:'var(--font-ui)', fontWeight:700, color:'var(--ok)', textTransform:'uppercase', letterSpacing:'0.05em', marginBottom:4 }}>
                Alert submitted to queue
              </div>
              <div style={{ fontFamily:'var(--font-mono)', fontSize:12, color:'var(--muted)' }}>
                {result.id} — forwarded: {result.forwarded ? 'yes' : 'no (no matching peers online yet)'}
              </div>
            </div>
            <div style={{ display:'flex', gap:8 }}>
              <Link to={`/alerts?feed_source=${encodeURIComponent('Local CBE')}`}><button type="button">View in Alerts</button></Link>
              <button type="button" onClick={() => setResult(null)}>Dismiss</button>
            </div>
          </div>
        </div>
      )}

      {error && <div className="error-msg">{error}</div>}

      <form onSubmit={submit}>
        <div style={{ maxWidth:900, margin:'0 auto' }}>
          <div className="card">
            <div className="section-title" style={{ marginTop:0 }}>Originator</div>
            <div className="form-2col">
              <div className="form-row">
                <label>Sender *</label>
                <input value={form.sender} onChange={e => setF('sender', e.target.value)}
                  placeholder="cbe@eag.example.com" required />
              </div>
              <div className="form-row">
                <label>Sender Name</label>
                <input value={form.sender_name} onChange={e => setF('sender_name', e.target.value)}
                  placeholder="Ops Room Console" />
              </div>
            </div>

            <div className="section-title">Message</div>
            <div className="form-2col">
              <div className="form-row">
                <label>Status</label>
                <select value={form.status} onChange={e => setF('status', e.target.value)}>
                  {STATUSES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
              <div className="form-row">
                <label>Msg Type</label>
                <select value={form.msg_type} onChange={e => setF('msg_type', e.target.value)}>
                  {MSG_TYPES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
            </div>
            <div className="form-2col">
              <div className="form-row">
                <label>Scope</label>
                <select value={form.scope} onChange={e => setF('scope', e.target.value)}>
                  {SCOPES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
              <div className="form-row">
                <label>Category</label>
                <select value={form.category} onChange={e => setF('category', e.target.value)}>
                  {CATEGORIES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
            </div>

            {form.msg_type !== 'Alert' && (
              <div className="form-row">
                <label>References <span style={{fontWeight:400,textTransform:'none'}}>— prior CBE alert being {form.msg_type.toLowerCase()}d</span></label>
                <select value={refPick} onChange={e => pickReference(e.target.value)}>
                  <option value="">— Select prior CBE alert —</option>
                  {priorAlerts.map(a => <option key={a.id} value={a.id}>{a.event} — {a.id}</option>)}
                </select>
                {form.references && <div style={{ fontFamily:'var(--font-mono)', fontSize:11, color:'var(--muted)', marginTop:4, wordBreak:'break-all' }}>{form.references}</div>}
              </div>
            )}

            <div className="section-title">Content</div>
            <div className="form-row">
              <label>Event *</label>
              <select value={eventChoice} onChange={e => pickEvent(e.target.value)} required>
                <option value="">— Select event type —</option>
                {WEA_EVENT_TYPES.map(ev => <option key={ev} value={ev}>{ev}</option>)}
                <option value={CUSTOM_EVENT}>Other (type below)…</option>
              </select>
              {eventChoice === CUSTOM_EVENT && (
                <input style={{ marginTop:6 }} value={form.event} onChange={e => setF('event', e.target.value)}
                  placeholder="Custom event type" required />
              )}
            </div>
            <div className="form-row">
              <label>Headline</label>
              <input value={form.headline} onChange={e => setF('headline', e.target.value)}
                placeholder="Short summary shown to recipients" />
            </div>
            <div className="form-2col">
              <div className="form-row">
                <label>Description</label>
                <textarea rows={3} value={form.description} onChange={e => setF('description', e.target.value)}
                  placeholder="Full details of the hazard" />
              </div>
              <div className="form-row">
                <label>Instruction</label>
                <textarea rows={3} value={form.instruction} onChange={e => setF('instruction', e.target.value)}
                  placeholder="Recommended action for the public" />
              </div>
            </div>

            <div className="section-title">Classification</div>
            <div className="form-2col">
              <div className="form-row">
                <label>Severity</label>
                <select value={form.severity} onChange={e => setF('severity', e.target.value)}>
                  {SEVERITIES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
              <div className="form-row">
                <label>Urgency</label>
                <select value={form.urgency} onChange={e => setF('urgency', e.target.value)}>
                  {URGENCIES.map(s => <option key={s}>{s}</option>)}
                </select>
              </div>
            </div>
            <div className="form-row">
              <label>Certainty</label>
              <select value={form.certainty} onChange={e => setF('certainty', e.target.value)}>
                {CERTAINTIES.map(s => <option key={s}>{s}</option>)}
              </select>
            </div>

            <div className="section-title">Timing</div>
            <div className="form-2col">
              <div className="form-row">
                <label>Effective <span style={{fontWeight:400,textTransform:'none'}}>(default: now)</span></label>
                <input type="datetime-local" value={form.effective} onChange={e => setF('effective', e.target.value)} />
              </div>
              <div className="form-row">
                <label>Onset</label>
                <input type="datetime-local" value={form.onset} onChange={e => setF('onset', e.target.value)} />
              </div>
            </div>
            <div className="form-row">
              <label>Expires *</label>
              <input type="datetime-local" value={form.expires} onChange={e => setF('expires', e.target.value)} required />
            </div>

            <div className="section-title">Area</div>
            <div className="form-row">
              <label>Area Description *</label>
              <input value={form.area_desc} onChange={e => setF('area_desc', e.target.value)}
                placeholder="e.g. Jefferson County, AL" required />
            </div>
            <div className="form-row">
              <label>Area Shapes <span style={{fontWeight:400,textTransform:'none'}}>— polygon/rectangle/circle, drawn on a map (or use Geo Codes below instead)</span></label>
              <div style={{ display:'flex', alignItems:'center', gap:10, fontSize:12, color:'var(--muted)', flexWrap:'wrap' }}>
                <span>{shapeCount} shape{shapeCount === 1 ? '' : 's'} drawn</span>
                <button type="button" onClick={() => setMapOpen(true)}>Create Shapes</button>
                <button type="button" onClick={clearShapes} disabled={shapeCount === 0}>Clear Shapes</button>
              </div>
            </div>
            <div className="form-row" style={{ marginBottom:0 }}>
              <label>Geo Codes <span style={{fontWeight:400,textTransform:'none'}}>— additive to any drawn shapes; on their own they also satisfy the area requirement (managed on the Geo Codes page)</span></label>
              <GeoCodeChipSelect geoCodes={geoCodes} selectedIds={selectedGeoCodeIds} onChange={setSelectedGeoCodeIds} />
            </div>
          </div>

          <div style={{ marginTop:16, display:'flex', gap:8, justifyContent:'space-between' }}>
            <div style={{ display:'flex', gap:8 }}>
              <input ref={fileInputRef} type="file" accept=".xml,text/xml,application/xml"
                style={{ display:'none' }} onChange={handleImportFile} />
              <button type="button" onClick={triggerImport}>Import Raw CAP</button>
              <button type="button" onClick={exportCAP} disabled={!form.event && !form.sender}>Export Raw CAP</button>
            </div>
            <div style={{ display:'flex', gap:8 }}>
              <button type="button" onClick={resetAll} disabled={submitting}>Reset</button>
              <button type="submit" className="primary" disabled={submitting}>
                {submitting ? 'Submitting…' : 'Submit to Alerts Queue'}
              </button>
            </div>
          </div>
        </div>
      </form>

      {/* Map popup — kept mounted (just hidden) so the Leaflet instance and
          drawn shapes survive between opens. */}
      <div
        className="modal-backdrop"
        style={{ display: mapOpen ? 'flex' : 'none' }}
        onClick={e => e.target === e.currentTarget && setMapOpen(false)}
      >
        <div style={{
          background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6,
          width: '90vw', height: '85vh', maxWidth: 1400,
          display: 'flex', flexDirection: 'column', overflow: 'hidden',
        }}>
          <div style={{
            display:'flex', justifyContent:'space-between', alignItems:'center',
            padding:'12px 16px', borderBottom:'1px solid var(--border)',
          }}>
            <div style={{ fontFamily:'var(--font-ui)', fontWeight:700, letterSpacing:'0.05em', textTransform:'uppercase', color:'var(--accent)' }}>
              Draw Alert Area
            </div>
            <div style={{ display:'flex', gap:8, alignItems:'center' }}>
              <span style={{ fontSize:12, color:'var(--muted)' }}>{shapeCount} shape{shapeCount === 1 ? '' : 's'} drawn</span>
              <button type="button" onClick={clearShapes} disabled={shapeCount === 0}>Clear Shapes</button>
              <button type="button" className="primary" onClick={() => setMapOpen(false)}>Done</button>
            </div>
          </div>
          <div ref={containerRef} style={{ flex:1, width:'100%' }} />
        </div>
      </div>
    </div>
  )
}

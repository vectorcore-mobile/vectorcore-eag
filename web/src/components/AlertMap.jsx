import React, { useEffect, useRef, useState } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

// Severity → polygon colour
function sevColor(severity) {
  switch ((severity || '').toLowerCase()) {
    case 'extreme':  return '#ff4444'
    case 'severe':   return '#ff7c2a'
    case 'moderate': return '#f5d623'
    case 'minor':    return '#3a8fd4'
    default:         return '#5a6a7e'
  }
}

export default function AlertMap({ geometry, severity, areaDesc }) {
  const containerRef = useRef(null)
  const mapRef       = useRef(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (!containerRef.current || !geometry) return

    // Parse geometry if it's a string
    let geomObj
    try {
      geomObj = typeof geometry === 'string' ? JSON.parse(geometry) : geometry
    } catch {
      setError('Invalid geometry data')
      return
    }

    // Wrap bare geometry in a GeoJSON Feature so L.geoJSON accepts it.
    // Feature and FeatureCollection are already valid GeoJSON on their own —
    // only a bare geometry object (Polygon, MultiPolygon, ...) needs wrapping.
    const feature = (geomObj.type === 'Feature' || geomObj.type === 'FeatureCollection') ? geomObj : {
      type: 'Feature',
      geometry: geomObj,
    }

    // Init map once
    if (!mapRef.current) {
      mapRef.current = L.map(containerRef.current, {
        zoomControl: true,
        attributionControl: true,
      })

      // CartoDB dark tiles — matches the ops console aesthetic
      L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/">CARTO</a>',
        subdomains: 'abcd',
        maxZoom: 19,
      }).addTo(mapRef.current)
    }

    const map = mapRef.current
    const color = sevColor(severity)

    const layer = L.geoJSON(feature, {
      style: {
        color,
        weight: 2,
        fillColor: color,
        fillOpacity: 0.2,
        opacity: 0.9,
      },
    }).addTo(map)

    try {
      map.fitBounds(layer.getBounds(), { padding: [16, 16] })
    } catch {
      // geometry may be a point or empty — fall back to US view
      map.setView([38, -96], 4)
    }

    return () => {
      layer.remove()
    }
  }, [geometry, severity])

  // Destroy map on unmount
  useEffect(() => {
    return () => {
      if (mapRef.current) {
        mapRef.current.remove()
        mapRef.current = null
      }
    }
  }, [])

  if (!geometry) return null
  if (error) return <div style={{ color: 'var(--muted)', fontSize: 12 }}>{error}</div>

  return (
    <div style={{ marginTop: 12, border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
      <div style={{
        fontFamily: 'var(--font-ui)', fontSize: 11, fontWeight: 700,
        letterSpacing: '0.08em', textTransform: 'uppercase',
        color: 'var(--muted)', padding: '6px 10px',
        background: 'var(--surface2)', borderBottom: '1px solid var(--border)',
      }}>
        Alert Area {areaDesc && <span style={{ fontWeight: 400, textTransform: 'none' }}>— {areaDesc}</span>}
      </div>
      <div ref={containerRef} style={{ height: 280, width: '100%' }} />
    </div>
  )
}

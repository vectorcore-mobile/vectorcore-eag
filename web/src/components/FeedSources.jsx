import React, { useState, useCallback, useEffect } from 'react'
import { api } from '../api.js'
import { StatusBadge, Toggle, Modal, fmtTime, fmtRel } from './shared.jsx'

const TYPES = ['nws','atom','rss']

const EMPTY_FORM = { name:'', url:'', type:'nws', enabled:true, poll_interval:60, params:{} }

export default function FeedSources() {
  const [sources, setSources] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError]     = useState(null)
  const [modal, setModal]     = useState(null) // null | 'create' | src (edit)
  const [saving, setSaving]   = useState(false)
  const [form, setForm]       = useState(EMPTY_FORM)
  const [paramKey, setParamKey]   = useState('')
  const [paramVal, setParamVal]   = useState('')
  const [polling, setPolling] = useState({}) // id => true

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.getFeeds()
      setSources(res || [])
      setError(null)
    } catch (e) { setError(e.message) }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  const openCreate = () => { setForm(EMPTY_FORM); setModal('create') }
  const openEdit   = (src) => {
    setForm({ name: src.name, url: src.url, type: src.type, enabled: src.enabled,
      poll_interval: src.poll_interval, params: src.params || {} })
    setModal(src)
  }

  const save = async () => {
    setSaving(true)
    try {
      if (modal === 'create') {
        await api.createFeed(form)
      } else {
        await api.updateFeed(modal.id, form)
      }
      setModal(null)
      load()
    } catch (e) { alert('Error: ' + e.message) }
    finally { setSaving(false) }
  }

  const remove = async (src) => {
    if (!window.confirm(`Delete feed "${src.name}"?`)) return
    await api.deleteFeed(src.id).catch(e => alert(e.message))
    load()
  }

  const pollNow = async (src) => {
    setPolling(p => ({ ...p, [src.id]: true }))
    try { await api.pollFeed(src.id) }
    catch (e) { alert('Poll error: ' + e.message) }
    finally {
      setTimeout(() => setPolling(p => { const n = {...p}; delete n[src.id]; return n }), 2000)
    }
  }

  const toggleEnabled = async (src) => {
    await api.updateFeed(src.id, {
      name: src.name,
      url: src.url,
      type: src.type,
      enabled: !src.enabled,
      poll_interval: src.poll_interval,
      params: src.params || {},
    }).catch(e => alert(e.message))
    load()
  }

  const setF = (k, v) => setForm(f => ({ ...f, [k]: v }))
  const addParam = () => {
    if (!paramKey.trim()) return
    setForm(f => ({ ...f, params: { ...f.params, [paramKey.trim()]: paramVal } }))
    setParamKey(''); setParamVal('')
  }
  const removeParam = (k) => setForm(f => { const p = {...f.params}; delete p[k]; return {...f, params:p} })

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Feed Sources</div>
        <div className="page-actions">
          <button onClick={load}>Refresh</button>
          <button className="primary" onClick={openCreate}>+ Add Feed</button>
        </div>
      </div>

      {error && <div className="error-msg">{error}</div>}
      {loading && <div className="loading">Loading…</div>}

      <div className="table-wrap card">
        <table>
          <thead><tr>
            <th>Enabled</th><th>Name</th><th>Type</th><th>URL</th>
            <th>Interval</th><th>Last Polled</th><th>Status</th><th>Alerts</th><th>Actions</th>
          </tr></thead>
          <tbody>
            {sources.map(src => (
              <tr key={src.id}>
                <td><Toggle checked={src.enabled} onChange={() => toggleEnabled(src)} /></td>
                <td style={{ fontFamily:'var(--font-ui)', fontWeight:600 }}>{src.name}</td>
                <td><span className="badge" style={{background:'var(--surface2)',color:'var(--info)',border:'1px solid rgba(58,143,212,0.3)'}}>{src.type}</span></td>
                <td style={{ fontFamily:'var(--font-mono)', fontSize:11, maxWidth:200, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{src.url}</td>
                <td style={{ fontFamily:'var(--font-mono)', fontSize:12 }}>{src.poll_interval}s</td>
                <td style={{ fontFamily:'var(--font-mono)', fontSize:12, whiteSpace:'nowrap' }}>{src.last_polled ? fmtRel(src.last_polled) : '—'}</td>
                <td><StatusBadge status={src.enabled ? (src.last_status || '—') : 'disabled'} /></td>
                <td style={{ fontFamily:'var(--font-mono)' }}>{src.alert_count}</td>
                <td>
                  <div style={{ display:'flex', gap:6 }}>
                    <button onClick={() => pollNow(src)} disabled={polling[src.id]}>{polling[src.id] ? '…' : 'Poll'}</button>
                    <button onClick={() => openEdit(src)}>Edit</button>
                    <button className="danger" onClick={() => remove(src)}>Del</button>
                  </div>
                </td>
              </tr>
            ))}
            {!loading && !sources.length && (
              <tr><td colSpan={9} style={{color:'var(--muted)',textAlign:'center',padding:'20px 0'}}>No feed sources configured.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {modal && (
        <Modal
          title={modal === 'create' ? 'Add Feed Source' : `Edit: ${modal.name}`}
          onClose={() => setModal(null)}
          closeOnBackdrop={false}
          closeOnEscape={false}
          footer={<>
            <button onClick={() => setModal(null)}>Cancel</button>
            <button className="primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </>}
        >
          <div className="form-row">
            <label>Name</label>
            <input value={form.name} onChange={e => setF('name', e.target.value)} placeholder="NWS Active Alerts" />
          </div>
          <div className="form-row">
            <label>URL</label>
            <input value={form.url} onChange={e => setF('url', e.target.value)} placeholder="https://…" />
          </div>
          <div className="form-2col">
            <div className="form-row">
              <label>Type</label>
              <select value={form.type} onChange={e => setF('type', e.target.value)}>
                {TYPES.map(t => <option key={t}>{t}</option>)}
              </select>
            </div>
            <div className="form-row">
              <label>Poll Interval (seconds)</label>
              <input type="number" value={form.poll_interval} min={10}
                onChange={e => setF('poll_interval', parseInt(e.target.value) || 60)} />
            </div>
          </div>
          <div className="form-row">
            <div className="toggle-wrap">
              <Toggle checked={form.enabled} onChange={v => setF('enabled', v)} />
              <span style={{ fontSize:13 }}>Enabled</span>
            </div>
          </div>

          <div className="form-row">
            <label>Query Params</label>
            <div style={{ display:'flex', gap:6, marginBottom:6 }}>
              <input value={paramKey} onChange={e => setParamKey(e.target.value)}
                placeholder="key" style={{ flex:'0 0 120px' }} />
              <input value={paramVal} onChange={e => setParamVal(e.target.value)}
                placeholder="value" style={{ flex:1 }}
                onKeyDown={e => e.key === 'Enter' && (e.preventDefault(), addParam())} />
              <button type="button" onClick={addParam}>+</button>
            </div>
            {Object.entries(form.params).map(([k, v]) => (
              <div key={k} className="chip" style={{ marginBottom:4, display:'inline-flex', marginRight:6 }}>
                <code style={{fontSize:12}}>{k}</code>
                <span style={{color:'var(--muted)', margin:'0 4px'}}>=</span>
                <code style={{fontSize:12}}>{v || '(empty)'}</code>
                <span className="chip-remove" onClick={() => removeParam(k)}>×</span>
              </div>
            ))}
          </div>
        </Modal>
      )}
    </div>
  )
}

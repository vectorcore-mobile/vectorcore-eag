import React, { useState, useCallback, useEffect } from 'react'
import { api } from '../api.js'
import { Toggle, Modal } from './shared.jsx'

const EMPTY_FORM = { type: '', code: '', description: '', enabled: true }

export default function GeoCodes() {
  const [codes, setCodes]     = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError]     = useState(null)
  const [modal, setModal]     = useState(null) // null | 'create' | code (edit)
  const [saving, setSaving]   = useState(false)
  const [form, setForm]       = useState(EMPTY_FORM)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.getGeoCodes()
      setCodes(res || [])
      setError(null)
    } catch (e) { setError(e.message) }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  const openCreate = () => { setForm(EMPTY_FORM); setModal('create') }
  const openEdit   = (c) => { setForm({ type: c.type, code: c.code, description: c.description, enabled: c.enabled }); setModal(c) }

  const save = async () => {
    setSaving(true)
    try {
      if (modal === 'create') await api.createGeoCode(form)
      else await api.updateGeoCode(modal.id, form)
      setModal(null)
      load()
    } catch (e) { alert('Error: ' + e.message) }
    finally { setSaving(false) }
  }

  const remove = async (c) => {
    if (!window.confirm(`Delete ${c.type} code "${c.code}"?`)) return
    await api.deleteGeoCode(c.id).catch(e => alert(e.message))
    load()
  }

  const toggleEnabled = async (c) => {
    await api.updateGeoCode(c.id, { type: c.type, code: c.code, description: c.description, enabled: !c.enabled })
      .catch(e => alert(e.message))
    load()
  }

  const setF = (k, v) => setForm(f => ({ ...f, [k]: v }))

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Geo Codes</div>
        <div className="page-actions">
          <button onClick={load}>Refresh</button>
          <button className="primary" onClick={openCreate}>+ Add Geo Code</button>
        </div>
      </div>

      {error && <div className="error-msg">{error}</div>}
      {loading && <div className="loading">Loading…</div>}

      <div className="table-wrap card">
        <table>
          <thead><tr>
            <th>Enabled</th><th>Type</th><th>Code</th><th>Description</th><th>Actions</th>
          </tr></thead>
          <tbody>
            {codes.map(c => (
              <tr key={c.id}>
                <td><Toggle checked={c.enabled} onChange={() => toggleEnabled(c)} /></td>
                <td style={{ fontFamily:'var(--font-mono)', fontWeight:600 }}>{c.type}</td>
                <td style={{ fontFamily:'var(--font-mono)', fontWeight:600 }}>{c.code}</td>
                <td style={{ fontSize:13 }}>{c.description || <span style={{color:'var(--muted)'}}>—</span>}</td>
                <td>
                  <div style={{ display:'flex', gap:6 }}>
                    <button onClick={() => openEdit(c)}>Edit</button>
                    <button className="danger" onClick={() => remove(c)}>Del</button>
                  </div>
                </td>
              </tr>
            ))}
            {!loading && !codes.length && (
              <tr><td colSpan={5} style={{color:'var(--muted)',textAlign:'center',padding:'20px 0'}}>
                No geo codes configured.
              </td></tr>
            )}
          </tbody>
        </table>
      </div>

      {modal && (
        <Modal
          title={modal === 'create' ? 'Add Geo Code' : `Edit Geo Code: ${modal.type} ${modal.code}`}
          onClose={() => setModal(null)}
          closeOnBackdrop={false}
          closeOnEscape={false}
          footer={<>
            <button onClick={() => setModal(null)}>Cancel</button>
            <button className="primary" onClick={save} disabled={saving || !form.type.trim() || !form.code.trim()}>{saving ? 'Saving…' : 'Save'}</button>
          </>}
        >
          <div className="form-row">
            <label>Type * <span style={{fontWeight:400,textTransform:'none'}}>— coding system, e.g. SAME, UGC</span></label>
            <input value={form.type} onChange={e => setF('type', e.target.value.toUpperCase())}
              placeholder="SAME" style={{ textTransform:'uppercase' }} />
          </div>
          <div className="form-row">
            <label>Code *</label>
            <input value={form.code} onChange={e => setF('code', e.target.value)}
              placeholder="045011" />
          </div>
          <div className="form-row">
            <label>Description</label>
            <input value={form.description} onChange={e => setF('description', e.target.value)}
              placeholder="Jefferson County, AL" />
          </div>
          <div className="form-row">
            <div className="toggle-wrap">
              <Toggle checked={form.enabled} onChange={v => setF('enabled', v)} />
              <span style={{ fontSize:13 }}>Enabled</span>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

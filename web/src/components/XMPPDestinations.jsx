import React, { useState, useCallback, useEffect } from 'react'
import { api } from '../api.js'
import { Toggle, Modal, ChipInput, SelectChipInput, EVENT_TYPES } from './shared.jsx'

const SEVERITIES = ['Extreme','Severe','Moderate','Minor','Unknown']
const STATUSES   = ['Actual','Exercise','System','Test','Draft']

const EMPTY_FORM = {
  name:'', username:'', password:'', enabled:true,
  filter_severity:[], filter_event:[], filter_area:[], filter_status:[],
}

export default function XMPPPeers() {
  const [peers, setPeers]         = useState([])
  const [connected, setConnected] = useState([])
  const [loading, setLoading]     = useState(false)
  const [error, setError]         = useState(null)
  const [modal, setModal]         = useState(null)
  const [saving, setSaving]       = useState(false)
  const [form, setForm]           = useState(EMPTY_FORM)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [p, s] = await Promise.all([api.getXMPP(), api.getStatus()])
      setPeers(p || [])
      setConnected(s?.xmpp_server?.connected || [])
      setError(null)
    } catch (e) { setError(e.message) }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  const connInfo = (peer) => connected.find(c => c.id === peer.id)

  const openCreate = () => { setForm(EMPTY_FORM); setModal('create') }
  const openEdit   = (p) => {
    setForm({
      name: p.name, username: p.username, password: '', enabled: p.enabled,
      filter_severity: p.filter_severity || [],
      filter_event:    p.filter_event    || [],
      filter_area:     p.filter_area     || [],
      filter_status:   p.filter_status   || [],
    })
    setModal(p)
  }

  const save = async () => {
    setSaving(true)
    try {
      const payload = { ...form }
      if (modal !== 'create' && !payload.password) delete payload.password
      if (modal === 'create') await api.createXMPP(payload)
      else await api.updateXMPP(modal.id, payload)
      setModal(null); load()
    } catch (e) { alert('Error: ' + e.message) }
    finally { setSaving(false) }
  }

  const remove = async (p) => {
    if (!window.confirm(`Delete peer "${p.name}"?`)) return
    await api.deleteXMPP(p.id).catch(e => alert(e.message))
    load()
  }

  const setF = (k, v) => setForm(f => ({ ...f, [k]: v }))

  return (
    <div>
      <div className="page-header">
        <div className="page-title">CAP XMPP Peers</div>
        <div className="page-actions">
          <button onClick={load}>Refresh</button>
          <button className="primary" onClick={openCreate}>+ Add Peer</button>
        </div>
      </div>

      {error && <div className="error-msg">{error}</div>}
      {loading && <div className="loading">Loading…</div>}

      <div className="table-wrap card">
        <table>
          <thead><tr>
            <th>Status</th><th>Name</th><th>Username</th>
            <th>Filters</th><th>Enabled</th><th>Actions</th>
          </tr></thead>
          <tbody>
            {peers.map(p => {
              const ci = connInfo(p)
              const online = !!ci
              return (
                <tr key={p.id}>
                  <td>
                    <span style={{
                      display:'inline-block', width:8, height:8, borderRadius:'50%',
                      background: online ? 'var(--ok)' : 'var(--border)',
                      boxShadow: online ? '0 0 5px var(--ok)' : 'none',
                      marginRight: ci ? 6 : 0,
                    }} />
                    {ci && <span style={{fontSize:11,color:'var(--muted)',fontFamily:'var(--font-mono)'}}>{ci.remote_addr}</span>}
                  </td>
                  <td style={{ fontFamily:'var(--font-ui)', fontWeight:600 }}>{p.name}</td>
                  <td style={{ fontFamily:'var(--font-mono)', fontSize:12 }}>{p.username}</td>
                  <td>
                    <div style={{ display:'flex', flexWrap:'wrap', gap:4 }}>
                      {[...p.filter_severity||[], ...p.filter_event||[], ...p.filter_area||[], ...p.filter_status||[]]
                        .slice(0,4).map(v => <span key={v} className="chip">{v}</span>)}
                      {([...p.filter_severity||[], ...p.filter_event||[], ...p.filter_area||[], ...p.filter_status||[]]).length > 4 &&
                        <span style={{fontSize:11,color:'var(--muted)'}}>+more</span>}
                      {!([...p.filter_severity||[], ...p.filter_event||[], ...p.filter_area||[], ...p.filter_status||[]]).length &&
                        <span style={{fontSize:11,color:'var(--muted)'}}>All</span>}
                    </div>
                  </td>
                  <td><Toggle checked={p.enabled} onChange={async (v) => {
                    await api.updateXMPP(p.id, {
                      name: p.name, username: p.username, enabled: v,
                      filter_severity: p.filter_severity, filter_event: p.filter_event,
                      filter_area: p.filter_area, filter_status: p.filter_status,
                    }).catch(e => alert(e.message))
                    load()
                  }} /></td>
                  <td>
                    <div style={{ display:'flex', gap:6 }}>
                      <button onClick={() => openEdit(p)}>Edit</button>
                      <button className="danger" onClick={() => remove(p)}>Del</button>
                    </div>
                  </td>
                </tr>
              )
            })}
            {!loading && !peers.length && (
              <tr><td colSpan={6} style={{color:'var(--muted)',textAlign:'center',padding:'20px 0'}}>No XMPP peers configured.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {modal && (
        <Modal
          title={modal === 'create' ? 'Add XMPP Peer' : `Edit: ${modal.name}`}
          onClose={() => setModal(null)}
          footer={<>
            <button onClick={() => setModal(null)}>Cancel</button>
            <button className="primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </>}
        >
          <div className="form-row">
            <label>Name</label>
            <input value={form.name} onChange={e => setF('name', e.target.value)} placeholder="Ops Room Client" />
          </div>
          <div className="form-2col">
            <div className="form-row">
              <label>Username</label>
              <input value={form.username} onChange={e => setF('username', e.target.value)}
                placeholder="ops-client"
                disabled={modal !== 'create'}
                style={{ opacity: modal !== 'create' ? 0.6 : 1 }} />
            </div>
            <div className="form-row">
              <label>Password</label>
              <input type="password" value={form.password} onChange={e => setF('password', e.target.value)}
                placeholder={modal !== 'create' ? '(unchanged)' : ''} />
            </div>
          </div>
          <div className="form-row">
            <div className="toggle-wrap">
              <Toggle checked={form.enabled} onChange={v => setF('enabled', v)} />
              <span style={{ fontSize:13 }}>Enabled</span>
            </div>
          </div>

          <div style={{ borderTop:'1px solid var(--border)', margin:'16px 0 12px', paddingTop:12 }}>
            <div style={{ fontFamily:'var(--font-ui)', fontSize:12, fontWeight:700, letterSpacing:'0.08em', textTransform:'uppercase', color:'var(--muted)', marginBottom:12 }}>
              Filters — empty = match all
            </div>
            <div className="form-row">
              <label>Severity</label>
              <SelectChipInput values={form.filter_severity} onChange={v => setF('filter_severity', v)} options={SEVERITIES} placeholder="— Add severity —" />
            </div>
            <div className="form-row">
              <label>Event</label>
              <SelectChipInput values={form.filter_event} onChange={v => setF('filter_event', v)} options={EVENT_TYPES} placeholder="— Add event type —" />
            </div>
            <div className="form-row">
              <label>Area <span style={{color:'var(--muted)',fontWeight:400,textTransform:'none',fontSize:11}}>(substring match)</span></label>
              <ChipInput values={form.filter_area} onChange={v => setF('filter_area', v)} placeholder="Alabama, Jefferson County…" />
            </div>
            <div className="form-row">
              <label>Status</label>
              <SelectChipInput values={form.filter_status} onChange={v => setF('filter_status', v)} options={STATUSES} placeholder="— Add status —" />
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

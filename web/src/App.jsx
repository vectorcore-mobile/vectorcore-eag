import React from 'react'
import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import Dashboard from './components/Dashboard.jsx'
import Alerts from './components/Alerts.jsx'
import FeedSources from './components/FeedSources.jsx'
import CBE from './components/CBE.jsx'
import GeoCodes from './components/GeoCodes.jsx'
import XMPPDestinations from './components/XMPPDestinations.jsx'
import System from './components/System.jsx'
import './App.css'

export default function App() {
  return (
    <BrowserRouter>
      <div className="app-layout">
        <nav className="sidebar">
          <div className="sidebar-logo">
            <span className="logo-mark">▲</span>
            <div>
              <div className="logo-title">VectorCore</div>
              <div className="logo-sub">EAG · Emergency Alert Gateway</div>
            </div>
          </div>
          <ul className="nav-links">
            <li><NavLink to="/" end>Dashboard</NavLink></li>
            <li><NavLink to="/alerts">Alerts</NavLink></li>
            <li><NavLink to="/feeds">Feed Sources</NavLink></li>
            <li><NavLink to="/cbe">CBE</NavLink></li>
            <li><NavLink to="/geocodes">Geo Codes</NavLink></li>
            <li><NavLink to="/xmpp">CAP XMPP</NavLink></li>
            <li><NavLink to="/system">System</NavLink></li>
          </ul>
        </nav>
        <main className="main-content">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/alerts" element={<Alerts />} />
            <Route path="/feeds" element={<FeedSources />} />
            <Route path="/cbe" element={<CBE />} />
            <Route path="/geocodes" element={<GeoCodes />} />
            <Route path="/xmpp" element={<XMPPDestinations />} />
            <Route path="/system" element={<System />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}

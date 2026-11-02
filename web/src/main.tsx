import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/games.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Home from './pages/Home'
import Library from './pages/Library'
import GamePage from './pages/GamePage'
import Platforms, { PlatformPage } from './pages/Platforms'
import Collections, { CollectionPage } from './pages/Collections'
import Stats from './pages/Stats'
import Upload from './pages/Upload'
import Trash from './pages/Trash'
import Pair from './pages/Pair'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="games"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Home />} />
        <Route path="/library" element={<Library />} />
        <Route path="/games/:id" element={<GamePage />} />
        <Route path="/platforms" element={<Platforms />} />
        <Route path="/platforms/:slug" element={<PlatformPage />} />
        <Route path="/collections" element={<Collections />} />
        <Route path="/collections/:id" element={<CollectionPage />} />
        <Route path="/stats" element={<Stats />} />
        <Route path="/upload" element={<Upload />} />
        <Route path="/trash" element={<Trash />} />
        {/* RomM apps send people here to approve a device (verification_path) */}
        <Route path="/pair/device" element={<Pair />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)

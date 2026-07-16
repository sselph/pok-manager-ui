import { useEffect, useState } from 'react'
import ServerCard from './ServerCard.tsx'
import LogsModal from './LogsModal.tsx'
import ConfigModal from './ConfigModal.tsx'

interface DashboardProps {
  onLogout: () => void
}

interface Server {
  name: string
  status: string
  health: string
  cpu: string
  mem: string
  session_name: string
  map_name: string
  asa_port: string
  rcon_port: string
}

interface SystemStatus {
  max_map_count: string
  timezone: string
  status: string
}

function Dashboard({ onLogout }: DashboardProps) {
  const [servers, setServers] = useState<Server[]>([])
  const [system, setSystem] = useState<SystemStatus | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState('')

  // Updates State
  interface UpdateState {
    status: string
    message: string
    currentBuild: string
    latestBuild: string
    lastCheck: string
    updateRunning: boolean
    progress: number
  }
  const [updates, setUpdates] = useState<UpdateState | null>(null)
  const [isCheckingUpdates, setIsCheckingUpdates] = useState(false)
  const [showUpdateSettingsModal, setShowUpdateSettingsModal] = useState(false)
  const [updateSettings, setUpdateSettings] = useState({
    autoUpdateEnabled: false,
    checkInterval: 30,
    gracePeriod: 5,
    ignoreTimeOfDay: true,
    allowedWindowStart: '00:00',
    allowedWindowEnd: '23:59'
  })

  // Overlay States
  const [activeLogsInstance, setActiveLogsInstance] = useState<string | null>(null)
  const [activeConfigInstance, setActiveConfigInstance] = useState<string | null>(null)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newInstanceName, setNewInstanceName] = useState('')
  const [isCreating, setIsCreating] = useState(false)

  const [activeDeleteInstance, setActiveDeleteInstance] = useState<string | null>(null)
  const [deleteConfirmationInput, setDeleteConfirmationInput] = useState('')
  const [isDeleting, setIsDeleting] = useState(false)

  const fetchUpdates = async () => {
    try {
      const response = await fetch('/api/updates/status')
      if (response.ok) {
        const data = await response.json()
        setUpdates(data)
      }
    } catch (err) {
      console.error('Failed to fetch update status:', err)
    }
  }

  const fetchDashboardData = async () => {
    try {
      // Fetch system diagnostics
      const sysResponse = await fetch('/api/status')
      if (sysResponse.ok) {
        const sysData = await sysResponse.json()
        setSystem(sysData)
      }

      // Fetch server instances
      const instResponse = await fetch('/api/instances')
      if (!instResponse.ok) {
        const errorData = await instResponse.json().catch(() => ({}))
        throw new Error(errorData.error || 'Failed to load instances')
      }
      const instData = await instResponse.json()
      setServers(instData)

      // Fetch updates status
      fetchUpdates()
    } catch (err: any) {
      setError(err.message || 'Failed to fetch dashboard data')
    } finally {
      setIsLoading(false)
    }
  }

  const fetchUpdateSettings = async () => {
    try {
      const response = await fetch('/api/updates/settings')
      if (response.ok) {
        const data = await response.json()
        setUpdateSettings(data)
      }
    } catch (err) {
      console.error('Failed to fetch update settings:', err)
    }
  }

  const handleSaveUpdateSettings = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const response = await fetch('/api/updates/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updateSettings)
      })
      if (response.ok) {
        const data = await response.json()
        setUpdateSettings(data)
        setShowUpdateSettingsModal(false)
      } else {
        const data = await response.json()
        alert(data.error || 'Failed to save settings')
      }
    } catch (err) {
      console.error('Failed to save update settings:', err)
    }
  }

  // Poll data every 5 seconds
  useEffect(() => {
    fetchDashboardData()
    fetchUpdates()
    fetchUpdateSettings()
    const interval = setInterval(() => {
      fetchDashboardData()
      fetchUpdates()
    }, 5000)

    return () => clearInterval(interval)
  }, [])

  const handleCheckUpdates = async () => {
    setIsCheckingUpdates(true)
    try {
      const response = await fetch('/api/updates/check', { method: 'POST' })
      if (response.ok) {
        const data = await response.json()
        setUpdates(data)
      }
    } catch (err) {
      console.error('Failed to check for updates:', err)
    } finally {
      setIsCheckingUpdates(false)
    }
  }

  const handleTriggerUpdate = async () => {
    if (!window.confirm("Are you sure you want to trigger a central update? This will warn players, save world data, stop running instances, update files, and restart the servers.")) {
      return
    }
    try {
      await fetch('/api/updates/trigger', { method: 'POST' })
      fetchUpdates()
    } catch (err) {
      console.error('Failed to trigger update:', err)
    }
  }

  const handleCreateInstance = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newInstanceName.trim()) return
    setIsCreating(true)
    setError('')

    try {
      const response = await fetch('/api/instances', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newInstanceName }),
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Failed to create instance')
      }

      setNewInstanceName('')
      setShowCreateModal(false)
      fetchDashboardData()
    } catch (err: any) {
      setError(err.message || 'Creation failed')
    } finally {
      setIsCreating(false)
    }
  }

  const handleDeleteInstance = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!activeDeleteInstance || deleteConfirmationInput !== activeDeleteInstance) return
    setIsDeleting(true)
    setError('')

    try {
      const response = await fetch(`/api/instances/${activeDeleteInstance}`, {
        method: 'DELETE',
      })

      if (!response.ok) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.error || 'Failed to delete instance')
      }

      setActiveDeleteInstance(null)
      setDeleteConfirmationInput('')
      fetchDashboardData()
    } catch (err: any) {
      setError(err.message || 'Deletion failed')
    } finally {
      setIsDeleting(false)
    }
  }

  const isMapCountLow = system ? parseInt(system.max_map_count) < 262144 : false

  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: '100vh' }}>
      {/* Top Navbar */}
      <header style={{
        background: 'var(--color-surface)',
        backdropFilter: 'blur(12px)',
        borderBottom: '1px solid var(--color-border)',
        position: 'sticky',
        top: 0,
        zIndex: 100,
      }}>
        <div className="navbar-container">
          {/* Brand */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <div style={{ color: 'var(--color-primary)' }}>
              <svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
              </svg>
            </div>
            <div>
              <h1 style={{ fontSize: '1.25rem', fontWeight: 800 }}>POK Server Manager</h1>
              <span style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', display: 'block', marginTop: '-2px' }}>
                Docker Management Dashboard
              </span>
            </div>
          </div>

          {/* System Metrics */}
          {system && (
            <div style={{ display: 'flex', gap: '1.5rem', fontSize: '0.8rem', flexWrap: 'wrap' }} className="fade-in">
              <div style={{ padding: '0.25rem 0.75rem', background: 'rgba(255,255,255,0.03)', borderRadius: '6px', border: '1px solid var(--color-border)' }}>
                <span style={{ color: 'var(--color-text-muted)' }}>Host Timezone: </span>
                <strong>{system.timezone}</strong>
              </div>
              <div style={{ 
                padding: '0.25rem 0.75rem', 
                background: isMapCountLow ? 'rgba(239,68,68,0.1)' : 'rgba(255,255,255,0.03)', 
                borderRadius: '6px', 
                border: `1px solid ${isMapCountLow ? 'rgba(239,68,68,0.2)' : 'var(--color-border)'}` 
              }}>
                <span style={{ color: isMapCountLow ? 'var(--color-error)' : 'var(--color-text-muted)' }}>vm.max_map_count: </span>
                <strong style={{ color: isMapCountLow ? 'var(--color-error)' : 'inherit' }}>{system.max_map_count}</strong>
                {isMapCountLow && <span style={{ fontSize: '0.7rem', display: 'block', color: 'var(--color-error)' }}>⚠️ Too low for ARK Server!</span>}
              </div>
            </div>
          )}

          {/* Actions */}
          <div style={{ display: 'flex', gap: '0.75rem' }}>
            <button className="btn btn-primary" onClick={() => setShowCreateModal(true)} title="Create Instance" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
                <line x1="12" y1="5" x2="12" y2="19" />
                <line x1="5" y1="12" x2="19" y2="12" />
              </svg>
            </button>
            <button className="btn btn-secondary" onClick={fetchDashboardData} title="Force Refresh" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
              </svg>
            </button>
            <button className="btn btn-danger" onClick={onLogout}>Logout</button>
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="app-container" style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '2rem' }}>
        {/* Toolbar & Alert */}

        {error && (
          <div style={{
            background: 'rgba(239, 68, 68, 0.1)',
            border: '1px solid rgba(239, 68, 68, 0.2)',
            borderRadius: '8px',
            padding: '1rem',
            color: 'var(--color-error)',
            fontSize: '0.9rem',
          }}>
            {error}
          </div>
        )}

        {updates && (
          <div style={{
            background: 'var(--color-bg-card)',
            border: '1px solid var(--color-border)',
            borderRadius: '8px',
            padding: '1.25rem',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: '1rem',
            flexWrap: 'wrap',
            boxShadow: 'var(--shadow-md)',
            marginTop: '-0.5rem',
            marginBottom: '1rem'
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
              <div style={{
                width: '40px',
                height: '40px',
                borderRadius: '8px',
                background: updates.updateRunning ? 'rgba(235,166,42,0.1)' : updates.currentBuild !== updates.latestBuild && updates.latestBuild !== '0' ? 'rgba(239,68,68,0.1)' : 'rgba(16,185,129,0.1)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: updates.updateRunning ? '#eba62a' : updates.currentBuild !== updates.latestBuild && updates.latestBuild !== '0' ? 'var(--color-error)' : 'var(--color-success)'
              }}>
                {updates.updateRunning ? (
                  <svg className="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.2"/>
                    <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor"/>
                  </svg>
                ) : (
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                    <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
                  </svg>
                )}
              </div>
              <div style={{ textAlign: 'left' }}>
                <div style={{ fontWeight: 700, fontSize: '0.95rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <span>SteamCMD Server Files</span>
                  {updates.currentBuild !== updates.latestBuild && updates.latestBuild !== '0' && !updates.updateRunning && (
                    <span style={{
                      background: 'var(--color-error)',
                      color: '#fff',
                      fontSize: '0.7rem',
                      padding: '0.15rem 0.4rem',
                      borderRadius: '4px',
                      fontWeight: 800
                    }}>UPDATE AVAILABLE</span>
                  )}
                </div>
                <div style={{ fontSize: '0.85rem', color: 'var(--color-text-muted)', marginTop: '0.15rem' }}>
                  {updates.updateRunning ? (
                    <span style={{ color: '#eba62a', fontWeight: 600 }}>{updates.message}</span>
                  ) : (
                    <>Current: <strong style={{ color: 'var(--color-text)' }}>{updates.currentBuild}</strong> • Latest: <strong style={{ color: 'var(--color-text)' }}>{updates.latestBuild || 'Checking...'}</strong></>
                  )}
                </div>
              </div>
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
              <button 
                className="btn btn-secondary" 
                onClick={handleCheckUpdates} 
                disabled={isCheckingUpdates || updates.updateRunning}
                style={{ minWidth: '110px' }}
              >
                {isCheckingUpdates ? 'Checking...' : 'Check Steam'}
              </button>
              <button 
                type="button"
                className="btn btn-secondary" 
                onClick={() => setShowUpdateSettingsModal(true)} 
                disabled={updates.updateRunning}
                title="Configure Update Settings"
                style={{ padding: '0.5rem 0.65rem', display: 'inline-flex', alignItems: 'center', justifyContent: 'center' }}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <circle cx="12" cy="12" r="3"/>
                  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
                </svg>
              </button>
              {updates.currentBuild !== updates.latestBuild && updates.latestBuild !== '0' && (
                <button 
                  className="btn btn-primary" 
                  onClick={handleTriggerUpdate} 
                  disabled={updates.updateRunning}
                  style={{ background: 'var(--color-error)', borderColor: 'var(--color-error)' }}
                >
                  Apply Update
                </button>
              )}
            </div>

            {updates.updateRunning && updates.progress > 0 && (
              <div style={{
                width: '100%',
                background: 'rgba(255,255,255,0.05)',
                height: '6px',
                borderRadius: '3px',
                marginTop: '0.75rem',
                overflow: 'hidden',
                border: '1px solid rgba(255,255,255,0.02)'
              }}>
                <div style={{
                  width: `${updates.progress}%`,
                  background: 'linear-gradient(90deg, var(--color-primary), #3b82f6)',
                  height: '100%',
                  borderRadius: '3px',
                  transition: 'width 0.4s ease-out',
                  boxShadow: '0 0 8px var(--color-primary)'
                }} />
              </div>
            )}
          </div>
        )}

        {/* Server Cards Grid */}
        {isLoading && servers.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '4rem', color: 'var(--color-text-muted)' }}>
            <svg className="spin" width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" style={{ marginBottom: '1rem', color: 'var(--color-primary)' }}>
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.2"/>
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor"/>
            </svg>
            <p>Scanning local filesystem for server configurations...</p>
          </div>
        ) : servers.length === 0 ? (
          <div className="card fade-in" style={{
            textAlign: 'center',
            padding: '4rem 2rem',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            gap: '1.5rem',
            maxWidth: '600px',
            margin: '2rem auto'
          }}>
            <div style={{
              display: 'inline-flex',
              padding: '1.25rem',
              borderRadius: '50%',
              background: 'rgba(255,255,255,0.02)',
              border: '1px solid var(--color-border)',
              color: 'var(--color-text-muted)'
            }}>
              <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
                <rect x="2" y="2" width="20" height="8" rx="2" ry="2"/>
                <rect x="2" y="14" width="20" height="8" rx="2" ry="2"/>
                <line x1="6" y1="6" x2="6.01" y2="6"/>
                <line x1="6" y1="18" x2="6.01" y2="18"/>
              </svg>
            </div>
            <div>
              <h3 style={{ fontSize: '1.25rem', fontWeight: 700, marginBottom: '0.5rem' }}>No ARK Servers Configured</h3>
              <p style={{ color: 'var(--color-text-muted)', fontSize: '0.9rem', maxWidth: '350px' }}>
                Get started by creating a new server instance. This will generate a local docker-compose configuration.
              </p>
            </div>
            <button className="btn btn-primary" onClick={() => setShowCreateModal(true)}>
              Initialize First Server
            </button>
          </div>
        ) : (
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))',
            gap: '2rem',
          }}>
            {servers.map((server) => (
              <ServerCard
                key={server.name}
                server={server}
                onActionComplete={fetchDashboardData}
                onOpenLogs={(name) => setActiveLogsInstance(name)}
                onOpenConfig={(name) => setActiveConfigInstance(name)}
                onOpenDelete={(name) => {
                  setActiveDeleteInstance(name)
                  setDeleteConfirmationInput('')
                }}
              />
            ))}
          </div>
        )}
      </main>

      {/* Footer */}
      <footer style={{
        marginTop: 'auto',
        padding: '2rem',
        textAlign: 'center',
        color: 'var(--color-text-muted)',
        fontSize: '0.85rem',
        borderTop: '1px solid var(--color-border)',
        background: 'rgba(0,0,0,0.1)'
      }}>
        POK Ark Survival Ascended Server Manager Portal • Running in Docker
      </footer>

      {/* Overlays / Modals */}
      {activeLogsInstance && (
        <LogsModal
          instanceName={activeLogsInstance}
          onClose={() => setActiveLogsInstance(null)}
        />
      )}

      {activeConfigInstance && (
        <ConfigModal
          instanceName={activeConfigInstance}
          onClose={() => {
            setActiveConfigInstance(null)
            fetchDashboardData()
          }}
        />
      )}

      {showCreateModal && (
        <div className="modal-overlay">
          <form onSubmit={handleCreateInstance} className="modal-content" style={{ maxWidth: '450px' }}>
            <div className="modal-header">
              <h3 style={{ fontSize: '1.15rem' }}>Create Server Instance</h3>
              <button type="button" className="btn btn-secondary btn-icon" onClick={() => setShowCreateModal(false)}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>Instance Name</label>
                <input
                  type="text"
                  className="input"
                  placeholder="e.g. Island_PvP"
                  value={newInstanceName}
                  onChange={(e) => setNewInstanceName(e.target.value)}
                  disabled={isCreating}
                  required
                  autoFocus
                />
                <span style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                  Name can only contain alphanumeric characters, numbers, and underscores. Spaces will be replaced automatically.
                </span>
              </div>
            </div>
            <div className="modal-footer">
              <button type="button" className="btn btn-secondary" onClick={() => setShowCreateModal(false)} disabled={isCreating}>Cancel</button>
              <button type="submit" className="btn btn-primary" disabled={isCreating || !newInstanceName.trim()}>
                {isCreating ? 'Creating configuration...' : 'Create Server'}
              </button>
            </div>
          </form>
        </div>
      )}

      {activeDeleteInstance && (
        <div className="modal-overlay">
          <form onSubmit={handleDeleteInstance} className="modal-content" style={{ maxWidth: '480px' }}>
            <div className="modal-header">
              <h3 style={{ fontSize: '1.15rem', color: 'var(--color-error)' }}>Delete Server Instance</h3>
              <button type="button" className="btn btn-secondary btn-icon" onClick={() => setActiveDeleteInstance(null)}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
              <div style={{
                background: 'rgba(239, 68, 68, 0.1)',
                border: '1px solid rgba(239, 68, 68, 0.2)',
                borderRadius: '10px',
                padding: '1rem',
                color: 'var(--color-text)',
                fontSize: '0.9rem',
                display: 'flex',
                flexDirection: 'column',
                gap: '0.5rem'
              }}>
                <strong style={{ color: 'var(--color-error)' }}>⚠️ WARNING: PERMANENT DATA LOSS</strong>
                <span>
                  This action will stop the running container, delete all configuration files, and permanently wipe all save files for <strong>{activeDeleteInstance}</strong>. This cannot be undone.
                </span>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>
                  Type the instance name <strong style={{ color: 'var(--color-text)' }}>{activeDeleteInstance}</strong> to confirm:
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder={activeDeleteInstance}
                  value={deleteConfirmationInput}
                  onChange={(e) => setDeleteConfirmationInput(e.target.value)}
                  disabled={isDeleting}
                  required
                  autoFocus
                />
              </div>
            </div>
            <div className="modal-footer">
              <button type="button" className="btn btn-secondary" onClick={() => setActiveDeleteInstance(null)} disabled={isDeleting}>Cancel</button>
              <button 
                type="submit" 
                className="btn btn-danger" 
                disabled={isDeleting || deleteConfirmationInput !== activeDeleteInstance}
              >
                {isDeleting ? 'Deleting...' : 'Delete Instance'}
              </button>
            </div>
          </form>
        </div>
      )}

      {showUpdateSettingsModal && (
        <div className="modal-overlay">
          <form onSubmit={handleSaveUpdateSettings} className="modal-content" style={{ maxWidth: '480px' }}>
            <div className="modal-header">
              <h3 style={{ fontSize: '1.15rem' }}>Global Update Settings</h3>
              <button type="button" className="btn btn-secondary btn-icon" onClick={() => setShowUpdateSettingsModal(false)}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
              
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', fontSize: '0.9rem' }}>
                <input
                  type="checkbox"
                  checked={updateSettings.autoUpdateEnabled}
                  onChange={(e) => setUpdateSettings({ ...updateSettings, autoUpdateEnabled: e.target.checked })}
                  style={{ width: '16px', height: '16px' }}
                />
                <strong>Enable Automatic Updates</strong>
              </label>
              
              <div style={{ display: 'flex', gap: '1rem' }}>
                <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                  <label style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>Check Interval (Minutes)</label>
                  <input
                    type="number"
                    className="input"
                    min="1"
                    value={updateSettings.checkInterval}
                    onChange={(e) => setUpdateSettings({ ...updateSettings, checkInterval: parseInt(e.target.value) || 30 })}
                    required
                  />
                </div>
                <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                  <label style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>Warning Notice (Minutes)</label>
                  <input
                    type="number"
                    className="input"
                    min="0"
                    value={updateSettings.gracePeriod}
                    onChange={(e) => setUpdateSettings({ ...updateSettings, gracePeriod: parseInt(e.target.value) || 0 })}
                    required
                  />
                </div>
              </div>

              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', fontSize: '0.9rem' }}>
                <input
                  type="checkbox"
                  checked={updateSettings.ignoreTimeOfDay}
                  onChange={(e) => setUpdateSettings({ ...updateSettings, ignoreTimeOfDay: e.target.checked })}
                  style={{ width: '16px', height: '16px' }}
                />
                <strong>Ignore Time of Day (Update Instantly)</strong>
              </label>

              {!updateSettings.ignoreTimeOfDay && (
                <div style={{
                  background: 'rgba(255,255,255,0.02)',
                  border: '1px solid var(--color-border)',
                  borderRadius: '6px',
                  padding: '1rem',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '1rem'
                }}>
                  <div style={{ fontSize: '0.8rem', fontWeight: 700, color: 'var(--color-text-muted)' }}>Allowed Update Window</div>
                  <div style={{ display: 'flex', gap: '1rem' }}>
                    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                      <label style={{ fontSize: '0.75rem', color: 'var(--color-text-dimmed)' }}>Start Time</label>
                      <input
                        type="time"
                        className="input"
                        value={updateSettings.allowedWindowStart}
                        onChange={(e) => setUpdateSettings({ ...updateSettings, allowedWindowStart: e.target.value })}
                        required
                      />
                    </div>
                    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                      <label style={{ fontSize: '0.75rem', color: 'var(--color-text-dimmed)' }}>End Time</label>
                      <input
                        type="time"
                        className="input"
                        value={updateSettings.allowedWindowEnd}
                        onChange={(e) => setUpdateSettings({ ...updateSettings, allowedWindowEnd: e.target.value })}
                        required
                      />
                    </div>
                  </div>
                </div>
              )}

            </div>
            <div className="modal-footer" style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
              <button type="button" className="btn btn-secondary" onClick={() => setShowUpdateSettingsModal(false)}>
                Cancel
              </button>
              <button type="submit" className="btn btn-primary">
                Save Settings
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}

export default Dashboard

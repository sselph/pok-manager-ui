import { useState } from 'react'

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

interface ServerCardProps {
  server: Server
  onActionComplete: () => void
  onOpenLogs: (name: string) => void
  onOpenConfig: (name: string) => void
  onOpenDelete: (name: string) => void
}

function ServerCard({ server, onActionComplete, onOpenLogs, onOpenConfig, onOpenDelete }: ServerCardProps) {
  const [isActionLoading, setIsActionLoading] = useState(false)

  const handleAction = async (action: 'start' | 'stop' | 'restart' | 'update') => {
    setIsActionLoading(true)
    try {
      const response = await fetch(`/api/instances/${server.name}/action`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      })
      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || `Action ${action} failed`)
      }
      onActionComplete()
    } catch (e: any) {
      alert(e.message || 'Action failed')
    } finally {
      setIsActionLoading(false)
    }
  }

  const isRunning = server.status === 'running'
  const isHealthy = server.health === 'healthy'
  const isTransitioning = server.status === 'restarting' || (isRunning && server.health === 'starting')

  // Icon Render Helpers
  const renderPlayIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <polygon points="5 3 19 12 5 21 5 3" fill="currentColor"/>
    </svg>
  )

  const renderStopIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2" fill="currentColor"/>
    </svg>
  )

  const renderRefreshIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
    </svg>
  )

  const renderUploadIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12"/>
    </svg>
  )

  return (
    <div className="card fade-in" style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <h3 style={{ fontSize: '1.2rem', fontWeight: 700 }}>{server.name}</h3>
          <span style={{ fontSize: '0.85rem', color: 'var(--color-text-dimmed)' }}>
            Map: <strong style={{ color: 'var(--color-text)' }}>{server.map_name || 'Not Configured'}</strong>
          </span>
        </div>

        {/* Status Badge */}
        <span className={`status-badge ${isTransitioning ? 'transitioning' : (isRunning ? 'running' : 'stopped')}`}>
          <span className={`status-indicator ${isTransitioning ? 'transitioning' : (isRunning ? 'running' : 'stopped')} ${isRunning ? 'pulse' : ''}`} />
          {isTransitioning ? 'Starting' : (isRunning ? (isHealthy ? 'Healthy' : 'Running') : 'Offline')}
        </span>
      </div>

      {/* Network Info & Ports */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: '0.75rem',
        background: 'rgba(0,0,0,0.15)',
        padding: '0.75rem 1rem',
        borderRadius: '10px',
        fontSize: '0.85rem',
        border: '1px solid rgba(255,255,255,0.02)'
      }}>
        <div>
          <span style={{ color: 'var(--color-text-muted)', display: 'block', fontSize: '0.75rem' }}>ASA PORT</span>
          <strong style={{ fontFamily: 'var(--font-mono)' }}>{server.asa_port || '7777'}</strong>
        </div>
        <div>
          <span style={{ color: 'var(--color-text-muted)', display: 'block', fontSize: '0.75rem' }}>RCON PORT</span>
          <strong style={{ fontFamily: 'var(--font-mono)' }}>{server.rcon_port || '27020'}</strong>
        </div>
      </div>

      {/* Resource Utilization (only when running) */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', fontSize: '0.85rem', minHeight: '60px' }}>
        {isRunning ? (
          <>
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.25rem' }}>
                <span style={{ color: 'var(--color-text-muted)' }}>CPU Utilization</span>
                <span>{server.cpu}</span>
              </div>
              <div style={{ height: '4px', background: 'rgba(255,255,255,0.05)', borderRadius: '2px', overflow: 'hidden' }}>
                <div style={{ 
                  height: '100%', 
                  background: 'var(--color-primary)', 
                  width: `${parseFloat(server.cpu) || 0}%`,
                  transition: 'width 0.5s ease-in-out'
                }} />
              </div>
            </div>
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <span style={{ color: 'var(--color-text-muted)' }}>Memory (Allocated)</span>
                <span style={{ fontFamily: 'var(--font-mono)' }}>{server.mem}</span>
              </div>
            </div>
          </>
        ) : (
          <div style={{ 
            color: 'var(--color-text-muted)', 
            display: 'flex', 
            alignItems: 'center', 
            justifyContent: 'center', 
            height: '100%',
            fontStyle: 'italic'
          }}>
            Instance offline
          </div>
        )}
      </div>

      {/* Action Buttons */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem', marginTop: 'auto' }}>
        {isRunning ? (
          <button 
            className="btn btn-danger" 
            disabled={isActionLoading} 
            onClick={() => handleAction('stop')}
          >
            {renderStopIcon()} Stop
          </button>
        ) : (
          <button 
            className="btn btn-primary" 
            disabled={isActionLoading} 
            onClick={() => handleAction('start')}
          >
            {renderPlayIcon()} Start
          </button>
        )}

        <button 
          className="btn btn-secondary" 
          disabled={isActionLoading || !isRunning} 
          onClick={() => handleAction('restart')}
        >
          {renderRefreshIcon()} Restart
        </button>

        <button 
          className="btn btn-secondary" 
          disabled={isActionLoading} 
          onClick={() => handleAction('update')}
          title="Pull latest image and restart"
        >
          {renderUploadIcon()} Update
        </button>

        <button 
          className="btn btn-secondary" 
          onClick={() => onOpenLogs(server.name)}
        >
          {/* Logs Icon */}
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
            <polyline points="14 2 14 8 20 8"/>
            <line x1="16" y1="13" x2="8" y2="13"/>
            <line x1="16" y1="17" x2="8" y2="17"/>
            <polyline points="10 9 9 9 8 9"/>
          </svg>
          Logs
        </button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '4fr 1fr', gap: '0.5rem' }}>
        <button 
          className="btn btn-secondary" 
          style={{ width: '100%' }}
          onClick={() => onOpenConfig(server.name)}
        >
          {/* Settings Icon */}
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>
          </svg>
          Configure Settings
        </button>
        <button 
          className="btn btn-danger btn-icon" 
          onClick={() => onOpenDelete(server.name)}
          title="Delete Server Instance"
          disabled={isActionLoading}
        >
          {/* Trash Icon */}
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
            <polyline points="3 6 5 6 21 6"/>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
            <line x1="10" y1="11" x2="10" y2="17"/>
            <line x1="14" y1="11" x2="14" y2="17"/>
          </svg>
        </button>
      </div>
    </div>
  )
}

export default ServerCard

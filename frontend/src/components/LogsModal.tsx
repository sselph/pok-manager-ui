import { useEffect, useRef, useState } from 'react'

interface LogsModalProps {
  instanceName: string
  onClose: () => void
}

function LogsModal({ instanceName, onClose }: LogsModalProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [isConnected, setIsConnected] = useState(true)
  const logsEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // Establish Server-Sent Events stream
    const eventSource = new EventSource(`/api/instances/${instanceName}/logs`)

    eventSource.onmessage = (event) => {
      setLogs((prev) => [...prev.slice(-499), event.data]) // Keep last 500 lines max
    };

    eventSource.onerror = (err) => {
      console.error('SSE connection error:', err)
      setIsConnected(false)
      eventSource.close()
    };

    return () => {
      eventSource.close()
    };
  }, [instanceName])

  // Scroll to bottom on new log line
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  return (
    <div className="modal-overlay">
      <div className="modal-content" style={{ maxWidth: '850px', height: '80vh' }}>
        <div className="modal-header">
          <div>
            <h3 style={{ fontSize: '1.25rem' }}>Container Logs: {instanceName}</h3>
            <span style={{ fontSize: '0.8rem', color: isConnected ? 'var(--color-success)' : 'var(--color-text-muted)' }}>
              {isConnected ? '● Connected (Streaming)' : '○ Disconnected'}
            </span>
          </div>
          <button className="btn btn-secondary btn-icon" onClick={onClose}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        <div className="modal-body" style={{ 
          background: '#040711', 
          flex: 1, 
          padding: '1rem', 
          overflowY: 'auto',
          borderRadius: '8px',
          margin: '1.5rem',
          display: 'flex',
          flexDirection: 'column'
        }}>
          <div style={{ 
            fontFamily: 'var(--font-mono)', 
            fontSize: '0.85rem', 
            lineHeight: '1.6', 
            color: '#a5f3fc',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all'
          }}>
            {logs.length === 0 ? (
              <div style={{ color: 'var(--color-text-muted)', textAlign: 'center', padding: '2rem' }}>
                Fetching logs...
              </div>
            ) : (
              logs.map((log, idx) => (
                <div key={idx} style={{ borderBottom: '1px solid rgba(255,255,255,0.02)', padding: '2px 0' }}>
                  {log}
                </div>
              ))
            )}
            <div ref={logsEndRef} />
          </div>
        </div>

        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={() => setLogs([])}>Clear Screen</button>
          <button className="btn btn-primary" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  )
}

export default LogsModal

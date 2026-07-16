import { useEffect, useState } from 'react'

interface ConfigModalProps {
  instanceName: string
  onClose: () => void
}

interface ConfigData {
  name: string
  image_tag: string
  settings: Record<string, string>
}

function ConfigModal({ instanceName, onClose }: ConfigModalProps) {
  const [config, setConfig] = useState<ConfigData | null>(null)
  const [activeTab, setActiveTab] = useState<'general' | 'connection' | 'game' | 'updates' | 'advanced'>('general')
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    fetchConfig()
  }, [instanceName])

  const fetchConfig = async () => {
    try {
      const response = await fetch(`/api/instances/${instanceName}`)
      if (!response.ok) {
        throw new Error('Failed to load configuration')
      }
      const data = await response.json()
      setConfig(data)
    } catch (err: any) {
      setError(err.message || 'Failed to connect to backend')
    }
  }

  const handleSettingChange = (key: string, value: string) => {
    if (!config) return
    setConfig({
      ...config,
      settings: {
        ...config.settings,
        [key]: value,
      }
    })
  }

  const handleImageTagChange = (value: string) => {
    if (!config) return
    setConfig({
      ...config,
      image_tag: value,
    })
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!config) return
    setIsSaving(true)
    setError('')
    setSuccess('')

    try {
      const response = await fetch(`/api/instances/${instanceName}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          image_tag: config.image_tag,
          settings: config.settings,
        }),
      })

      const data = await response.json()
      if (!response.ok) {
        throw new Error(data.error || 'Failed to save configuration')
      }

      setSuccess('Configuration updated successfully!')
      setTimeout(() => onClose(), 1500)
    } catch (err: any) {
      setError(err.message || 'Failed to save settings')
    } finally {
      setIsSaving(false)
    }
  }

  if (!config && !error) {
    return (
      <div className="modal-overlay">
        <div className="modal-content" style={{ padding: '2rem', textAlign: 'center' }}>
          Loading configuration...
        </div>
      </div>
    )
  }

  const renderInputField = (key: string, label: string, type: 'text' | 'number' | 'boolean' | 'textarea' = 'text') => {
    if (!config) return null
    const val = config.settings[key] || ''

    if (type === 'boolean') {
      const isTrue = val.toUpperCase() === 'TRUE'
      return (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.5rem 0' }}>
          <span style={{ fontSize: '0.95rem', fontWeight: 600 }}>{label}</span>
          <button
            type="button"
            className={`btn ${isTrue ? 'btn-primary' : 'btn-secondary'}`}
            style={{ width: '100px', padding: '0.4rem' }}
            onClick={() => handleSettingChange(key, isTrue ? 'FALSE' : 'TRUE')}
          >
            {isTrue ? 'Enabled' : 'Disabled'}
          </button>
        </div>
      )
    }

    if (type === 'textarea') {
      return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
          <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>{label}</label>
          <textarea
            className="input"
            rows={3}
            value={val}
            onChange={(e) => handleSettingChange(key, e.target.value)}
          />
        </div>
      )
    }

    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
        <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>{label}</label>
        <input
          type={type === 'number' ? 'number' : 'text'}
          className="input"
          value={val}
          onChange={(e) => handleSettingChange(key, e.target.value)}
        />
      </div>
    )
  }

  return (
    <div className="modal-overlay">
      <form onSubmit={handleSave} className="modal-content" style={{ maxWidth: '680px' }}>
        <div className="modal-header">
          <div>
            <h3 style={{ fontSize: '1.25rem' }}>Edit Settings: {instanceName}</h3>
            <span style={{ fontSize: '0.80rem', color: 'var(--color-text-muted)' }}>Configure environment variables & server parameters</span>
          </div>
          <button type="button" className="btn btn-secondary btn-icon" onClick={onClose}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Tab Navigation */}
        <div style={{ 
          display: 'flex', 
          borderBottom: '1px solid var(--color-border)', 
          background: 'rgba(0,0,0,0.1)', 
          overflowX: 'auto',
          padding: '0 0.5rem'
        }}>
          {(['general', 'connection', 'game', 'updates', 'advanced'] as const).map((tab) => (
            <button
              key={tab}
              type="button"
              style={{
                background: 'none',
                border: 'none',
                color: activeTab === tab ? 'var(--color-primary)' : 'var(--color-text-muted)',
                padding: '1rem',
                fontSize: '0.85rem',
                fontWeight: 700,
                cursor: 'pointer',
                borderBottom: activeTab === tab ? '2px solid var(--color-primary)' : '2px solid transparent',
                textTransform: 'capitalize',
                outline: 'none'
              }}
              onClick={() => setActiveTab(tab)}
            >
              {tab}
            </button>
          ))}
        </div>

        <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem', minHeight: '350px' }}>
          {error && (
            <div style={{
              background: 'rgba(239, 68, 68, 0.1)',
              border: '1px solid rgba(239, 68, 68, 0.2)',
              borderRadius: '8px',
              padding: '0.75rem 1rem',
              color: 'var(--color-error)',
              fontSize: '0.85rem',
            }}>
              {error}
            </div>
          )}

          {success && (
            <div style={{
              background: 'rgba(16, 185, 129, 0.1)',
              border: '1px solid rgba(16, 185, 129, 0.2)',
              borderRadius: '8px',
              padding: '0.75rem 1rem',
              color: 'var(--color-success)',
              fontSize: '0.85rem',
            }}>
              {success}
            </div>
          )}

          {config && (
            <>
              {activeTab === 'general' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                  {renderInputField('Session Name', 'Session (Server Name)')}
                  {renderInputField('Map Name', 'Map Name')}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    {renderInputField('Memory Limit', 'Memory Limit (e.g. 16G, 32G)')}
                    {renderInputField('Max Players', 'Max Players', 'number')}
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    {renderInputField('TZ', 'Server Timezone')}
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                      <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>Docker Image Tag</label>
                      <input
                        type="text"
                        className="input"
                        value={config.image_tag}
                        onChange={(e) => handleImageTagChange(e.target.value)}
                      />
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'connection' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    {renderInputField('ASA Port', 'Server Port', 'number')}
                    {renderInputField('RCON Port', 'RCON Port', 'number')}
                  </div>
                  {renderInputField('Server Password', 'Server Join Password (blank if public)')}
                  {renderInputField('Admin Password', 'Admin Commands Password')}
                  {renderInputField('Cluster ID', 'Cross-play Cluster ID')}
                </div>
              )}

              {activeTab === 'game' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                  {renderInputField('BattleEye', 'Enable BattlEye Anti-Cheat', 'boolean')}
                  {renderInputField('API', 'Enable Server API Logs', 'boolean')}
                  {renderInputField('RCON Enabled', 'Enable RCON Console Support', 'boolean')}
                  {renderInputField('Discord Channel ID', 'Discord Channel ID (for chat/command relay)')}
                  {renderInputField('MOTD Enabled', 'Show Message of the Day (MOTD)', 'boolean')}
                  {renderInputField('MOTD', 'MOTD Content', 'textarea')}
                  {renderInputField('MOTD Duration', 'MOTD Display Duration (seconds)', 'number')}
                </div>
              )}

              {activeTab === 'updates' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                  {renderInputField('Update Server', 'Auto Update Server on Start/Interval', 'boolean')}
                  {renderInputField('Update Interval', 'Update Verification Interval (hours)', 'number')}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    {renderInputField('Update Window Start', 'Update Window Start (e.g. 12:00 AM)')}
                    {renderInputField('Update Window End', 'Update Window End (e.g. 11:59 PM)')}
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    {renderInputField('Restart Notice', 'Restart Countdown Warning (minutes)', 'number')}
                    {renderInputField('Save Wait Seconds', 'Post-Save Shut Down Delay (seconds)', 'number')}
                  </div>
                </div>
              )}

              {activeTab === 'advanced' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                  {renderInputField('CPU Optimization', 'Optimize CPU Performance (sets affinities)', 'boolean')}
                  {renderInputField('Random Startup Delay', 'Stagger Boot Up Times', 'boolean')}
                  {renderInputField('Show Admin Commands In Chat', 'Log Admin Commands to Game Chat', 'boolean')}
                  {renderInputField('Mod IDs', 'CurseForge Mod IDs (comma separated)')}
                  {renderInputField('Passive Mods', 'Passive Mod IDs (comma separated)')}
                  {renderInputField('Custom Server Args', 'Raw Command Line Flags (Args)')}
                </div>
              )}
            </>
          )}
        </div>

        <div className="modal-footer">
          <button type="button" className="btn btn-secondary" onClick={onClose} disabled={isSaving}>Cancel</button>
          <button type="submit" className="btn btn-primary" disabled={isSaving}>
            {isSaving ? 'Saving Changes...' : 'Save Configuration'}
          </button>
        </div>
      </form>
    </div>
  )
}

export default ConfigModal

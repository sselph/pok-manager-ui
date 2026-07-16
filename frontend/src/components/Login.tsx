import { useState } from 'react'

interface LoginProps {
  onLoginSuccess: (token: string) => void
}

function Login({ onLoginSuccess }: LoginProps) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setIsLoading(true)
    setError('')

    try {
      const response = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      })

      const data = await response.json()

      if (!response.ok) {
        throw new Error(data.error || 'Login failed')
      }

      onLoginSuccess(data.token)
    } catch (err: any) {
      setError(err.message || 'Server connection failed')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div style={{
      flex: 1,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      padding: '1rem',
      position: 'relative'
    }}>
      <form 
        onSubmit={handleSubmit}
        className="card fade-in"
        style={{
          width: '100%',
          maxWidth: '400px',
          padding: '2.5rem 2rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '1.5rem',
        }}
      >
        <div style={{ textAlign: 'center', marginBottom: '0.5rem' }}>
          {/* Logo SVG */}
          <div style={{
            display: 'inline-flex',
            padding: '1rem',
            borderRadius: '50%',
            background: 'var(--color-primary-glow)',
            color: 'var(--color-primary)',
            marginBottom: '1rem',
          }}>
            <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
            </svg>
          </div>
          <h2 style={{ fontSize: '1.75rem', fontWeight: 800, marginBottom: '0.25rem' }}>
            POK Manager
          </h2>
          <p style={{ color: 'var(--color-text-muted)', fontSize: '0.9rem' }}>
            ARK: Survival Ascended Server Portal
          </p>
        </div>

        {error && (
          <div style={{
            background: 'rgba(239, 68, 68, 0.1)',
            border: '1px solid rgba(239, 68, 68, 0.2)',
            borderRadius: '8px',
            padding: '0.75rem 1rem',
            color: 'var(--color-error)',
            fontSize: '0.85rem',
            textAlign: 'center',
          }}>
            {error}
          </div>
        )}

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-dimmed)' }}>
            Admin Password
          </label>
          <input
            type="password"
            className="input"
            placeholder="••••••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={isLoading}
            required
            autoFocus
          />
        </div>

        <button 
          type="submit" 
          className="btn btn-primary"
          style={{ width: '100%', padding: '0.875rem' }}
          disabled={isLoading}
        >
          {isLoading ? (
            <svg className="spin" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.2"/>
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor"/>
            </svg>
          ) : 'Authenticate'}
        </button>
      </form>
    </div>
  )
}

export default Login

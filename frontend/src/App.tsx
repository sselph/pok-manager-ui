import { useState, useEffect } from 'react'
import Login from './components/Login.tsx'
import Dashboard from './components/Dashboard.tsx'

function App() {
  const [token, setToken] = useState<string | null>(localStorage.getItem('pok_token'))

  useEffect(() => {
    if (token) {
      localStorage.setItem('pok_token', token)
    } else {
      localStorage.removeItem('pok_token')
    }
  }, [token])

  const handleLoginSuccess = (newToken: string) => {
    setToken(newToken)
  }

  const handleLogout = async () => {
    try {
      await fetch('/api/logout', { method: 'POST' })
    } catch (e) {
      console.error('Logout request failed', e)
    }
    setToken(null)
  }

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      {token ? (
        <Dashboard onLogout={handleLogout} />
      ) : (
        <Login onLoginSuccess={handleLoginSuccess} />
      )}
    </div>
  )
}

export default App

import { Routes, Route } from 'react-router-dom'
import { AppLayout } from './components/layout'
import Dashboard from './pages/Dashboard'
import Repositories from './pages/Repositories'
import Containers from './pages/Containers'
import ContainerDetail from './pages/ContainerDetail'
import Volumes from './pages/Volumes'
import Compose from './pages/Compose'
import Network from './pages/Network'
import Settings from './pages/Settings'

function App() {
  return (
    <Routes>
      <Route path="/" element={<AppLayout />}>
        <Route index element={<Dashboard />} />
        <Route path="repos" element={<Repositories />} />
        <Route path="containers" element={<Containers />} />
        <Route path="containers/:id" element={<ContainerDetail />} />
        <Route path="volumes" element={<Volumes />} />
        <Route path="compose" element={<Compose />} />
        <Route path="network" element={<Network />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}

export default App

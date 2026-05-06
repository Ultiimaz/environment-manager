import { Routes, Route } from 'react-router-dom'
import { AppLayout } from './components/layout'
import Home from './pages/Home'
import Projects from './pages/Projects'
import ProjectDetail from './pages/ProjectDetail'
import EnvDetail from './pages/EnvDetail'
import Builds from './pages/Builds'
import Services from './pages/Services'
import ServiceDetail from './pages/ServiceDetail'
import Topology from './pages/Topology'
import Settings from './pages/Settings'

function App() {
  return (
    <Routes>
      <Route path="/" element={<AppLayout />}>
        <Route index element={<Home />} />
        <Route path="projects" element={<Projects />} />
        <Route path="projects/:id" element={<ProjectDetail />} />
        <Route path="projects/:pid/envs/:envId" element={<EnvDetail />} />
        <Route path="builds" element={<Builds />} />
        <Route path="services" element={<Services />} />
        <Route path="services/:name" element={<ServiceDetail />} />
        <Route path="topology" element={<Topology />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}

export default App

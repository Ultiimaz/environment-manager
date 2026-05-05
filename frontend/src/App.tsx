import { Routes, Route } from 'react-router-dom'
import { AppLayout } from './components/layout'
import Home from './pages/Home'
import Projects from './pages/Projects'
import ProjectDetail from './pages/ProjectDetail'
import Builds from './pages/Builds'
import Services from './pages/Services'
import Settings from './pages/Settings'

function App() {
  return (
    <Routes>
      <Route path="/" element={<AppLayout />}>
        <Route index element={<Home />} />
        <Route path="projects" element={<Projects />} />
        <Route path="projects/:id" element={<ProjectDetail />} />
        <Route path="builds" element={<Builds />} />
        <Route path="services" element={<Services />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}

export default App

package stats

import (
	"sync"
	"time"

	"github.com/environment-manager/backend/internal/models"
)

// Store holds historical stats for containers in memory
type Store struct {
	mu        sync.RWMutex
	history   map[string]*models.StatsHistory
	maxAge    time.Duration
	maxPoints int
}

// NewStore creates a new stats store
func NewStore(maxAge time.Duration, maxPoints int) *Store {
	if maxAge == 0 {
		maxAge = 1 * time.Hour // Default: keep 1 hour of history
	}
	if maxPoints == 0 {
		maxPoints = 720 // Default: 720 points (1 hour at 5s intervals)
	}

	return &Store{
		history:   make(map[string]*models.StatsHistory),
		maxAge:    maxAge,
		maxPoints: maxPoints,
	}
}

// Add adds a new stats entry for a container
func (s *Store) Add(stats *models.ContainerStats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h, ok := s.history[stats.ContainerID]
	if !ok {
		h = &models.StatsHistory{
			ContainerID: stats.ContainerID,
			Stats:       make([]models.ContainerStats, 0, s.maxPoints),
			MaxEntries:  s.maxPoints,
		}
		s.history[stats.ContainerID] = h
	}

	h.Stats = append(h.Stats, *stats)

	// Trim old entries by time
	cutoff := time.Now().Add(-s.maxAge)
	newStats := h.Stats[:0]
	for _, stat := range h.Stats {
		if stat.Timestamp.After(cutoff) {
			newStats = append(newStats, stat)
		}
	}

	// Also limit by count
	if len(newStats) > s.maxPoints {
		newStats = newStats[len(newStats)-s.maxPoints:]
	}

	h.Stats = newStats
}

// GetHistory returns historical stats for a container
func (s *Store) GetHistory(containerID string, since time.Time, limit int) []models.ContainerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h, ok := s.history[containerID]
	if !ok {
		return nil
	}

	var result []models.ContainerStats
	for _, stat := range h.Stats {
		if since.IsZero() || stat.Timestamp.After(since) {
			result = append(result, stat)
		}
	}

	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result
}

// GetLatest returns the most recent stats for a container
func (s *Store) GetLatest(containerID string) *models.ContainerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h, ok := s.history[containerID]
	if !ok || len(h.Stats) == 0 {
		return nil
	}

	return &h.Stats[len(h.Stats)-1]
}

// GetAllLatest returns the most recent stats for all containers
func (s *Store) GetAllLatest() []*models.ContainerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.ContainerStats
	for _, h := range s.history {
		if len(h.Stats) > 0 {
			latest := h.Stats[len(h.Stats)-1]
			result = append(result, &latest)
		}
	}

	return result
}

// Remove removes stats history for a container
func (s *Store) Remove(containerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.history, containerID)
}

// Clear removes all stats history
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = make(map[string]*models.StatsHistory)
}

// ContainerIDs returns all container IDs with stats
func (s *Store) ContainerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.history))
	for id := range s.history {
		ids = append(ids, id)
	}

	return ids
}

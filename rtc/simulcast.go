package rtc

import (
	"sync"
)

// Layer represents a simulcast quality level.
type Layer int

const (
	LayerLow Layer = iota
	LayerMid
	LayerHigh
)

func (l Layer) String() string {
	switch l {
	case LayerLow:
		return "low"
	case LayerMid:
		return "mid"
	case LayerHigh:
		return "high"
	default:
		return "unknown"
	}
}

// SimulcastTrack wraps track metadata to manage quality layers.
type SimulcastTrack struct {
	mu sync.RWMutex

	ID       string
	ClientID string

	// Bitrates for each layer (in bps)
	Bitrates map[Layer]uint32
	// Active status for each layer
	Active map[Layer]bool

	// Bitrate estimator for bandwidth adaptation
	estimator *BitrateEstimator
}

// NewSimulcastTrack creates a new tracker for a simulcast track.
func NewSimulcastTrack(id, clientID string) *SimulcastTrack {
	return &SimulcastTrack{
		ID:        id,
		ClientID:  clientID,
		Bitrates:  make(map[Layer]uint32),
		Active:    make(map[Layer]bool),
		estimator: NewBitrateEstimator(),
	}
}

// UpdateLayer updates the state of a specific layer.
func (t *SimulcastTrack) UpdateLayer(l Layer, active bool, bitrate uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Active[l] = active
	t.Bitrates[l] = bitrate
}

// SelectLayer chooses the best active layer within the given bandwidth limit.
func (t *SimulcastTrack) SelectLayer(bandwidth uint32) Layer {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Try High -> Mid -> Low
	if active, ok := t.Active[LayerHigh]; ok && active && t.Bitrates[LayerHigh] <= bandwidth {
		return LayerHigh
	}
	if active, ok := t.Active[LayerMid]; ok && active && t.Bitrates[LayerMid] <= bandwidth {
		return LayerMid
	}
	return LayerLow // Fallback to Low
}

// GetBitrateEstimator returns the bitrate estimator for this track.
func (t *SimulcastTrack) GetBitrateEstimator() *BitrateEstimator {
	return t.estimator
}

// BitrateEstimator estimates available bandwidth using exponential moving average.
type BitrateEstimator struct {
	mu sync.RWMutex

	// Estimated bitrate in bps
	estimatedBitrate uint32

	// History of bitrate measurements
	history    []uint32
	maxHistory int

	// Minimum and maximum observed bitrates
	minBitrate uint32
	maxBitrate uint32
}

// NewBitrateEstimator creates a new bitrate estimator.
func NewBitrateEstimator() *BitrateEstimator {
	return &BitrateEstimator{
		history:    make([]uint32, 0, 10),
		maxHistory: 10,
		minBitrate: 100000,   // 100 kbps minimum
		maxBitrate: 10000000, // 10 Mbps maximum
	}
}

// Feed adds a bitrate measurement.
func (be *BitrateEstimator) Feed(subscriberID string, bitrate float32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	b := uint32(bitrate)

	// Add to history
	be.history = append(be.history, b)
	if len(be.history) > be.maxHistory {
		be.history = be.history[1:]
	}

	// Calculate estimated bitrate using exponential moving average (alpha = 0.3)
	if len(be.history) == 1 {
		be.estimatedBitrate = b
	} else {
		be.estimatedBitrate = uint32(0.3*float64(b) + 0.7*float64(be.estimatedBitrate))
	}

	// Update min/max
	if b < be.minBitrate {
		be.minBitrate = b
	}
	if b > be.maxBitrate {
		be.maxBitrate = b
	}
}

// GetEstimatedBitrate returns the current estimated bitrate.
func (be *BitrateEstimator) GetEstimatedBitrate() uint32 {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.estimatedBitrate
}

// Min returns the minimum bitrate across all feeds.
func (be *BitrateEstimator) Min() float32 {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return float32(be.minBitrate)
}

// Empty returns true if no bitrates have been recorded.
func (be *BitrateEstimator) Empty() bool {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return len(be.history) == 0
}

// SimulcastManager coordinates layer selection across all subscribers in a room.
type SimulcastManager struct {
	mu sync.RWMutex

	tracks      map[string]*SimulcastTrack
	preferences map[string]map[string]Layer // subscriberID -> trackID -> preferred layer
	bandwidths  map[string]uint32           // subscriberID -> max bandwidth
}

// NewSimulcastManager creates a new manager for a room's simulcast tracks.
func NewSimulcastManager() *SimulcastManager {
	return &SimulcastManager{
		tracks:      make(map[string]*SimulcastTrack),
		preferences: make(map[string]map[string]Layer),
		bandwidths:  make(map[string]uint32),
	}
}

// AddTrack registers a new simulcast track.
func (m *SimulcastManager) AddTrack(id, clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks[id] = NewSimulcastTrack(id, clientID)
}

// SetBandwidth updates the bandwidth limit for a subscriber (from CongestionController).
func (m *SimulcastManager) SetBandwidth(subscriberID string, bps uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bandwidths[subscriberID] = bps
}

// SelectOptimalLayer returns the best layer for a subscriber to view a track.
func (m *SimulcastManager) SelectOptimalLayer(subscriberID, trackID string) Layer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	track, ok := m.tracks[trackID]
	if !ok {
		return LayerHigh // Default
	}

	bw := m.bandwidths[subscriberID]
	if bw == 0 {
		bw = 5000000 // Default 5 Mbps
	}

	// Respect manual preference if set
	if prefs, ok := m.preferences[subscriberID]; ok {
		if pref, ok := prefs[trackID]; ok {
			if active := track.Active[pref]; active && track.Bitrates[pref] <= bw {
				return pref
			}
		}
	}

	return track.SelectLayer(bw)
}

// SetPreference allows a user to manually pin a specific quality.
func (m *SimulcastManager) SetPreference(subscriberID, trackID string, l Layer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.preferences[subscriberID]; !ok {
		m.preferences[subscriberID] = make(map[string]Layer)
	}
	m.preferences[subscriberID][trackID] = l
}

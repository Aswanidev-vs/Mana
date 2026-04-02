package rtc

import (
	"log"
	"sync"
	"time"
)

// CongestionState represents the bandwidth estimation state.
type CongestionState int

const (
	StateNormal CongestionState = iota
	StateWarning
	StateCongested
	StateRecovering
)

func (s CongestionState) String() string {
	switch s {
	case StateNormal:
		return "normal"
	case StateWarning:
		return "warning"
	case StateCongested:
		return "congested"
	case StateRecovering:
		return "recovering"
	default:
		return "unknown"
	}
}

// CongestionController manages bandwidth estimation and bitrate adaptation per subscriber.
type CongestionController struct {
	mu sync.RWMutex

	// Config
	minBitrate   uint32
	maxBitrate   uint32
	startBitrate uint32

	// States: subscriberID -> state
	subscribers map[string]*SubscriberStats

	onBitrateChange func(subscriberID string, bitrate uint32)

	// Control loop
	ticker *time.Ticker
	done   chan struct{}
}

// SubscriberStats tracks the network metrics and estimated bandwidth for a single peer.
type SubscriberStats struct {
	ID               string
	State            CongestionState
	EstimatedBitrate uint32
	RTT              uint32
	LossRate         float64 // 0.0 - 1.0
	LastUpdate       time.Time
}

// NewCongestionController creates a new controller for managing subscriber bandwidth.
func NewCongestionController(min, max, start uint32, onChange func(string, uint32)) *CongestionController {
	c := &CongestionController{
		minBitrate:      min,
		maxBitrate:      max,
		startBitrate:    start,
		subscribers:     make(map[string]*SubscriberStats),
		onBitrateChange: onChange,
		ticker:          time.NewTicker(1 * time.Second),
		done:            make(chan struct{}),
	}

	go c.controlLoop()

	return c
}

// controlLoop runs periodic congestion control updates.
func (c *CongestionController) controlLoop() {
	for {
		select {
		case <-c.ticker.C:
			c.updateAll()
		case <-c.done:
			return
		}
	}
}

// updateAll re-evaluates bitrate for all subscribers based on current state.
func (c *CongestionController) updateAll() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for id, s := range c.subscribers {
		oldBitrate := s.EstimatedBitrate
		switch s.State {
		case StateNormal:
			s.EstimatedBitrate = uint32(float64(s.EstimatedBitrate) * 1.08) // 8% increase
		case StateCongested:
			s.EstimatedBitrate = uint32(float64(s.EstimatedBitrate) * 0.75) // 25% decrease
		case StateRecovering:
			s.EstimatedBitrate = uint32(float64(s.EstimatedBitrate) * 1.02) // 2% cautious increase
			// After 5s in recovering, transition back to normal
			if time.Since(s.LastUpdate) > 5*time.Second {
				s.State = StateNormal
			}
		case StateWarning:
			// Hold steady
		}

		// Clamp
		if s.EstimatedBitrate < c.minBitrate {
			s.EstimatedBitrate = c.minBitrate
		}
		if s.EstimatedBitrate > c.maxBitrate {
			s.EstimatedBitrate = c.maxBitrate
		}

		s.LastUpdate = time.Now()

		if s.EstimatedBitrate != oldBitrate && c.onBitrateChange != nil {
			log.Printf("Congestion: Subscriber %s estimated bitrate: %d bps (loss: %.1f%%, rtt: %dms)",
				id, s.EstimatedBitrate, s.LossRate*100, s.RTT)
			c.onBitrateChange(id, s.EstimatedBitrate)
		}
	}
}

// AddSubscriber initializes congestion tracking for a new peer.
func (c *CongestionController) AddSubscriber(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.subscribers[id] = &SubscriberStats{
		ID:               id,
		State:            StateNormal,
		EstimatedBitrate: c.startBitrate,
		LastUpdate:       time.Now(),
	}
}

// RemoveSubscriber cleans up tracking for a peer.
func (c *CongestionController) RemoveSubscriber(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subscribers, id)
}

// UpdateMetrics updates RTT and loss rate and re-estimates the target bitrate.
func (c *CongestionController) UpdateMetrics(id string, rtt uint32, loss float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.subscribers[id]
	if !ok {
		return
	}

	s.RTT = rtt
	s.LossRate = loss

	// State Transition Logic
	if rtt > 300 || loss > 0.10 {
		s.State = StateCongested
	} else if rtt > 150 || loss > 0.02 {
		if s.State == StateNormal {
			s.State = StateWarning
		}
	} else if s.State == StateCongested || s.State == StateWarning {
		s.State = StateRecovering
	}
}

// UpdateBitrateEstimate uses an explicit REMB/TWCC estimate from the client.
func (c *CongestionController) UpdateBitrateEstimate(id string, bitrate uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.subscribers[id]
	if !ok {
		return
	}

	if bitrate < s.EstimatedBitrate {
		s.State = StateWarning
	}

	s.EstimatedBitrate = bitrate
	s.LastUpdate = time.Now()

	if c.onBitrateChange != nil {
		c.onBitrateChange(id, s.EstimatedBitrate)
	}
}

// Close stops the congestion control loop.
func (c *CongestionController) Close() {
	close(c.done)
	c.ticker.Stop()
}

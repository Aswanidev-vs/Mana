package rtc

import (
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

const (
	// DefaultBufferSize is the default size of the jitter buffer
	DefaultBufferSize = 1024
	// MaxNackPackets is the maximum number of packets to request in a single NACK
	MaxNackPackets = 17
	// NackThreshold is the number of missing packets before sending NACK
	NackThreshold = 2
)

// JitterBuffer manages a ring buffer of RTP packets to detect packet loss and support retransmissions.
// Based on the Remainwith jitter buffer pattern with per-SSRC management.
type JitterBuffer struct {
	mu sync.RWMutex

	// packets stores RTP packets indexed by sequence number
	packets map[uint16]*rtp.Packet

	// sequence number tracking
	lastSequenceNumber uint16
	initialized        bool

	// NACK tracking to avoid sending duplicate NACKs
	missingPackets map[uint16]time.Time
	nackCount      int

	// buffer configuration
	size int

	// metrics
	packetsReceived uint64
	packetsLost     uint64
}

// NewJitterBuffer creates a new jitter buffer of the specified size.
func NewJitterBuffer(size uint16) *JitterBuffer {
	if size == 0 {
		size = DefaultBufferSize
	}
	return &JitterBuffer{
		packets:        make(map[uint16]*rtp.Packet),
		missingPackets: make(map[uint16]time.Time),
		size:           int(size),
	}
}

// Push adds a packet to the buffer and returns missing sequence numbers that should trigger NACKs.
func (jb *JitterBuffer) Push(packet *rtp.Packet) []uint16 {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.packetsReceived++
	sn := packet.SequenceNumber

	if !jb.initialized {
		jb.lastSequenceNumber = sn
		jb.initialized = true
		jb.packets[sn] = packet
		return nil
	}

	// Calculate sequence number difference (handles wraparound)
	diff := sn - jb.lastSequenceNumber

	// Handle out-of-order packets (diff is large due to unsigned wraparound)
	if diff > 0x8000 {
		// Late packet - add it if we have space
		if len(jb.packets) < jb.size {
			jb.packets[sn] = packet
			delete(jb.missingPackets, sn)
		}
		return nil
	}

	// Check for missing packets
	if diff > 1 {
		for i := jb.lastSequenceNumber + 1; i != sn; i++ {
			if _, ok := jb.packets[i]; !ok {
				jb.missingPackets[i] = time.Now()
				jb.packetsLost++
			}
		}
	}

	// Add current packet
	jb.packets[sn] = packet
	jb.lastSequenceNumber = sn

	// Clean up old packets
	jb.cleanup()

	// Check if we should generate NACKs
	return jb.generateNACKs()
}

// GetPacket retrieves a packet from the buffer by sequence number.
func (jb *JitterBuffer) GetPacket(sn uint16) *rtp.Packet {
	jb.mu.RLock()
	defer jb.mu.RUnlock()

	p := jb.packets[sn]
	if p != nil && p.SequenceNumber == sn {
		return p
	}
	return nil
}

// generateNACKs returns sequence numbers that should be NACKed.
func (jb *JitterBuffer) generateNACKs() []uint16 {
	if len(jb.missingPackets) < NackThreshold {
		return nil
	}

	now := time.Now()
	var nackSNs []uint16

	for sn, lastNack := range jb.missingPackets {
		// Only NACK if it's been at least 10ms since last NACK
		if now.Sub(lastNack) > 10*time.Millisecond {
			nackSNs = append(nackSNs, sn)
			jb.missingPackets[sn] = now
		}
		if len(nackSNs) >= MaxNackPackets {
			break
		}
	}

	return nackSNs
}

// cleanup removes old packets from the buffer.
func (jb *JitterBuffer) cleanup() {
	if len(jb.packets) < jb.size {
		return
	}

	threshold := jb.lastSequenceNumber - uint16(jb.size)
	for sn := range jb.packets {
		if sn < threshold {
			delete(jb.packets, sn)
			delete(jb.missingPackets, sn)
		}
	}
}

// Stats returns buffer statistics.
func (jb *JitterBuffer) Stats() (received, lost uint64) {
	jb.mu.RLock()
	defer jb.mu.RUnlock()
	return jb.packetsReceived, jb.packetsLost
}

// CreateNack builds an RTCP NACK packet for the missing sequence numbers.
func CreateNack(ssrc uint32, missing []uint16) []rtcp.Packet {
	if len(missing) == 0 {
		return nil
	}

	nack := &rtcp.TransportLayerNack{
		MediaSSRC: ssrc,
		Nacks:     []rtcp.NackPair{},
	}

	// Group missing SNs into NackPairs
	for i := 0; i < len(missing); {
		base := missing[i]
		var mask uint16
		i++
		for i < len(missing) && missing[i]-base <= 16 {
			mask |= (1 << (missing[i] - base - 1))
			i++
		}
		nack.Nacks = append(nack.Nacks, rtcp.NackPair{PacketID: base, LostPackets: rtcp.PacketBitmap(mask)})
	}

	return []rtcp.Packet{nack}
}

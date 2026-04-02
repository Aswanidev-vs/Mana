package rtc

import (
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// UpTrack represents a media track published by a peer.
type UpTrack struct {
	ID     string
	PeerID string
	Kind   webrtc.RTPCodecType
	Remote *webrtc.TrackRemote
	SSRC   webrtc.SSRC
	Done   chan struct{}
	OnNACK func(nack []uint16)
}

// DownTrack represents a media track being sent to a subscriber.
type DownTrack struct {
	ID     string
	PeerID string // ID of the subscriber
	Local  *webrtc.TrackLocalStaticRTP
	Sender *webrtc.RTPSender
}

// Router manages the distribution of media tracks within a room.
// It implements a PubSub pattern where Publishers (UpTracks) are linked to Subscribers (DownTracks).
type Router struct {
	mu sync.RWMutex

	roomID string

	// upTracks: trackID -> UpTrack
	upTracks map[string]*UpTrack

	// subscriptions: trackID -> subscriberID -> DownTrack
	subscriptions map[string]map[string]*DownTrack

	// stopCh closes all forwarding loops
	stopCh chan struct{}
}

// NewRouter creates a new media router for a room.
func NewRouter(roomID string) *Router {
	return &Router{
		roomID:        roomID,
		upTracks:      make(map[string]*UpTrack),
		subscriptions: make(map[string]map[string]*DownTrack),
		stopCh:        make(chan struct{}),
	}
}

// AddUpTrack registers a new publisher track and starts its forwarding loop
// with jitter buffer integration for packet loss detection.
func (r *Router) AddUpTrack(peerID string, track *webrtc.TrackRemote, onNack func(missing []uint16)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trackID := track.ID()
	log.Printf("Router[%s]: Adding UpTrack %s from peer %s", r.roomID, trackID, peerID)

	ut := &UpTrack{
		ID:     trackID,
		PeerID: peerID,
		Kind:   track.Kind(),
		Remote: track,
		SSRC:   track.SSRC(),
		Done:   make(chan struct{}),
		OnNACK: onNack,
	}

	r.upTracks[trackID] = ut

	// Start forwarding loop with jitter buffer for NACK generation
	go r.forwardLoop(ut)
}

// RemoveUpTrack stops forwarding and removes a publisher track.
func (r *Router) RemoveUpTrack(trackID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ut, ok := r.upTracks[trackID]; ok {
		close(ut.Done)
		delete(r.upTracks, trackID)
		delete(r.subscriptions, trackID)
		log.Printf("Router[%s]: Removed UpTrack %s", r.roomID, trackID)
	}
}

// AddSubscriber links a peer to a published track.
func (r *Router) AddSubscriber(trackID, subscriberID string, local *webrtc.TrackLocalStaticRTP, sender *webrtc.RTPSender) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.upTracks[trackID]; !ok {
		return fmt.Errorf("track %s not found", trackID)
	}

	if r.subscriptions[trackID] == nil {
		r.subscriptions[trackID] = make(map[string]*DownTrack)
	}

	r.subscriptions[trackID][subscriberID] = &DownTrack{
		ID:     trackID,
		PeerID: subscriberID,
		Local:  local,
		Sender: sender,
	}

	log.Printf("Router[%s]: Peer %s subscribed to track %s", r.roomID, subscriberID, trackID)
	return nil
}

// RemoveSubscriber removes a peer's subscription to a track.
func (r *Router) RemoveSubscriber(trackID, subscriberID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if subs, ok := r.subscriptions[trackID]; ok {
		delete(subs, subscriberID)
	}
}

// forwardLoop reads RTP packets from an UpTrack and fans them out to Subscribers.
// Uses a jitter buffer to detect packet loss and generate NACKs.
func (r *Router) forwardLoop(ut *UpTrack) {
	log.Printf("Router[%s]: Starting forward loop for track %s", r.roomID, ut.ID)

	// Create a jitter buffer for this track
	jb := NewJitterBuffer(512)

	for {
		select {
		case <-ut.Done:
			return
		case <-r.stopCh:
			return
		default:
			packet, _, err := ut.Remote.ReadRTP()
			if err != nil {
				if err != io.EOF {
					log.Printf("Router[%s]: Read error on track %s: %v", r.roomID, ut.ID, err)
				}
				return
			}

			// Push to jitter buffer to detect missing packets
			missing := jb.Push(packet)
			if len(missing) > 0 && ut.OnNACK != nil {
				go ut.OnNACK(missing)
			}

			r.fanOut(ut.ID, packet)
		}
	}
}

// fanOut distributes a packet to all subscribers of a track.
// Uses packet cloning to prevent memory races across concurrent writers.
func (r *Router) fanOut(trackID string, packet *rtp.Packet) {
	r.mu.RLock()
	subs, ok := r.subscriptions[trackID]
	r.mu.RUnlock()

	if !ok || len(subs) == 0 {
		return
	}

	// Marshal once, clone per-subscriber to avoid memory races
	raw, err := packet.Marshal()
	if err != nil {
		return
	}

	for _, sub := range subs {
		cloned := &rtp.Packet{}
		if err := cloned.Unmarshal(raw); err == nil {
			if err := sub.Local.WriteRTP(cloned); err != nil {
				log.Printf("Router[%s]: Write error to subscriber %s: %v", r.roomID, sub.PeerID, err)
			}
		}
	}
}

// Close stops all routing activity.
func (r *Router) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	close(r.stopCh)
	for _, ut := range r.upTracks {
		close(ut.Done)
	}
	r.upTracks = nil
	r.subscriptions = nil
}

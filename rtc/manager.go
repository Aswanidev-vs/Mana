package rtc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// PeerState represents the state of a peer connection.
type PeerState int

const (
	PeerStateNew PeerState = iota
	PeerStateConnecting
	PeerStateConnected
	PeerStateDisconnected
	PeerStateFailed
)

func (s PeerState) String() string {
	switch s {
	case PeerStateNew:
		return "new"
	case PeerStateConnecting:
		return "connecting"
	case PeerStateConnected:
		return "connected"
	case PeerStateDisconnected:
		return "disconnected"
	case PeerStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Peer wraps a pion/webrtc PeerConnection with framework metadata.
type Peer struct {
	mu      sync.RWMutex
	ID      string
	UserID  string
	RoomID  string
	State   PeerState
	PC      *webrtc.PeerConnection
	Created time.Time
	Updated time.Time

	Negotiator *Negotiator
	Router     *Router

	// Pending ICE candidates waiting for remote description
	pendingCandidates []*webrtc.ICECandidateInit

	// Initial connection tracking
	initialConnected bool

	// JitterBuffers: SSRC -> JitterBuffer
	JitterBuffers map[uint32]*JitterBuffer

	onTrack func(peerID string, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	onRecoveryNeeded func(peerID, roomID, reason string)
}

// NewPeer creates a new Peer wrapping the given pion PeerConnection.
func NewPeer(id, userID, roomID string, pc *webrtc.PeerConnection, onTrack func(string, string, string), onRecoveryNeeded func(string, string, string)) *Peer {
	now := time.Now()
	p := &Peer{
		ID:                id,
		UserID:            userID,
		RoomID:            roomID,
		State:             PeerStateNew,
		PC:                pc,
		Created:           now,
		Updated:           now,
		pendingCandidates: make([]*webrtc.ICECandidateInit, 0),
		JitterBuffers:     make(map[uint32]*JitterBuffer),
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.mu.Lock()
		switch state {
		case webrtc.PeerConnectionStateNew:
			p.State = PeerStateNew
		case webrtc.PeerConnectionStateConnecting:
			p.State = PeerStateConnecting
		case webrtc.PeerConnectionStateConnected:
			p.State = PeerStateConnected
			p.initialConnected = true
		case webrtc.PeerConnectionStateDisconnected:
			p.State = PeerStateDisconnected
		case webrtc.PeerConnectionStateFailed:
			p.State = PeerStateFailed
		}
		p.Updated = time.Now()
		p.mu.Unlock()

		log.Printf("Peer %s state: %s", id, state.String())
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("Peer %s ICE state: %s", id, state.String())

		p.mu.RLock()
		connectedOnce := p.initialConnected
		p.mu.RUnlock()

		if !connectedOnce {
			return
		}
		if state == webrtc.ICEConnectionStateDisconnected || state == webrtc.ICEConnectionStateFailed {
			if onRecoveryNeeded != nil {
				go onRecoveryNeeded(id, roomID, state.String())
			}
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Peer %s received track: %s from room %s", id, track.Kind().String(), p.RoomID)

		// Setup Jitter Buffer for loss recovery if needed
		ssrc := uint32(track.SSRC())
		p.mu.Lock()
		if _, ok := p.JitterBuffers[ssrc]; !ok {
			p.JitterBuffers[ssrc] = NewJitterBuffer(512)
		}
		p.mu.Unlock()

		// Publish to room router with NACK feedback loop
		if p.Router != nil {
			p.Router.AddUpTrack(id, track, func(missing []uint16) {
				// Send RTCP NACK back to the source
				nacks := CreateNack(uint32(track.SSRC()), missing)
				if err := p.PC.WriteRTCP(nacks); err != nil {
					log.Printf("Peer %s: Error writing RTCP NACK: %v", id, err)
				}
			})
		}

		// Trigger manager-level notification
		if onTrack != nil {
			onTrack(id, roomID, track.ID())
		}

		if p.onTrack != nil {
			p.onTrack(id, track, receiver)
		}
	})

	return p
}

// SetOnTrack registers a callback for incoming media tracks.
func (p *Peer) SetOnTrack(handler func(peerID string, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onTrack = handler
}

// SetState updates the peer state.
func (p *Peer) SetState(state PeerState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.State = state
	p.Updated = time.Now()
}

// GetState returns the current peer state.
func (p *Peer) GetState() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State
}

// AddICECandidate adds a remote ICE candidate to the peer connection.
// If the remote description is not yet set, the candidate is queued for later.
func (p *Peer) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.PC.RemoteDescription() == nil {
		// Queue for later processing
		p.pendingCandidates = append(p.pendingCandidates, &candidate)
		log.Printf("Peer %s: Queued ICE candidate (remote description not set yet)", p.ID)
		return nil
	}

	return p.PC.AddICECandidate(candidate)
}

// ProcessPendingCandidates processes any queued ICE candidates after remote description is set.
func (p *Peer) ProcessPendingCandidates() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pendingCandidates) == 0 {
		return
	}

	log.Printf("Peer %s: Processing %d pending ICE candidates", p.ID, len(p.pendingCandidates))

	for _, candidate := range p.pendingCandidates {
		if err := p.PC.AddICECandidate(*candidate); err != nil {
			log.Printf("Peer %s: Error processing pending candidate: %v", p.ID, err)
		}
	}

	p.pendingCandidates = p.pendingCandidates[:0]
}

// IsInitialConnected returns whether the initial connection has been established.
func (p *Peer) IsInitialConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialConnected
}

// CreateOffer creates an SDP offer on the peer connection.
// Uses the Negotiator if available, otherwise falls back to direct creation.
func (p *Peer) CreateOffer() (*webrtc.SessionDescription, error) {
	if p.Negotiator != nil {
		p.Negotiator.Negotiate()
		return p.PC.LocalDescription(), nil
	}

	offer, err := p.PC.CreateOffer(nil)
	if err != nil {
		return nil, err
	}
	err = p.PC.SetLocalDescription(offer)
	if err != nil {
		return nil, err
	}

	// Wait for ICE gathering to complete
	<-webrtc.GatheringCompletePromise(p.PC)

	return p.PC.LocalDescription(), nil
}

// CreateAnswer creates an SDP answer after receiving an offer.
func (p *Peer) CreateAnswer() (*webrtc.SessionDescription, error) {
	answer, err := p.PC.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	err = p.PC.SetLocalDescription(answer)
	if err != nil {
		return nil, err
	}

	// Wait for ICE gathering to complete
	<-webrtc.GatheringCompletePromise(p.PC)

	return p.PC.LocalDescription(), nil
}

// SetRemoteDescription sets the remote SDP (offer or answer) and processes pending ICE candidates.
func (p *Peer) SetRemoteDescription(sdp webrtc.SessionDescription) error {
	if p.Negotiator != nil {
		if sdp.Type == webrtc.SDPTypeOffer {
			err := p.Negotiator.HandleOffer(sdp)
			if err == nil {
				p.ProcessPendingCandidates()
			}
			return err
		} else if sdp.Type == webrtc.SDPTypeAnswer {
			err := p.Negotiator.HandleAnswer(sdp)
			if err == nil {
				p.ProcessPendingCandidates()
			}
			return err
		}
	}

	err := p.PC.SetRemoteDescription(sdp)
	if err == nil {
		p.ProcessPendingCandidates()
	}
	return err
}

// AddTrack adds a local media track to the peer connection.
func (p *Peer) AddTrack(track webrtc.TrackLocal) (*webrtc.RTPSender, error) {
	return p.PC.AddTrack(track)
}

// Close closes the peer connection.
func (p *Peer) Close() error {
	p.mu.Lock()
	p.State = PeerStateDisconnected
	p.Updated = time.Now()
	p.mu.Unlock()
	return p.PC.Close()
}

// Manager manages WebRTC peer connections using pion/webrtc.
type Manager struct {
	mu    sync.RWMutex
	peers map[string]*Peer

	// routers: RoomID -> Router
	routers map[string]*Router

	// Phase 2 components
	Congestion *CongestionController
	Simulcast  *SimulcastManager

	// Callbacks
	onTrack          func(peerID, roomID, trackID string)
	onRecoveryNeeded func(peerID, roomID, reason string)

	api  *webrtc.API
	conf webrtc.Configuration

	externalIPs []string
}

// NewManager creates a new RTC Manager with default settings.
func NewManager(stunServers []string) *Manager {
	cfg := core.DefaultConfig()
	if len(stunServers) > 0 {
		cfg.STUNServers = stunServers
	}
	return NewManagerWithConfig(cfg)
}

// NewManagerWithConfig creates a new RTC Manager with full production-ready configuration.
func NewManagerWithConfig(cfg core.Config) *Manager {
	stunServers := cfg.STUNServers
	if len(stunServers) == 0 {
		stunServers = []string{
			"stun:stun.cloudflare.com:3478",
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		}
	}

	// Create media engine with proper codec registration
	m := &webrtc.MediaEngine{}

	// Register Opus for audio
	opusCodec := webrtc.RTPCodecCapability{
		MimeType:     webrtc.MimeTypeOpus,
		ClockRate:    48000,
		Channels:     1,
		SDPFmtpLine:  "minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0;maxaveragebitrate=32000;cbr=1",
		RTCPFeedback: nil,
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: opusCodec,
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		log.Printf("RTC: Failed to register Opus codec: %v", err)
	}

	// Register VP8 for video with RTCP feedback for packet loss recovery
	vp8Codec := webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
		RTCPFeedback: []webrtc.RTCPFeedback{
			{Type: "goog-remb", Parameter: ""},
			{Type: "ccm", Parameter: "fir"},
			{Type: "nack", Parameter: ""},
			{Type: "nack", Parameter: "pli"},
		},
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: vp8Codec,
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("RTC: Failed to register VP8 codec: %v", err)
	}

	// Create interceptor registry
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		log.Printf("RTC: Failed to register interceptors: %v", err)
	}

	// Create setting engine with optimizations
	s := webrtc.SettingEngine{}
	s.SetSRTPReplayProtectionWindow(512)
	s.SetICETimeouts(10*time.Second, 30*time.Second, 2*time.Second)
	s.SetDTLSRetransmissionInterval(100 * time.Millisecond)
	s.DisableActiveTCP(true) // Standard for cloud/tunnel stability

	// Configure Port Range (Essential for firewalls)
	if cfg.WebRTCPortRangeLow != 0 && cfg.WebRTCPortRangeHigh != 0 {
		if err := s.SetEphemeralUDPPortRange(cfg.WebRTCPortRangeLow, cfg.WebRTCPortRangeHigh); err != nil {
			log.Printf("RTC: Failed to set UDP port range: %v", err)
		}
	}

	// NAT 1:1 Mapping for global cloud delivery
	if len(cfg.ExternalIPs) > 0 {
		var validIPs []string
		for _, ip := range cfg.ExternalIPs {
			if net.ParseIP(ip) != nil {
				validIPs = append(validIPs, ip)
			}
		}
		if len(validIPs) > 0 {
			log.Printf("RTC: Configuring NAT 1:1 IPs: %v", validIPs)
			s.SetNAT1To1IPs(validIPs, webrtc.ICECandidateTypeHost)
		}
	}

	// Network Restriction (Force TCP for tunnels/highly restrictive firewalls)
	if cfg.WebRTCForceTCP {
		log.Printf("RTC: WebRTC Network restricted to TCP-ONLY (Tunnel/Firewall Mode)")
		s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeTCP4})
	} else {
		log.Printf("RTC: WebRTC standard UDP+TCP networking enabled")
		s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeTCP4})
	}

	// Enable UDP-Mux (Single-port UDP discovery - Pion v4 style)
	if cfg.WebRTCUDPPort > 0 {
		mux, err := ice.NewMultiUDPMuxFromPort(cfg.WebRTCUDPPort)
		if err != nil {
			log.Printf("RTC: Failed to start UDP-Mux on port %d: %v", cfg.WebRTCUDPPort, err)
		} else {
			log.Printf("RTC: WebRTC UDP-Mux listening on port %d", cfg.WebRTCUDPPort)
			s.SetICEUDPMux(mux)
		}
	}

	// Enable TCP-Mux (NAT traversal via tunnels/80/443 mapping)
	if cfg.WebRTCTCPPort > 0 {
		tcpListener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4zero, Port: cfg.WebRTCTCPPort})
		if err != nil {
			log.Printf("RTC: Failed to start TCP-Mux on port %d: %v", cfg.WebRTCTCPPort, err)
		} else {
			log.Printf("RTC: WebRTC TCP-Mux listening on %s", tcpListener.Addr())
			tcpMux := ice.NewTCPMuxDefault(ice.TCPMuxParams{
				Listener:       tcpListener,
				Logger:         nil, // Use default pion logger
				ReadBufferSize: 20,
			})
			s.SetICETCPMux(tcpMux)
		}
	}

	// Generate standard ICE Configuration
	iceServers := []webrtc.ICEServer{}
	for _, srv := range stunServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{srv}})
	}
	for _, t := range cfg.TURNServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:           t.URLs,
			Username:       t.Username,
			Credential:     t.Credential,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}

	conf := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.NewICETransportPolicy(cfg.ICETransportPolicy),
	}

	// Create API
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithSettingEngine(s),
	)

	// Update configuration to use the API
	rtc := &Manager{
		peers:   make(map[string]*Peer),
		routers: make(map[string]*Router),
		api:     api,
		conf:    conf,
		externalIPs: cfg.ExternalIPs,
	}

	// Initialize Phase 2: Adaptive Media Optimization
	rtc.Simulcast = NewSimulcastManager()
	// Min 100kbps, Max 5mbps, Start 1mbps
	rtc.Congestion = NewCongestionController(100000, 5000000, 1000000, func(subID string, bw uint32) {
		rtc.Simulcast.SetBandwidth(subID, bw)
	})

	return rtc
}

// GetRouter returns or creates the router for the given room.
func (m *Manager) GetRouter(roomID string) *Router {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.routers[roomID]
	if !ok {
		r = NewRouter(roomID)
		m.routers[roomID] = r
	}
	return r
}

// CreatePeerConnection creates a new pion PeerConnection and tracks it.
// Uses the configured MediaEngine/interceptors API for proper codec support.
func (m *Manager) CreatePeerConnection(id, userID, roomID string) (*Peer, error) {
	var pc *webrtc.PeerConnection
	var err error

	if m.api != nil {
		pc, err = m.api.NewPeerConnection(m.conf)
	} else {
		pc, err = webrtc.NewPeerConnection(m.conf)
	}

	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	peer := NewPeer(id, userID, roomID, pc, m.onTrack, m.onRecoveryNeeded)

	// Link to room router
	peer.Router = m.GetRouter(roomID)

	m.mu.Lock()
	m.peers[id] = peer
	m.mu.Unlock()

	return peer, nil
}

// SetOnTrack registers a global callback for new tracks.
func (m *Manager) SetOnTrack(handler func(peerID, roomID, trackID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onTrack = handler
}

// SetOnRecoveryNeeded registers a callback for network recovery and ICE restart requests.
func (m *Manager) SetOnRecoveryNeeded(handler func(peerID, roomID, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRecoveryNeeded = handler
}

// GetPeer retrieves a tracked peer by ID.
func (m *Manager) GetPeer(id string) (*Peer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peer, ok := m.peers[id]
	if !ok {
		return nil, fmt.Errorf("peer %s not found", id)
	}
	return peer, nil
}

// RemovePeer removes and closes a peer connection.
func (m *Manager) RemovePeer(id string) {
	m.mu.Lock()
	peer, ok := m.peers[id]
	if ok {
		delete(m.peers, id)
	}
	m.mu.Unlock()

	if ok {
		_ = peer.Close()
	}
}

// HandleOffer creates a peer from an incoming SDP offer and returns the answer.
func (m *Manager) HandleOffer(ctx context.Context, peerID, userID, roomID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	peer, err := m.CreatePeerConnection(peerID, userID, roomID)
	if err != nil {
		return nil, err
	}

	// Setup Negotiator for Perfect Negotiation
	peer.Negotiator = NewNegotiator(false, peer.PC, nil, nil)

	if err := peer.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("set remote description: %w", err)
	}

	peer.SetState(PeerStateConnecting)

	answer := peer.PC.LocalDescription()
	if answer == nil {
		answer, err = peer.CreateAnswer()
		if err != nil {
			return nil, fmt.Errorf("create answer: %w", err)
		}
	}

	peer.SetState(PeerStateConnected)
	return answer, nil
}

// HandleAnswer sets the remote SDP answer on an existing peer.
func (m *Manager) HandleAnswer(peerID string, answer webrtc.SessionDescription) error {
	peer, err := m.GetPeer(peerID)
	if err != nil {
		return err
	}

	if err := peer.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}

	peer.SetState(PeerStateConnected)
	return nil
}

// Subscribe hooks a peer to an existing track in their room.
func (m *Manager) Subscribe(peerID, trackID string) error {
	peer, err := m.GetPeer(peerID)
	if err != nil {
		return err
	}

	if peer.Router == nil {
		return fmt.Errorf("peer %s has no router", peerID)
	}

	// Phase 2: Track subscription in simulcast manager
	m.Simulcast.AddTrack(trackID, peer.ID)
	m.Congestion.AddSubscriber(peer.ID)

	// Create a local track to receive the media
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}
	localTrack, err := webrtc.NewTrackLocalStaticRTP(codec, trackID, peer.ID)
	if err != nil {
		return err
	}

	sender, err := peer.AddTrack(localTrack)
	if err != nil {
		return err
	}

	// Link to router
	if err := peer.Router.AddSubscriber(trackID, peer.ID, localTrack, sender); err != nil {
		return err
	}

	// Start RTCP feedback loop for this subscriber
	go func() {
		for {
			packets, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, pkt := range packets {
				switch p := pkt.(type) {
				case *rtcp.PictureLossIndication:
					log.Printf("SFU: Received PLI from subscriber %s for track %s", peer.ID, trackID)
					m.forwardPLI(trackID)
				case *rtcp.ReceiverEstimatedMaximumBitrate:
					m.Congestion.UpdateBitrateEstimate(peer.ID, uint32(p.Bitrate))
				case *rtcp.ReceiverReport:
					for _, r := range p.Reports {
						if r.Delay != 0 {
							rtt := uint32(r.Delay) / 65536 * 1000
							m.Congestion.UpdateMetrics(peer.ID, rtt, float64(r.FractionLost)/255.0)
						}
					}
				}
			}
		}
	}()

	return nil
}

// forwardPLI finds the publisher of a track and sends a PLI to request a keyframe.
func (m *Manager) forwardPLI(trackID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find the peer that is publishing this track
	for _, p := range m.peers {
		for _, receiver := range p.PC.GetReceivers() {
			if receiver.Track() != nil && receiver.Track().ID() == trackID {
				// Send PLI to the publisher
				_ = p.PC.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(receiver.Track().SSRC())}})
				return
			}
		}
	}
}

// HandleICECandidate adds a remote ICE candidate to a peer.
func (m *Manager) HandleICECandidate(peerID string, candidate webrtc.ICECandidateInit) error {
	peer, err := m.GetPeer(peerID)
	if err != nil {
		return err
	}
	return peer.AddICECandidate(candidate)
}

// RestartICE creates a new offer with ICE restart enabled for a peer.
func (m *Manager) RestartICE(peerID string) (*webrtc.SessionDescription, error) {
	peer, err := m.GetPeer(peerID)
	if err != nil {
		return nil, err
	}

	offer, err := peer.PC.CreateOffer(&webrtc.OfferOptions{ICERestart: true})
	if err != nil {
		return nil, err
	}
	if err := peer.PC.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	<-webrtc.GatheringCompletePromise(peer.PC)
	return peer.PC.LocalDescription(), nil
}

// CreateLocalTrack creates a new local media track for sending.
func CreateLocalTrack(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
	return webrtc.NewTrackLocalStaticRTP(codec, id, streamID)
}

func parseCredentialType(value string) webrtc.ICECredentialType {
	switch value {
	case "oauth":
		return webrtc.ICECredentialTypeOauth
	default:
		return webrtc.ICECredentialTypePassword
	}
}

// PeerCount returns the number of tracked peers.
func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// CallHandler handles call events.
type CallHandler func(event core.CallEvent)

// CallManager manages active calls.
type CallManager struct {
	mu      sync.RWMutex
	calls   map[string]*core.CallEvent
	handler func(event core.CallEvent)
}

// NewCallManager creates a new CallManager.
func NewCallManager() *CallManager {
	return &CallManager{
		calls: make(map[string]*core.CallEvent),
	}
}

// OnCallEvent registers a handler for call events.
func (cm *CallManager) OnCallEvent(handler func(event core.CallEvent)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.handler = handler
}

// StartCall initiates a new call.
func (cm *CallManager) StartCall(callType core.CallType, roomID, caller, callee string) *core.CallEvent {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	event := &core.CallEvent{
		Status:  "started",
		Type:    callType,
		RoomID:  roomID,
		Caller:  caller,
		Callee:  callee,
		Started: time.Now(),
	}
	cm.calls[roomID] = event

	if cm.handler != nil {
		go cm.handler(*event)
	}
	return event
}

// EndCall terminates a call.
func (cm *CallManager) EndCall(roomID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	call, ok := cm.calls[roomID]
	if !ok {
		return fmt.Errorf("no active call in room %s", roomID)
	}
	delete(cm.calls, roomID)
	if cm.handler != nil {
		ended := *call
		ended.Status = "ended"
		ended.Ended = time.Now()
		go cm.handler(ended)
	}
	return nil
}

// ActiveCallCount returns the number of active calls.
func (cm *CallManager) ActiveCallCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.calls)
}

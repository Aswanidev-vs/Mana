package rtc

import (
	"log"
	"sync"

	"github.com/pion/webrtc/v4"
)

// TransceiverRequest represents a request to add a transceiver during renegotiation.
type TransceiverRequest struct {
	CodecType webrtc.RTPCodecType
	Init      webrtc.RTPTransceiverInit
}

// Negotiator implements the "Perfect Negotiation" pattern for stable WebRTC state transitions.
// It ensures that offers and answers are sequenced correctly even with multiple participants.
// Follows the peer-calls pattern with signaling state tracking and queued transceivers.
type Negotiator struct {
	mu sync.Mutex

	pc          *webrtc.PeerConnection
	onOffer     func(offer webrtc.SessionDescription)
	onRequest   func()
	isInitiator bool
	makingOffer bool
	ignoreOffer bool
	isPolite    bool

	// Negotiation state tracking
	negotiationDone   chan struct{}
	queuedNegotiation bool

	// Queued transceiver requests to batch before creating an offer
	queuedTransceiverRequests []TransceiverRequest
}

// NewNegotiator creates a new negotiator for a PeerConnection.
// isPolite: If true, this peer will yield to incoming offers during "SDP glare".
func NewNegotiator(isPolite bool, pc *webrtc.PeerConnection, onOffer func(webrtc.SessionDescription), onRequest func()) *Negotiator {
	n := &Negotiator{
		pc:          pc,
		onOffer:     onOffer,
		onRequest:   onRequest,
		isPolite:    isPolite,
		isInitiator: onOffer != nil, // If we have an onOffer callback, we're the initiator
	}

	// Track signaling state changes for proper sequencing
	pc.OnSignalingStateChange(n.handleSignalingStateChange)

	return n
}

// handleSignalingStateChange manages negotiation queue when signaling state transitions.
func (n *Negotiator) handleSignalingStateChange(state webrtc.SignalingState) {
	log.Printf("Negotiator: Signaling state changed to %v", state)

	n.mu.Lock()
	defer n.mu.Unlock()

	switch state {
	case webrtc.SignalingStateClosed:
		n.closeDoneChannel()
	case webrtc.SignalingStateStable:
		if n.queuedNegotiation {
			log.Printf("Negotiator: Execute queued negotiation")
			n.queuedNegotiation = false
			n.negotiate()
		} else {
			n.closeDoneChannel()
		}
	}
}

// closeDoneChannel safely closes the negotiation done channel.
func (n *Negotiator) closeDoneChannel() {
	if n.negotiationDone != nil {
		close(n.negotiationDone)
		n.negotiationDone = nil
	}
}

// Done returns a channel that's closed when negotiation is complete.
func (n *Negotiator) Done() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.negotiationDone != nil {
		return n.negotiationDone
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

// AddTransceiverFromKind queues a transceiver request and triggers negotiation.
func (n *Negotiator) AddTransceiverFromKind(t TransceiverRequest) {
	log.Printf("Negotiator: Add transceiver - kind=%v, direction=%v", t.CodecType, t.Init.Direction)

	n.mu.Lock()
	n.queuedTransceiverRequests = append(n.queuedTransceiverRequests, t)
	n.mu.Unlock()

	n.Negotiate()
}

// addQueuedTransceivers adds any queued transceivers to the peer connection.
func (n *Negotiator) addQueuedTransceivers() {
	for _, t := range n.queuedTransceiverRequests {
		log.Printf("Negotiator: Adding queued transceiver - kind=%v, direction=%v", t.CodecType, t.Init.Direction)
		if _, err := n.pc.AddTransceiverFromKind(t.CodecType, t.Init); err != nil {
			log.Printf("Negotiator: Error adding queued transceiver: %v", err)
		}
	}
	n.queuedTransceiverRequests = n.queuedTransceiverRequests[:0]
}

// Negotiate triggers the negotiation process.
// Returns a channel that's closed when negotiation completes.
func (n *Negotiator) Negotiate() (done <-chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.pc == nil {
		return
	}

	if n.negotiationDone != nil {
		log.Printf("Negotiator: Already negotiating, queueing for later")
		n.queuedNegotiation = true
		return
	}

	log.Printf("Negotiator: Starting negotiation")
	n.negotiationDone = make(chan struct{})

	n.negotiate()
	return n.negotiationDone
}

// negotiate performs the actual negotiation (must be called with lock held).
func (n *Negotiator) negotiate() {
	n.addQueuedTransceivers()

	if !n.isInitiator {
		log.Printf("Negotiator: Requesting negotiation from initiator")
		n.requestNegotiation()
		return
	}

	log.Printf("Negotiator: Creating offer")
	n.makingOffer = true

	offer, err := n.pc.CreateOffer(nil)
	if err != nil {
		log.Printf("Negotiator: Error creating offer: %v", err)
		n.makingOffer = false
		n.closeDoneChannel()
		return
	}

	if err := n.pc.SetLocalDescription(offer); err != nil {
		log.Printf("Negotiator: Error setting local description: %v", err)
		n.makingOffer = false
		n.closeDoneChannel()
		return
	}

	n.makingOffer = false

	if n.onOffer != nil {
		n.onOffer(*n.pc.LocalDescription())
	}
}

// requestNegotiation requests the initiator to start negotiation.
func (n *Negotiator) requestNegotiation() {
	if n.onRequest != nil {
		n.onRequest()
	}
}

// HandleOffer processes an incoming SDP offer with collision handling.
func (n *Negotiator) HandleOffer(offer webrtc.SessionDescription) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Glare handling
	offerCollision := (n.makingOffer || n.pc.SignalingState() != webrtc.SignalingStateStable)
	n.ignoreOffer = !n.isPolite && offerCollision

	if n.ignoreOffer {
		log.Printf("Negotiator: Ignoring offer due to collision (impolite)")
		return nil
	}

	if err := n.pc.SetRemoteDescription(offer); err != nil {
		return err
	}

	n.addQueuedTransceivers()

	answer, err := n.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	return n.pc.SetLocalDescription(answer)
}

// HandleAnswer processes an incoming SDP answer.
func (n *Negotiator) HandleAnswer(answer webrtc.SessionDescription) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.pc.SignalingState() == webrtc.SignalingStateStable {
		log.Printf("Negotiator: Ignoring answer (already stable)")
		return nil
	}

	if err := n.pc.SetRemoteDescription(answer); err != nil {
		return err
	}

	return nil
}

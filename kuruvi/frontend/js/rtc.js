const RTC = {
    pc: null,
    localStream: null,
    remoteStreams: new Map(), // peerID -> MediaStream
    onTrack: null, // callback(peerID, stream, track)
    onConnectionStateChange: null, // callback(state)
    pendingCandidates: [],

    async init(iceServers = []) {
        if (this.pc) this.pc.close();
        this.pc = new RTCPeerConnection({ iceServers });
        // Don't clear pendingCandidates here! They might have arrived before init.
        
        this.pc.ontrack = (e) => {
            if (this.onTrack) this.onTrack(e.streams[0], e.track);
        };

        this.pc.oniceconnectionstatechange = () => {
            if (this.onConnectionStateChange) this.onConnectionStateChange(this.pc.iceConnectionState);
        };

        return this.pc;
    },

    async getUserMedia(video = true, audio = true) {
        this.localStream = await navigator.mediaDevices.getUserMedia({ video, audio });
        this.localStream.getTracks().forEach(track => {
            this.pc.addTrack(track, this.localStream);
        });
        return this.localStream;
    },

    async startScreenShare() {
        const screenStream = await navigator.mediaDevices.getDisplayMedia({ video: true });
        const screenTrack = screenStream.getVideoTracks()[0];
        
        // Find the video sender and replace track
        const senders = this.pc.getSenders();
        const videoSender = senders.find(s => s.track && s.track.kind === 'video');
        
        if (videoSender) {
            videoSender.replaceTrack(screenTrack);
        }

        screenTrack.onended = () => {
            this.stopScreenShare();
        };

        this.screenStream = screenStream;
        return screenStream;
    },

    async stopScreenShare() {
        if (this.screenStream) {
            this.screenStream.getTracks().forEach(t => t.stop());
            this.screenStream = null;
        }
        
        // Revert to camera if available
        if (this.localStream) {
            const videoTrack = this.localStream.getVideoTracks()[0];
            const senders = this.pc.getSenders();
            const videoSender = senders.find(s => s.track && s.track.kind === 'video');
            if (videoSender && videoTrack) {
                videoSender.replaceTrack(videoTrack);
            }
        }
    },

    async createOffer() {
        const offer = await this.pc.createOffer();
        await this.pc.setLocalDescription(offer);
        return offer;
    },

    async handleAnswer(answerSDP) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'answer', sdp: answerSDP }));
        await this.processPendingCandidates();
    },

    async handleOffer(offerSDP) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'offer', sdp: offerSDP }));
        const answer = await this.pc.createAnswer();
        await this.pc.setLocalDescription(answer);
        await this.processPendingCandidates();
        return answer;
    },

    async addIceCandidate(candidate) {
        if (!this.pc || !this.pc.remoteDescription) {
            this.pendingCandidates.push(candidate);
            return;
        }
        try {
            await this.pc.addIceCandidate(new RTCIceCandidate(candidate));
        } catch (e) {
            console.error('Error adding ICE candidate:', e);
        }
    },

    async processPendingCandidates() {
        while (this.pendingCandidates.length > 0) {
            const candidate = this.pendingCandidates.shift();
            try {
                await this.pc.addIceCandidate(new RTCIceCandidate(candidate));
            } catch (e) {
                console.warn('Deferred ICE candidate addition failed:', e);
            }
        }
    },

    close() {
        if (this.pc) {
            this.pc.close();
            this.pc = null;
        }
        if (this.localStream) {
            this.localStream.getTracks().forEach(t => t.stop());
            this.localStream = null;
        }
        this.pendingCandidates = [];
    }
};

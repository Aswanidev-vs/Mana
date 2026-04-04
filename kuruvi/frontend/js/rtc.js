const RTC = {
    pc: null,
    localStream: null,
    remoteStreams: new Map(), // peerID -> MediaStream
    onTrack: null, // callback(peerID, stream, track)
    onConnectionStateChange: null, // callback(state)

    async init(iceServers = []) {
        if (this.pc) this.pc.close();
        this.pc = new RTCPeerConnection({ iceServers });
        
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

    async createOffer() {
        const offer = await this.pc.createOffer();
        await this.pc.setLocalDescription(offer);
        return offer;
    },

    async handleAnswer(answerSDP) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'answer', sdp: answerSDP }));
    },

    async handleOffer(offerSDP) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'offer', sdp: offerSDP }));
        const answer = await this.pc.createAnswer();
        await this.pc.setLocalDescription(answer);
        return answer;
    },

    async addIceCandidate(candidate) {
        if (!this.pc || !this.pc.remoteDescription) return;
        await this.pc.addIceCandidate(new RTCIceCandidate(candidate));
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
    }
};

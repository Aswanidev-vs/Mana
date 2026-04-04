let ws;
let currentChat = null;
let contacts = [];

const App = {
    async init() {
        if (API.token) {
            this.showApp();
        } else {
            this.showLogin();
        }

        this.bindEvents();
    },

    showLogin() {
        document.getElementById('login-modal').classList.remove('hidden');
        document.getElementById('app').classList.add('hidden');
    },

    async showApp() {
        document.getElementById('login-modal').classList.add('hidden');
        document.getElementById('app').classList.remove('hidden');
        await this.loadContacts();
        this.renderContacts();
        this.connectWS();
        this.initE2EE();
    },

    async loadContacts() {
        try {
            contacts = await API.getContacts();
        } catch (e) {
            console.warn('Failed to load contacts:', e);
            contacts = [];
        }
    },

    async initE2EE() {
        const pubKey = await Crypto.generateKeys();
        // Upload public key to server for persistence
        await API.uploadPublicKey(API.username, pubKey);

        if (ws && ws.readyState === WebSocket.OPEN) {
            const binaryString = Array.from(pubKey).map(b => String.fromCharCode(b)).join('');
            ws.send(JSON.stringify({
                type: 'key_exchange',
                from: API.username,
                payload: btoa(binaryString) // Base64 encode for Go []byte
            }));
        }
    },

    connectWS() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        // Specify "mana" subprotocol required by coder/websocket in the framework
        ws = new WebSocket(`${protocol}//${window.location.host}/ws?token=${API.token}`, ['mana']);

        ws.onopen = () => {
            console.log('Connected to Kuruvi WebSocket');
            this.initE2EE(); // Reshare keys on reconnect
        };

        ws.onclose = (e) => {
            console.warn('WebSocket connection closed:', e.code, e.reason);
            if (e.code === 4001) { // Custom auth error from mana framework
                API.logout();
            }
        };

        ws.onerror = (e) => {
            console.error('WebSocket error:', e);
            document.getElementById('auth-error').textContent = 'Connection failed. Please check if the server is running.';
        };

        ws.onmessage = (e) => {
            const msg = JSON.parse(e.data);
            this.handleWSMessage(msg);
        };
    },

    async handleWSMessage(msg) {
        switch (msg.type) {
            case 'message':
                await this.onReceiveMessage(msg);
                break;
            case 'message_sync':
                if (msg.messages) {
                    for (const m of msg.messages) {
                        await this.onReceiveMessage(m);
                    }
                }
                break;
            case 'key_exchange':
                const rawKey = new Uint8Array(atob(msg.payload).split('').map(c => c.charCodeAt(0)));
                await Crypto.importRemoteKey(msg.from, rawKey);
                console.log('Received public key from', msg.from);
                break;
            case 'offer':
                this.onReceiveCallOffer(msg);
                break;
            case 'answer':
                await RTC.handleAnswer(msg.sdp);
                break;
            case 'candidate':
                await RTC.addIceCandidate(JSON.parse(msg.candidate));
                break;
            case 'typing':
                if (currentChat && msg.from === currentChat.id) {
                    this.showTyping(msg.from);
                }
                break;
        }
    },

    async onReceiveMessage(msg) {
        let text = '';
        try {
            // Attempt E2EE decryption
            text = await Crypto.decrypt(msg.payload, msg.sender_id || msg.from);
        } catch (e) {
            // Fallback for non-encrypted (decode from Base64 if needed)
            const raw = new Uint8Array(atob(msg.payload).split('').map(c => c.charCodeAt(0)));
            text = new TextDecoder().decode(raw);
        }

        this.addMessageToUI({
            text,
            sender: msg.sender_id || msg.from,
            timestamp: new Date()
        });
    },

    bindEvents() {
        // Auth
        document.getElementById('login-btn').onclick = async () => {
            const user = document.getElementById('username').value;
            const pass = document.getElementById('password').value;
            try {
                await API.login(user, pass);
                this.showApp();
            } catch (e) {
                document.getElementById('auth-error').textContent = e.message;
            }
        };

        document.getElementById('toggle-auth').onclick = async (e) => {
            e.preventDefault();
            const btn = document.getElementById('login-btn');
            const toggle = document.getElementById('toggle-auth');
            const isLogin = btn.textContent === 'Sign In';

            if (isLogin) {
                btn.textContent = 'Sign Up';
                toggle.textContent = 'Already have an account? Sign In';
                btn.onclick = async () => {
                    const user = document.getElementById('username').value;
                    const pass = document.getElementById('password').value;
                    try {
                        await API.register(user, pass);
                        this.showApp();
                    } catch (e) {
                        document.getElementById('auth-error').textContent = e.message;
                    }
                };
            } else {
                btn.textContent = 'Sign In';
                toggle.textContent = 'Create an account';
                btn.onclick = async () => {
                    const user = document.getElementById('username').value;
                    const pass = document.getElementById('password').value;
                    try {
                        await API.login(user, pass);
                        this.showApp();
                    } catch (e) {
                        document.getElementById('auth-error').textContent = e.message;
                    }
                };
            }
        };

        document.getElementById('logout-btn').onclick = () => API.logout();

        // Chat
        document.getElementById('send-btn').onclick = () => this.sendMessage();
        document.getElementById('message-input').onkeydown = (e) => {
            if (e.key === 'Enter') this.sendMessage();
            else this.sendTyping();
        };

        // Calls
        document.getElementById('video-call-btn').onclick = () => this.startCall('video');
        document.getElementById('end-call-btn').onclick = () => this.endCall();
    },

    renderContacts() {
        const list = document.getElementById('contact-list');
        list.innerHTML = '';
        contacts.forEach(c => {
            if (c.id === API.username) return; // Don't show self
            const item = document.createElement('div');
            item.className = 'contact-item';
            item.onclick = () => this.selectChat(c);
            item.innerHTML = `
                <div class="avatar">${c.name[0]}</div>
                <div class="contact-info">
                    <div class="contact-name">${c.name}</div>
                    <div class="contact-last-msg">${c.lastMsg}</div>
                </div>
            `;
            list.appendChild(item);
        });
    },

    selectChat(contact) {
        currentChat = contact;
        document.getElementById('no-chat-state').classList.add('hidden');
        document.getElementById('chat-active').classList.remove('hidden');
        document.getElementById('active-chat-name').textContent = contact.name;
        document.getElementById('active-chat-avatar').textContent = contact.name[0];
        document.getElementById('message-container').innerHTML = '';

        // Mark active item in list
        document.querySelectorAll('.contact-item').forEach(el => el.classList.remove('active'));
        // Find the one we clicked... implementation omitted for brevity, but needed in real app
    },

    async sendMessage() {
        const input = document.getElementById('message-input');
        const text = input.value.trim();
        
        if (!text) return;
        
        if (!currentChat) {
            alert('Please select a contact from the sidebar to start chatting.');
            return;
        }

        if (!ws || ws.readyState !== WebSocket.OPEN) {
            alert('Not connected to server. Please wait or refresh the page.');
            return;
        }

        try {
            // Check if we have the recipient's public key
            if (!Crypto.remoteKeys.has(currentChat.id)) {
                console.log(`Key missing for ${currentChat.id}, fetching from server...`);
                const remotePubRaw = await API.getPublicKey(currentChat.id);
                if (remotePubRaw) {
                    await Crypto.importRemoteKey(currentChat.id, remotePubRaw);
                } else {
                    alert(`${currentChat.name} hasn't set up secure messaging yet. They need to log in at least once.`);
                    return;
                }
            }

            // E2EE Encrypt
            const encrypted = await Crypto.encrypt(text, currentChat.id);

            // Robust Base64 encoding for Uint8Array
            const binaryString = Array.from(encrypted).map(b => String.fromCharCode(b)).join('');
            const payload = btoa(binaryString);

            ws.send(JSON.stringify({
                type: 'message',
                to: currentChat.id,
                from: API.username,
                payload: payload
            }));

            this.addMessageToUI({ text, sender: API.username, timestamp: new Date() });
            input.value = '';
        } catch (e) {
            console.error('Send failed:', e);
            alert('Failed to send secure message: ' + e.message);
        }
    },

    addMessageToUI({ text, sender, timestamp }) {
        const container = document.getElementById('message-container');
        const isSelf = sender === API.username;
        const msgDiv = document.createElement('div');
        msgDiv.className = `message ${isSelf ? 'self' : 'received'}`;
        msgDiv.innerHTML = `
            <div class="message-bubble">${this.escapeHTML(text)}</div>
            <div class="message-meta">
                ${timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                ${isSelf ? '<i data-lucide="check-check" style="width: 14px; height: 14px; color: var(--accent-cyan);"></i>' : ''}
            </div>
        `;
        container.appendChild(msgDiv);
        container.scrollTop = container.scrollHeight;
        lucide.createIcons();
    },

    async startCall(type) {
        document.getElementById('call-overlay').classList.remove('hidden');
        await RTC.init([{ urls: 'stun:stun.l.google.com:19302' }]);
        const stream = await RTC.getUserMedia(type === 'video', true);

        this.addStreamToUI('local', stream);

        const offer = await RTC.createOffer();
        ws.send(JSON.stringify({
            type: 'offer',
            to: currentChat.id,
            from: API.username,
            sdp: offer.sdp
        }));
    },

    async onReceiveCallOffer(msg) {
        const accept = confirm(`Incoming ${msg.type} call from ${msg.from}. Accept?`);
        if (!accept) return;

        document.getElementById('call-overlay').classList.remove('hidden');
        await RTC.init([{ urls: 'stun:stun.l.google.com:19302' }]);
        const stream = await RTC.getUserMedia(true, true);
        this.addStreamToUI('local', stream);

        const answer = await RTC.handleOffer(msg.sdp);
        ws.send(JSON.stringify({
            type: 'answer',
            to: msg.from,
            from: API.username,
            sdp: answer.sdp
        }));
    },

    addStreamToUI(id, stream) {
        let video = document.getElementById(`video-${id}`);
        if (!video) {
            const container = document.createElement('div');
            container.className = 'video-container';
            video = document.createElement('video');
            video.id = `video-${id}`;
            video.autoplay = true;
            video.playsInline = true;
            if (id === 'local') video.muted = true;
            container.appendChild(video);
            document.getElementById('video-grid').appendChild(container);
        }
        video.srcObject = stream;
    },

    endCall() {
        RTC.close();
        document.getElementById('call-overlay').classList.add('hidden');
        document.getElementById('video-grid').innerHTML = '';
    },

    sendTyping() {
        if (!currentChat || !ws) return;
        ws.send(JSON.stringify({ type: 'typing', to: currentChat.id, from: API.username }));
    },

    showTyping(userId) {
        const el = document.getElementById('typing-indicator');
        el.textContent = `${userId} is typing...`;
        clearTimeout(el.timer);
        el.timer = setTimeout(() => el.textContent = '', 3000);
    },

    escapeHTML(str) {
        const p = document.createElement('p');
        p.textContent = str;
        return p.innerHTML;
    }
};

window.onload = () => App.init();

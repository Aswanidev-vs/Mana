let ws;
let currentChat = null;
let contacts = [];

const App = {
    messages: {}, // Local store for session history { userId: [msg1, msg2] }
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
        try {
            const pubKey = await Crypto.generateKeys();
            await API.uploadPublicKey(API.userId, pubKey);
            console.log('E2EE Keys generated and uploaded');

            // Broadcast new key to all current contacts if any
            const contactsList = await API.getContacts();
            const binaryString = Array.from(pubKey).map(b => String.fromCharCode(b)).join('');
            const payload = btoa(binaryString);

            contactsList.forEach(c => {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({
                        type: 'key_exchange',
                        to: c.id,
                        from: API.userId,
                        payload: payload
                    }));
                }
            });
        } catch (e) {
            console.error('E2EE Init failed:', e);
        }
    },

    connectWS() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        // Specify "mana" subprotocol required by coder/websocket in the framework
        ws = new WebSocket(`${protocol}//${window.location.host}/ws?token=${API.token}`, ['mana']);

        ws.onopen = () => {
            console.log('Connected to Kuruvi WebSocket');
            this.initE2EE(); // Generate and share keys on connect
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
            case 'sfu_answer':
                console.log('Received RTC Answer');
                RTC.handleAnswer(msg.sdp);
                break;
            case 'candidate':
            case 'sfu_candidate':
                console.log('Received ICE Candidate');
                RTC.addIceCandidate(msg.candidate);
                break;
            case 'track_added':
                console.log('SFU: New track published');
                ws.send(JSON.stringify({
                    type: 'subscribe',
                    from: API.uniqueId || API.username,
                    room_id: msg.room_id,
                    payload: msg.payload
                }));
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
        const senderId = msg.sender_id || msg.from;
        
        try {
            // Decode Base64 string to Uint8Array first!
            const binaryString = atob(msg.payload);
            const data = new Uint8Array(binaryString.length);
            for (let i = 0; i < binaryString.length; i++) {
                data[i] = binaryString.charCodeAt(i);
            }

            // 2. Attempt decryption
            try {
                text = await Crypto.decrypt(data, senderId);
            } catch (err) {
                // If decryption failed, try fetching the latest key and retry once
                console.warn('Decryption failed, re-fetching key for', senderId);
                const remotePubRaw = await API.getPublicKey(senderId);
                if (remotePubRaw) {
                    await Crypto.importRemoteKey(senderId, remotePubRaw);
                    text = await Crypto.decrypt(data, senderId);
                } else {
                    throw err; // Re-throw if no key available
                }
            }
        } catch (e) {
            console.error('E2EE Decryption failed permanently for msg from', senderId, e);
            // Fallback for non-encrypted or identity mismatch
            try {
                const raw = new Uint8Array(atob(msg.payload).split('').map(c => c.charCodeAt(0)));
                const decoded = new TextDecoder().decode(raw);
                if (/[^\x20-\x7E]/.test(decoded)) throw new Error("Binary content");
                text = decoded;
            } catch (err) {
                text = "[Encrypted Message]";
            }
        }

        // 1. Storage: Save to local session memory
        if (!this.messages[senderId]) this.messages[senderId] = [];
        const msgObj = { text, senderId, timestamp: new Date() };
        this.messages[senderId].push(msgObj);

        // 2. Update/Add Contact in Sidebar
        let contact = contacts.find(c => c.id === senderId);
        if (!contact) {
            contact = { 
                id: senderId, 
                name: senderId.replace(/^u-/, ''), 
                lastMsg: text 
            };
            contacts.unshift(contact);
        } else {
            contact.lastMsg = text;
            contacts = [contact, ...contacts.filter(c => c.id !== senderId)];
        }
        this.renderContacts();

        // 3. Render to Chat UI if this is the active chat
        if (currentChat && currentChat.id === senderId) {
            this.addMessageToUI(msgObj);
        }
    },

    currentLoginTab: 'username',

    bindEvents() {
        // Login Tab Switching
        document.querySelectorAll('.login-tab').forEach(tab => {
            tab.onclick = () => {
                // Update active tab
                document.querySelectorAll('.login-tab').forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                
                // Show corresponding form
                document.querySelectorAll('.login-form').forEach(form => form.classList.remove('active'));
                document.getElementById(`${tab.dataset.tab}-form`).classList.add('active');
                
                this.currentLoginTab = tab.dataset.tab;
            };
        });

        // Show Password Toggles
        document.querySelectorAll('.toggle-password').forEach(btn => {
            btn.onclick = (e) => {
                e.preventDefault();
                const targetId = btn.getAttribute('data-target');
                const pInput = document.getElementById(targetId);
                const icon = btn.querySelector('i');
                if (pInput.type === 'password') {
                    pInput.type = 'text';
                    icon.setAttribute('data-lucide', 'eye-off');
                } else {
                    pInput.type = 'password';
                    icon.setAttribute('data-lucide', 'eye');
                }
                lucide.createIcons();
            };
        });

        // Forgot Password Logic
        const resetModal = document.getElementById('reset-modal');
        document.getElementById('forgot-password-link').onclick = (e) => {
            e.preventDefault();
            resetModal.classList.remove('hidden');
        };
        document.getElementById('close-reset-btn').onclick = () => {
            resetModal.classList.add('hidden');
            document.getElementById('reset-error').textContent = '';
        };
        document.getElementById('submit-reset-btn').onclick = async () => {
            const identifier = document.getElementById('reset-identifier').value.trim();
            const newPassword = document.getElementById('reset-password').value;
            const errEl = document.getElementById('reset-error');
            if (!identifier || !newPassword) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = 'Please enter identifier and new password';
                return;
            }
            try {
                // Call API 
                await API.resetPassword(identifier, newPassword);
                errEl.style.color = 'var(--accent-cyan)';
                errEl.textContent = 'Password reset successfully! You can now login.';
                setTimeout(() => {
                    resetModal.classList.add('hidden');
                    errEl.textContent = '';
                }, 2000);
            } catch (e) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = e.message;
            }
        };

        // Auth - Toggle between Sign In and Sign Up
        document.getElementById('toggle-auth').onclick = (e) => {
            e.preventDefault();
            const regFields = document.getElementById('registration-fields');
            const tabsContainer = document.getElementById('login-tabs-container');
            const btn = document.getElementById('login-btn');
            const toggleText = document.getElementById('toggle-text');
            const toggleLink = document.getElementById('toggle-auth');

            if (regFields.classList.contains('hidden')) {
                // Switch to Sign Up mode
                regFields.classList.remove('hidden');
                tabsContainer.classList.add('hidden');
                // Force Username tab if registering
                this.currentLoginTab = 'username';
                document.querySelectorAll('.login-tab').forEach(t => t.classList.remove('active'));
                document.querySelector('[data-tab="username"]').classList.add('active');
                document.querySelectorAll('.login-form').forEach(f => f.classList.remove('active'));
                document.getElementById('username-form').classList.add('active');
                
                btn.textContent = 'Create Account';
                toggleText.textContent = 'Already have an account?';
                toggleLink.textContent = 'Sign In';
            } else {
                // Switch to Sign In mode
                regFields.classList.add('hidden');
                tabsContainer.classList.remove('hidden');
                btn.textContent = 'Sign In';
                toggleText.textContent = 'New here?';
                toggleLink.textContent = 'Create an account';
            }
            document.getElementById('auth-error').textContent = '';
        };

        // Main Auth Action (Login or Register)
        document.getElementById('login-btn').onclick = async () => {
            const btn = document.getElementById('login-btn');
            const isLogin = btn.textContent === 'Sign In';
            const errorEl = document.getElementById('auth-error');
            errorEl.textContent = '';

            try {
                let result;
                if (isLogin) {
                    // Sign In Logic (Multi-method)
                    switch(this.currentLoginTab) {
                        case 'username':
                            const u = document.getElementById('username').value.trim();
                            const p = document.getElementById('password').value;
                            if (!u || !p) throw new Error('Username and Password required');
                            result = await API.login(u, p);
                            break;
                        case 'phone':
                            const ph = document.getElementById('phone').value.trim();
                            const phP = document.getElementById('phone-password').value;
                            if (!ph || !phP) throw new Error('Phone and Password required');
                            result = await API.loginByPhone(ph, phP);
                            break;
                        case 'email':
                            const em = document.getElementById('email').value.trim();
                            const emP = document.getElementById('email-password').value;
                            if (!em || !emP) throw new Error('Email and Password required');
                            result = await API.loginByEmail(em, emP);
                            break;
                    }
                } else {
                    // Sign Up Logic
                    const u = document.getElementById('username').value.trim();
                    const p = document.getElementById('password').value;
                    const ph = document.getElementById('reg-phone').value.trim();
                    const em = document.getElementById('reg-email').value.trim();

                    if (!u || !p) throw new Error('Username and Password required');
                    if (!ph && !em) throw new Error('Phone or Email required for account discovery');
                    
                    result = await API.register(u, p, ph, em);
                }

                if (result) {
                    document.getElementById('login-modal').classList.add('hidden');
                    document.getElementById('app').classList.remove('hidden');
                    this.init();
                }
            } catch (err) {
                errorEl.style.color = 'var(--danger)';
                errorEl.textContent = err.message;
            }
        };

        // Unified logout functionality
        document.getElementById('logout-btn').onclick = () => API.logout();

        // Group Modal Logic
        const newGroupBtn = document.getElementById('new-group-btn');
        const groupModal = document.getElementById('group-modal');
        const closeGroupBtn = document.getElementById('close-group-btn');
        
        newGroupBtn.onclick = () => {
            document.getElementById('group-name-input').value = '';
            document.getElementById('group-error').textContent = '';
            groupModal.classList.remove('hidden');
        };

        closeGroupBtn.onclick = () => groupModal.classList.add('hidden');

        document.getElementById('create-group-btn').onclick = async () => {
            const groupName = document.getElementById('group-name-input').value.trim();
            const errEl = document.getElementById('group-error');
            if (!groupName) return;
            
            try {
                const res = await API.createGroup(groupName, []);
                errEl.style.color = 'var(--accent-cyan)';
                errEl.textContent = 'Group created! Select it to start chatting.';
                
                // Set active target and clear modal
                setTimeout(() => {
                    groupModal.classList.add('hidden');
                    App.state.activeTarget = res.room_id;
                    App.renderChatArea(res.room_id, groupName, true);
                    App.renderContacts();
                }, 1000);
            } catch (e) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = e.message;
            }
        };

        // Profile Modal Logic
        const profileBtn = document.getElementById('profile-btn');
        const profileModal = document.getElementById('profile-modal');
        const closeProfileBtn = document.getElementById('close-profile-btn');
        
        profileBtn.onclick = () => {
            document.getElementById('profile-unique-id').textContent = API.uniqueId || 'unknown#0000';
            document.getElementById('profile-avatar-display').textContent = (API.username || '?').charAt(0).toUpperCase();
            document.getElementById('profile-username-input').value = API.username;
            document.getElementById('profile-error').textContent = '';
            profileModal.classList.remove('hidden');
        };

        closeProfileBtn.onclick = () => profileModal.classList.add('hidden');

        document.getElementById('save-username-btn').onclick = async () => {
            const newName = document.getElementById('profile-username-input').value.trim();
            const errEl = document.getElementById('profile-error');
            if (!newName) return;
            if (newName === API.username) {
                profileModal.classList.add('hidden');
                return;
            }
            
            try {
                await API.updateUsername(newName);
                document.getElementById('profile-avatar-display').textContent = newName.charAt(0).toUpperCase();
                errEl.style.color = 'var(--accent-cyan)';
                errEl.textContent = 'Username updated successfully!';
                
                // Refresh contacts to update any self-rendered objects
                setTimeout(() => {
                    profileModal.classList.add('hidden');
                }, 1500);
            } catch (e) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = e.message;
            }
        };

        // Search
        document.querySelector('.search-input').addEventListener('input', async (e) => {
            const query = e.target.value.trim();
            if (query) {
                try {
                    const results = await API.search(query);
                    this.renderSearchResults(results);
                } catch (err) {
                    console.error('Search failed:', err);
                    this.renderContacts();
                }
            } else {
                this.renderContacts();
            }
        });

        // Chat
        document.getElementById('send-btn').onclick = () => this.sendMessage();
        document.getElementById('message-input').onkeydown = (e) => {
            if (e.key === 'Enter') this.sendMessage();
            else this.sendTyping();
        };

        // UI Button Handlers
        document.getElementById('video-call-btn').onclick = () => this.startCall('video');
        document.getElementById('voice-call-btn').onclick = () => this.startCall('audio');
        document.getElementById('end-call-btn').onclick = () => this.endCall();
    },

    renderContacts() {
        const list = document.getElementById('contact-list');
        list.innerHTML = '';
        contacts.forEach(c => {
            if (c.id === (API.userId || API.username)) return; 
            const item = document.createElement('div');
            item.className = `contact-item ${currentChat && currentChat.id === c.id ? 'active' : ''}`;
            item.onclick = () => this.selectChat(c);
            
            const isOnline = c.status === 'Online';
            
            item.innerHTML = `
                <div class="avatar">
                    ${c.name[0].toUpperCase()}
                    <div class="online-indicator ${isOnline ? '' : 'offline'}"></div>
                </div>
                <div class="contact-info">
                    <div class="contact-name">${this.escapeHTML(c.name)}</div>
                    <div class="contact-last-msg">${this.escapeHTML(c.lastMsg || 'Say hi!')}</div>
                </div>
                <div class="status-tag ${isOnline ? 'online' : 'offline'}">${c.status}</div>
            `;
            list.appendChild(item);
        });
    },

    renderSearchResults(results) {
        const list = document.getElementById('contact-list');
        list.innerHTML = '';
        results.forEach(user => {
            const item = document.createElement('div');
            item.className = 'contact-item';
            item.onclick = () => this.selectChat({ id: user.id, name: user.name, lastMsg: 'Click to start chat' });
            
            const isOnline = user.status === 'Online';

            item.innerHTML = `
                <div class="avatar">
                    ${user.name[0].toUpperCase()}
                    <div class="online-indicator ${isOnline ? '' : 'offline'}"></div>
                </div>
                <div class="contact-info">
                    <div class="contact-name">${this.escapeHTML(user.name)}</div>
                    <div class="contact-last-msg">Click to start chat</div>
                </div>
                <div class="status-tag ${isOnline ? 'online' : 'offline'}">${user.status || 'Found'}</div>
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
        
        const container = document.getElementById('message-container');
        container.innerHTML = '';

        // Load messages from local session history
        if (this.messages[contact.id]) {
            this.messages[contact.id].forEach(msg => this.addMessageToUI(msg));
        }

        // Mark active item in list
        document.querySelectorAll('.contact-item').forEach(el => el.classList.remove('active'));
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

            // selectChat will clear this, so we add to memory then render
            const senderId = API.userId || API.username;
            if (!this.messages[currentChat.id]) this.messages[currentChat.id] = [];
            const msgObj = { text, senderId, timestamp: new Date() };
            this.messages[currentChat.id].push(msgObj);

            ws.send(JSON.stringify({
                type: 'message',
                to: currentChat.id,
                from: senderId,
                payload: payload
            }));

            this.addMessageToUI(msgObj);
            input.value = '';
            
            // Update sidebar for self-sent message
            const contact = contacts.find(c => c.id === currentChat.id);
            if (contact) {
                contact.lastMsg = `You: ${text}`;
                this.renderContacts();
            }
        } catch (e) {
            console.error('Send failed:', e);
            alert('Failed to send secure message: ' + e.message);
        }
    },

    addMessageToUI({ text, senderId, timestamp }) {
        const container = document.getElementById('message-container');
        const isSelf = senderId === (API.userId || API.username);
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
        const isGroup = currentChat && currentChat.id.startsWith('group-');
        
        ws.send(JSON.stringify({
            type: isGroup ? 'sfu_offer' : 'offer',
            to: isGroup ? undefined : currentChat.id,
            room_id: isGroup ? currentChat.id : undefined,
            from: API.uniqueId || API.username,
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
        ws.send(JSON.stringify({ type: 'typing', to: currentChat.id, from: API.userId || API.username }));
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

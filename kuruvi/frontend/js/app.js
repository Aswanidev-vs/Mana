let ws;
let currentChat = null;
let currentTab = 'all'; // all, groups, people
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
            // Proactively fetch keys for all direct contacts to ensure history can be decrypted
            for (const c of contacts) {
                if (!c.id.startsWith('group-')) {
                    try {
                        const pubRaw = await API.getPublicKey(c.id);
                        if (pubRaw) await Crypto.importRemoteKey(c.id, pubRaw);
                    } catch (e) {
                        console.warn('Key fetch failed for', c.id, e);
                    }
                }
            }
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
        try {
            switch (msg.type) {
                case 'message':
                case 'message_sync':
                    if (msg.messages) {
                        for (const m of msg.messages) await this.onReceiveMessage(m);
                    } else {
                        await this.onReceiveMessage(msg);
                    }
                    break;
                case 'key_exchange':
                    if (msg.payload) {
                        const rawKey = new Uint8Array(atob(msg.payload).split('').map(c => c.charCodeAt(0)));
                        await Crypto.importRemoteKey(msg.from, rawKey);
                        console.log('Received public key from', msg.from);
                    }
                    break;
                case 'call_start':
                    console.log('Received Call Invite from', msg.from);
                    this.onReceiveCallInvite(msg);
                    break;
                case 'offer':
                case 'sfu_offer':
                    this.onReceiveCallOffer(msg);
                    break;
                case 'answer':
                case 'sfu_answer':
                    console.log('Received RTC Answer');
                    const rewrittenSdp = this.rewriteSDP(msg.sdp);
                    RTC.handleAnswer(rewrittenSdp);
                    break;
                case 'candidate':
                case 'sfu_candidate':
                    let candidate = msg.candidate;
                    if (typeof candidate === 'string') candidate = JSON.parse(candidate);
                    if (candidate && candidate.candidate) {
                        candidate.candidate = this.rewriteICECandidate(candidate.candidate);
                    }
                    RTC.addIceCandidate(candidate);
                    break;
                case 'hangup':
                    console.log('Received Hangup');
                    this.endCall(false); // Don't send hangup back to avoid loops
                    break;
                case 'track_added':
                    console.log('SFU: New track published');
                    ws.send(JSON.stringify({
                        type: 'subscribe',
                        from: API.userId || API.username,
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
        } catch (err) {
            console.error('Error handling WebSocket message:', err, msg);
        }
    },

    async onReceiveMessage(msg) {
        let text = '';
        const senderId = msg.sender_id || msg.from;
        const myId = API.userId || API.username;
        const isSelf = senderId === myId;
        
        // Resolve which Chat/Conversation this belongs to
        // For groups, use room_id. For private, use the *other* person's ID.
        let chatId = msg.room_id || (isSelf ? (msg.target_id || msg.to) : senderId);
        if (!chatId) {
            console.warn('Could not resolve ChatID for message:', msg);
            return;
        }

        try {
            if (msg.payload) {
                // Decode Base64 string to Uint8Array first!
                const binaryString = atob(msg.payload);
                const data = new Uint8Array(binaryString.length);
                for (let i = 0; i < binaryString.length; i++) data[i] = binaryString.charCodeAt(i);

                // Attempt decryption if not self-sent (self-sent are usually plaintext in local sync or already handled)
                try {
                    text = await Crypto.decrypt(data, senderId);
                } catch (err) {
                    // Fallback to direct decoding
                    const decoded = new TextDecoder().decode(data);
                    // Check if it's likely plaintext or failed binary
                    if (/[^\x20-\x7E\n\r]/.test(decoded)) throw new Error("Binary content");
                    text = decoded;
                }
            } else {
                text = msg.text || '';
            }
        } catch (e) {
            console.error('Decryption/Decoding failed for msg from', senderId, e);
            text = "[Message Content Error]";
        }

        // 1. Storage: Save to local session memory
        if (!this.messages[chatId]) this.messages[chatId] = [];
        const msgObj = { text, senderId, timestamp: new Date() };
        this.messages[chatId].push(msgObj);

        // 2. Update/Add Contact in Sidebar
        let contact = contacts.find(c => c.id === chatId);
        if (!contact && !msg.room_id) {
            // Add new private contact
            contact = { id: chatId, name: chatId.replace(/^u-/, ''), lastMsg: text };
            contacts.unshift(contact);
            API.addContact(chatId).catch(console.error);
        } else if (contact) {
            contact.lastMsg = text;
            contacts = [contact, ...contacts.filter(c => c.id !== chatId)];
        }
        this.renderContacts();

        // 3. Render to Chat UI if this is the active chat
        if (currentChat && currentChat.id === chatId) {
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
                const icon = btn.querySelector('i, svg'); // Handle Lucide replacement
                if (!pInput || !icon) return;

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
        };

        // Account Deletion
        document.getElementById('delete-account-btn').onclick = () => this.confirmDeleteAccount();
        document.getElementById('confirm-cancel').onclick = () => document.getElementById('confirm-modal').classList.add('hidden');
        document.getElementById('confirm-execute').onclick = () => this.executeDeleteAccount();

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
                regFields.classList.remove('hidden');
                tabsContainer.classList.add('hidden');
                this.currentLoginTab = 'username';
                document.querySelectorAll('.login-tab').forEach(t => t.classList.remove('active'));
                document.querySelector('[data-tab="username"]').classList.add('active');
                document.querySelectorAll('.login-form').forEach(f => f.classList.remove('active'));
                document.getElementById('username-form').classList.add('active');
                
                btn.textContent = 'Create Account';
                toggleText.textContent = 'Already have an account?';
                toggleLink.textContent = 'Sign In';
            } else {
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
                    const u = document.getElementById('username').value.trim();
                    const p = document.getElementById('password').value;
                    const ph = document.getElementById('reg-phone').value.trim();
                    const em = document.getElementById('reg-email').value.trim();

                    if (!u || !p) throw new Error('Username and Password required');
                    if (!ph && !em) throw new Error('Phone or Email required for account discovery');
                    
                    result = await API.register(u, p, ph, em);
                }

                if (result) {
                    this.showApp();
                }
            } catch (err) {
                errorEl.style.color = 'var(--danger)';
                errorEl.textContent = err.message;
            }
        };

        const logoutBtn = document.getElementById('logout-btn');
        if (logoutBtn) logoutBtn.onclick = () => API.logout();

        // Group Modal
        const groupModal = document.getElementById('group-modal');
        document.getElementById('new-group-btn').onclick = () => {
            document.getElementById('group-name-input').value = '';
            document.getElementById('group-error').textContent = '';
            groupModal.classList.remove('hidden');
        };
        document.getElementById('close-group-btn').onclick = () => groupModal.classList.add('hidden');
        document.getElementById('create-group-btn').onclick = async () => {
            const groupName = document.getElementById('group-name-input').value.trim();
            const errEl = document.getElementById('group-error');
            if (!groupName) return;
            try {
                const res = await API.createGroup(groupName, []);
                errEl.style.color = 'var(--accent-cyan)';
                errEl.textContent = 'Group created!';
                setTimeout(() => {
                    groupModal.classList.add('hidden');
                    this.selectChat({ id: res.room_id, name: groupName, lastMsg: 'Group created' });
                }, 1000);
            } catch (e) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = e.message;
            }
        };

        // Profile Modal
        const profileModal = document.getElementById('profile-modal');
        document.getElementById('profile-btn').onclick = () => {
            document.getElementById('profile-unique-id').textContent = API.uniqueId || 'unknown#0000';
            document.getElementById('profile-avatar-display').textContent = (API.username || '?').charAt(0).toUpperCase();
            document.getElementById('profile-username-input').value = API.username;
            document.getElementById('profile-error').textContent = '';
            profileModal.classList.remove('hidden');
        };
        document.getElementById('close-profile-btn').onclick = () => profileModal.classList.add('hidden');
        document.getElementById('cancel-profile-btn').onclick = () => profileModal.classList.add('hidden');
        
        const saveBtnHandler = async () => {
            const newName = document.getElementById('profile-username-input').value.trim();
            const errEl = document.getElementById('profile-error');
            if (!newName || newName === API.username) {
                profileModal.classList.add('hidden');
                return;
            }
            try {
                await API.updateUsername(newName);
                document.getElementById('profile-avatar-display').textContent = newName.charAt(0).toUpperCase();
                errEl.style.color = 'var(--accent-cyan)';
                errEl.textContent = 'Username updated!';
                setTimeout(() => profileModal.classList.add('hidden'), 1500);
            } catch (e) {
                errEl.style.color = 'var(--danger)';
                errEl.textContent = e.message;
            }
        };

        document.getElementById('save-username-btn').onclick = saveBtnHandler;
        document.getElementById('save-profile-btn').onclick = saveBtnHandler;

        // Sidebar Tabs
        document.querySelectorAll('.sidebar-tab').forEach(tab => {
            tab.onclick = () => {
                document.querySelectorAll('.sidebar-tab').forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                currentTab = tab.dataset.tab;
                this.renderContacts();
            };
        });

        // Search
        document.querySelector('.search-input').addEventListener('input', async (e) => {
            const query = e.target.value.trim();
            if (query) {
                try {
                    const results = await API.search(query);
                    this.renderSearchResults(results);
                } catch (err) {
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

        // Media
        const mediaInput = document.getElementById('media-input');
        const attachBtn = document.getElementById('attach-btn');
        if (attachBtn && mediaInput) {
            attachBtn.onclick = () => mediaInput.click();
            mediaInput.onchange = async () => {
                if (!mediaInput.files.length || !currentChat) return;
                try {
                    attachBtn.innerHTML = '<i data-lucide="loader-2" class="animate-spin"></i>';
                    lucide.createIcons({ attrs: { 'data-lucide': 'loader-2' }, nameAttr: 'data-lucide', root: attachBtn });
                    const { url } = await API.uploadMedia(mediaInput.files[0]);
                    await this.sendRawMessage(url);
                    mediaInput.value = '';
                } catch (err) {
                    alert('Upload failed: ' + err.message);
                } finally {
                    attachBtn.innerHTML = '<i data-lucide="plus"></i>';
                    lucide.createIcons({ attrs: { 'data-lucide': 'plus' }, nameAttr: 'data-lucide', root: attachBtn });
                }
            };
        }

        document.getElementById('video-call-btn').onclick = () => this.startCall('video');
        document.getElementById('voice-call-btn').onclick = () => this.startCall('audio');
        document.getElementById('screenshare-btn').onclick = () => this.toggleScreenshare();
        document.getElementById('end-call-btn').onclick = () => this.endCall();

        // Clear Chat
        const clearChatBtn = document.getElementById('clear-chat-btn');
        if (clearChatBtn) {
            clearChatBtn.onclick = async () => {
                if (!currentChat) return;
                if (!confirm(`Are you sure you want to permanently delete your chat history with ${currentChat.name}?`)) return;
                
                try {
                    await API.deleteHistory(currentChat.id);
                    this.messages[currentChat.id] = [];
                    document.getElementById('message-container').innerHTML = `<div style="text-align:center; opacity:0.5; padding:20px; display:flex; align-items:center; justify-content:center; gap:8px;"><i data-lucide="lock" style="width:14px; height:14px;"></i> End-to-End Encrypted</div>`;
                    lucide.createIcons();
                    // Find contact and clear last message in UI
                    const contact = contacts.find(c => c.id === currentChat.id);
                    if (contact) {
                        contact.lastMsg = "";
                        this.renderContacts();
                    }
                } catch (err) {
                    alert('Failed to clear chat: ' + err.message);
                }
            };
        }

        // Responsive Mobile Back Button
        document.getElementById('back-to-sidebar').onclick = () => {
            document.getElementById('app').classList.remove('chat-open');
        };

        // In-call controls: Mic Toggle
        document.getElementById('toggle-mic').onclick = () => {
            if (!RTC.localStream) return;
            const audioTrack = RTC.localStream.getAudioTracks()[0];
            if (!audioTrack) return;
            audioTrack.enabled = !audioTrack.enabled;
            const btn = document.getElementById('toggle-mic');
            const icon = btn.querySelector('i');
            if (audioTrack.enabled) {
                icon.setAttribute('data-lucide', 'mic');
                btn.style.background = '';
                btn.style.color = '';
            } else {
                icon.setAttribute('data-lucide', 'mic-off');
                btn.style.background = 'var(--danger)';
                btn.style.color = 'white';
            }
            lucide.createIcons();
        };

        // In-call controls: Camera Toggle
        document.getElementById('toggle-video').onclick = () => {
            if (!RTC.localStream) return;
            const videoTrack = RTC.localStream.getVideoTracks()[0];
            if (!videoTrack) return;
            videoTrack.enabled = !videoTrack.enabled;
            const btn = document.getElementById('toggle-video');
            const icon = btn.querySelector('i');
            if (videoTrack.enabled) {
                icon.setAttribute('data-lucide', 'video');
                btn.style.background = '';
                btn.style.color = '';
            } else {
                icon.setAttribute('data-lucide', 'video-off');
                btn.style.background = 'var(--danger)';
                btn.style.color = 'white';
            }
            lucide.createIcons();
        };

        // Group Management
        document.getElementById('manage-group-btn-header').onclick = () => this.showManageGroupModal();
        document.getElementById('close-manage-group-btn').onclick = () => document.getElementById('manage-group-modal').classList.add('hidden');
        document.getElementById('add-member-btn').onclick = () => this.addGroupMember();
        document.getElementById('leave-group-btn').onclick = () => this.leaveGroup();
        document.getElementById('delete-group-btn').onclick = () => this.deleteGroup();
    },

    renderContacts() {
        const list = document.getElementById('contact-list');
        list.innerHTML = '';
        
        let filtered = contacts;
        if (currentTab === 'groups') {
            filtered = contacts.filter(c => c.id.startsWith('group-'));
        } else if (currentTab === 'people') {
            filtered = contacts.filter(c => !c.id.startsWith('group-'));
        }

        filtered.forEach(c => {
            if (c.id === (API.userId || API.username)) return; 
            const item = document.createElement('div');
            item.className = `contact-item ${currentChat && currentChat.id === c.id ? 'active' : ''}`;
            item.onclick = () => this.selectChat(c);
            const isOnline = c.status === 'Online';
            item.innerHTML = `
                <div class="avatar">${c.name[0].toUpperCase()}<div class="online-indicator ${isOnline ? '' : 'offline'}"></div></div>
                <div class="contact-info">
                    <div class="contact-name">${this.escapeHTML(c.name)}</div>
                    <div class="contact-last-msg">${this.escapeHTML(c.lastMsg || 'Say hi!')}</div>
                </div>
                <div class="status-tag ${isOnline ? 'online' : 'offline'}">${this.escapeHTML(c.status || 'Offline')}</div>
            `;
            list.appendChild(item);
        });
        lucide.createIcons();
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
                <div class="avatar">${user.name[0].toUpperCase()}<div class="online-indicator ${isOnline ? '' : 'offline'}"></div></div>
                <div class="contact-info">
                    <div class="contact-name">${this.escapeHTML(user.name)}</div>
                    <div class="contact-last-msg">Click to start chat</div>
                </div>
                <div class="status-tag ${isOnline ? 'online' : 'offline'}">${user.status || 'Found'}</div>
            `;
            list.appendChild(item);
        });
    },

    async selectChat(contact) {
        currentChat = contact;
        document.getElementById('no-chat-state').classList.add('hidden');
        document.getElementById('chat-active').classList.remove('hidden');
        document.getElementById('app').classList.add('chat-open'); // for mobile responsiveness
        document.getElementById('active-chat-name').textContent = contact.name;
        document.getElementById('active-chat-avatar').textContent = contact.name[0];
        
        // Group Admin Button visibility
        const manageBtn = document.getElementById('manage-group-btn-header');
        if (contact.id.startsWith('group-')) {
            manageBtn.classList.remove('hidden');
        } else {
            manageBtn.classList.add('hidden');
        }

        const container = document.getElementById('message-container');
        container.innerHTML = `<div style="text-align:center; opacity:0.5; padding:20px; display:flex; align-items:center; justify-content:center; gap:8px;"><i data-lucide="lock" style="width:14px; height:14px;"></i> End-to-End Encrypted</div>`;
        lucide.createIcons();

        // Always re-fetch history from server (handles refresh case)
        try {
            const history = await API.getHistory(contact.id);
            this.messages[contact.id] = [];
            for (const m of history) {
                const text = await this.decryptPayload(m.payload, m.sender_id);
                this.messages[contact.id].push({ text, senderId: m.sender_id, timestamp: new Date(m.timestamp) });
            }
        } catch (err) {
            console.error('History failed:', err);
        }
        if (this.messages[contact.id]) {
            this.messages[contact.id].forEach(msg => this.addMessageToUI(msg));
        }
    },

    async decryptPayload(payloadBase64, senderId) {
        // Groups use Base64-encoded plaintext (not E2EE)
        const isGroupContext = (currentChat && currentChat.id.startsWith('group-')) || 
                               (senderId && senderId.startsWith('group-'));
        if (isGroupContext) {
            try { return decodeURIComponent(escape(atob(payloadBase64))); } catch (e) { 
                try { return atob(payloadBase64); } catch(e2) { return "[Message]"; }
            }
        }

        // For E2EE: determine the correct peer for ECDH key derivation
        // If senderId is ourselves, the peer is the chat partner (currentChat.id)
        // If senderId is someone else, the peer is the sender
        const selfId = API.userId || API.username;
        const peerId = (senderId === selfId && currentChat) ? currentChat.id : senderId;

        try {
            const binaryString = atob(payloadBase64);
            const data = new Uint8Array(binaryString.length);
            for (let i = 0; i < binaryString.length; i++) data[i] = binaryString.charCodeAt(i);
            
            // Try fetching peer's key if missing
            if (!Crypto.remoteKeys.has(peerId)) {
                const pubRaw = await API.getPublicKey(peerId);
                if (pubRaw) await Crypto.importRemoteKey(peerId, pubRaw);
            }

            return await Crypto.decrypt(data, peerId);
        } catch (e) {
            // Fallback: try plain Base64 decode for unencrypted or legacy messages
            try {
                return decodeURIComponent(escape(atob(payloadBase64)));
            } catch (err) {
                try { return atob(payloadBase64); } catch(e2) {
                    return "[Encrypted Message]";
                }
            }
        }
    },

    async sendMessage() {
        const input = document.getElementById('message-input');
        const text = input.value.trim();
        if (!text) return;
        await this.sendRawMessage(text);
        input.value = '';
    },

    async sendRawMessage(text) {
        if (!currentChat || !ws || ws.readyState !== WebSocket.OPEN) return;
        try {
            let payload;
            const isGroup = currentChat.id.startsWith('group-');

            if (isGroup) {
                // Group messages: Base64-encode plaintext (broadcast to room)
                payload = btoa(unescape(encodeURIComponent(text)));
            } else {
                // Direct messages: E2EE
                if (!Crypto.remoteKeys.has(currentChat.id)) {
                    const remotePubRaw = await API.getPublicKey(currentChat.id);
                    if (remotePubRaw) await Crypto.importRemoteKey(currentChat.id, remotePubRaw);
                    else { console.error('No public key for', currentChat.id); return; }
                }
                const encrypted = await Crypto.encrypt(text, currentChat.id);
                const binaryString = Array.from(encrypted).map(b => String.fromCharCode(b)).join('');
                payload = btoa(binaryString);
            }

            const senderId = API.userId || API.username;
            if (!this.messages[currentChat.id]) this.messages[currentChat.id] = [];
            const msgObj = { text, senderId, timestamp: new Date() };
            this.messages[currentChat.id].push(msgObj);

            ws.send(JSON.stringify({
                type: 'message',
                to: isGroup ? undefined : currentChat.id,
                room_id: isGroup ? currentChat.id : undefined,
                from: senderId,
                payload
            }));

            if (!isGroup) {
                API.addContact(currentChat.id).catch(console.error);
            }
            this.addMessageToUI(msgObj);
        } catch (e) {
            console.error('Send failed:', e);
        }
    },

    addMessageToUI({ text, senderId, timestamp }) {
        const container = document.getElementById('message-container');
        const isSelf = senderId === (API.userId || API.username);
        const msgDiv = document.createElement('div');
        msgDiv.className = `message ${isSelf ? 'self' : 'received'}`;

        // Create message bubble container
        const bubbleDiv = document.createElement('div');
        bubbleDiv.className = 'message-bubble';

        // Default content is plain text message
        let messageContentNode = null;

        if (text.includes('/attachments/')) {
            // Resolve relative URLs strictly against the configured API backend
            let mediaUrl = text;
            if (mediaUrl.startsWith('/attachments/')) {
                const hostBase = API.baseUrl.replace('/api', '');
                mediaUrl = hostBase + mediaUrl;
            }

            const fileName = text.split('/').pop();
            const ext = fileName.split('.').pop().toLowerCase();

            // Container for media content
            const mediaContainer = document.createElement('div');
            mediaContainer.className = 'media-container';

            // Helper to create a small download button/link
            const createDownloadMiniButton = () => {
                const a = document.createElement('a');
                a.href = mediaUrl;
                a.className = 'download-mini-btn';
                a.title = 'Download';
                a.setAttribute('download', fileName);
                const icon = document.createElement('i');
                icon.setAttribute('data-lucide', 'download');
                icon.style.width = '14px';
                a.appendChild(icon);
                return a;
            };

            if (['jpg','jpeg','png','gif','webp'].includes(ext)) {
                const img = document.createElement('img');
                img.src = mediaUrl;
                img.style.maxWidth = '300px';
                img.style.borderRadius = '8px';
                img.style.cursor = 'pointer';
                img.addEventListener('click', () => {
                    window.open(mediaUrl, '_blank');
                });

                mediaContainer.appendChild(img);
                mediaContainer.appendChild(createDownloadMiniButton());
                messageContentNode = mediaContainer;
            } else if (['mp4','webm','ogg'].includes(ext)) {
                const video = document.createElement('video');
                video.src = mediaUrl;
                video.controls = true;
                video.style.maxWidth = '300px';
                video.style.borderRadius = '8px';

                mediaContainer.appendChild(video);
                mediaContainer.appendChild(createDownloadMiniButton());
                messageContentNode = mediaContainer;
            } else if (['mp3','wav','m4a','aac'].includes(ext)) {
                const audio = document.createElement('audio');
                audio.src = mediaUrl;
                audio.controls = true;
                audio.style.width = '250px';

                mediaContainer.appendChild(audio);
                mediaContainer.appendChild(createDownloadMiniButton());
                messageContentNode = mediaContainer;
            } else {
                // Generic file download layout
                const wrapper = document.createElement('div');
                wrapper.style.display = 'flex';
                wrapper.style.alignItems = 'center';
                wrapper.style.gap = '8px';

                const link = document.createElement('a');
                link.href = mediaUrl;
                link.target = '_blank';
                link.className = 'attachment-link';
                link.style.display = 'flex';
                link.style.alignItems = 'center';
                link.style.gap = '8px';
                link.style.color = 'var(--accent-cyan)';
                link.style.background = 'rgba(255,255,255,0.05)';
                link.style.padding = '10px';
                link.style.borderRadius = '8px';
                link.style.border = '1px solid var(--border)';
                link.style.flex = '1';

                const fileIcon = document.createElement('i');
                fileIcon.setAttribute('data-lucide', 'file-text');

                const fileNameSpan = document.createElement('span');
                fileNameSpan.style.overflow = 'hidden';
                fileNameSpan.style.textOverflow = 'ellipsis';
                fileNameSpan.style.whiteSpace = 'nowrap';
                fileNameSpan.style.maxWidth = '200px';
                fileNameSpan.textContent = fileName;

                link.appendChild(fileIcon);
                link.appendChild(fileNameSpan);

                const downloadBtn = document.createElement('a');
                downloadBtn.href = mediaUrl;
                downloadBtn.className = 'btn-icon';
                downloadBtn.style.background = 'var(--bg-surface)';
                downloadBtn.style.padding = '8px';
                downloadBtn.setAttribute('download', fileName);
                const downloadIcon = document.createElement('i');
                downloadIcon.setAttribute('data-lucide', 'download');
                downloadBtn.appendChild(downloadIcon);

                wrapper.appendChild(link);
                wrapper.appendChild(downloadBtn);

                messageContentNode = wrapper;
            }
        }

        if (!messageContentNode) {
            // Fallback to plain text message
            const textNode = document.createElement('span');
            // Use escapeHTML to mirror existing behavior, even though we use textContent
            textNode.textContent = this.escapeHTML(text);
            messageContentNode = textNode;
        }

        bubbleDiv.appendChild(messageContentNode);
        msgDiv.appendChild(bubbleDiv);

        // Message meta (time and status)
        const metaDiv = document.createElement('div');
        metaDiv.className = 'message-meta';
        metaDiv.textContent = timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

        if (isSelf) {
            const space = document.createTextNode(' ');
            metaDiv.appendChild(space);
            const statusIcon = document.createElement('i');
            statusIcon.setAttribute('data-lucide', 'check-check');
            statusIcon.style.width = '14px';
            statusIcon.style.color = 'var(--accent-cyan)';
            metaDiv.appendChild(statusIcon);
        }

        msgDiv.appendChild(metaDiv);
        container.appendChild(msgDiv);
        
        // Use requestAnimationFrame to ensure DOM is rendered before scrolling
        requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
        });
        
        lucide.createIcons();
    },

    async startCall(type) {
        this.callType = type; // 'audio' or 'video'
        const overlay = document.getElementById('call-overlay');
        const statusEl = document.getElementById('ice-status');
        
        overlay.classList.remove('hidden');
        if (statusEl) statusEl.textContent = 'Status: Initializing...';
        
        try {
            const iceConfig = [
                { urls: 'stun:stun.l.google.com:19302' },
                { 
                    urls: 'turn:relay.metered.ca:80?transport=tcp',
                    username: 'openrelayproject',
                    credential: 'openrelayproject'
                },
                {
                    urls: 'turn:relay.metered.ca:443?transport=tcp',
                    username: 'openrelayproject',
                    credential: 'openrelayproject'
                },
                {
                    urls: 'turn:openrelay.metered.ca:80?transport=tcp',
                    username: 'openrelayproject',
                    credential: 'openrelayproject'
                }
            ];

            if (statusEl) statusEl.textContent = 'Status: Setting up ICE...';
            // Use 'all' instead of 'relay' to allow the browser to use our TCP-Mux tunnel candidates.
            await RTC.init(iceConfig, 'all'); 
            
            if (statusEl) statusEl.textContent = `Status: ICE ${RTC.pc.iceConnectionState}`;
            
            RTC.onConnectionStateChange = (state) => {
                if (statusEl) statusEl.textContent = `Status: ${state}`;
                console.log(`RTC: Connection state changed to ${state}`);
                if (state === 'failed') {
                    console.error('RTC: ICE Connection Failed. Check if port 10000 is open on the tunnel.');
                }
            };

            RTC.onTrack = (stream, track) => {
                console.log('RTC: Received remote track:', track.kind);
                this.addStreamToUI('remote', stream);
            };
            
            RTC.pc.onicecandidate = (e) => {
                if (e.candidate && ws && ws.readyState === WebSocket.OPEN) {
                    const isGroup = currentChat && currentChat.id.startsWith('group-');
                    const targetId = isGroup ? undefined : currentChat.id;
                    let rid = currentChat.id;
                    if (!isGroup) {
                        const sortedIds = [API.userId, targetId].sort();
                        rid = `private-${sortedIds[0]}-${sortedIds[1]}`;
                    }

                    ws.send(JSON.stringify({
                        type: 'sfu_candidate', // Always use SFU type for tunnel mode
                        room_id: rid,
                        from: API.userId || API.username,
                        candidate: e.candidate
                    }));
                }
            };

            if (statusEl) statusEl.textContent = 'Status: Requesting Camera/Mic...';
            const stream = await RTC.getUserMedia(type === 'video', true);
            this.addStreamToUI('local', stream);
            
            if (statusEl) statusEl.textContent = 'Status: Creating Offer...';
            const offer = await RTC.createOffer();
            
            // For Kuruvi in tunnel mode, we route 1:1 calls through the SFU 
            // to take advantage of the server's TCP-Mux on port 10000.
            const isGroup = currentChat && currentChat.id.startsWith('group-');
            const targetId = isGroup ? undefined : currentChat.id;
            
            // If it's a 1:1 call, we create a temporary 'virtual room' based on the two user IDs
            // This allows the SFU to recognize the 1:1 call as a mini-room.
            let roomId = currentChat.id;
            if (!isGroup) {
                // Ensure room ID is deterministic for both parties: "call-userA-userB"
                const sortedIds = [API.userId, targetId].sort();
                roomId = `private-${sortedIds[0]}-${sortedIds[1]}`;
            }

            if (statusEl) statusEl.textContent = 'Status: Sending Signal...';
            ws.send(JSON.stringify({
                type: 'sfu_offer', // Force SFU path for TCP relay support
                to: targetId,
                room_id: roomId,
                from: API.userId || API.username,
                sdp: offer.sdp,
                call_type: type 
            }));
            
            if (statusEl) statusEl.textContent = 'Status: Waiting for Peer...';
        } catch (err) {
            console.error('Call failed:', err);
            if (statusEl) {
                statusEl.style.color = '#ff4b2b';
                statusEl.textContent = `Error: ${err.message}`;
            }
        }
        lucide.createIcons();
    },

    async onReceiveCallInvite(msg) {
        const callType = msg.call_type || (msg.payload ? atob(msg.payload) : 'video');
        const callerName = msg.from.replace(/^u-/, '');
        const callLabel = callType === 'audio' ? '📞 Audio' : '📹 Video';
        
        if (!confirm(`${callLabel} call from ${callerName}. Accept?`)) return;
        
        this.callType = callType;
        document.getElementById('call-overlay').classList.remove('hidden');
        const statusEl = document.getElementById('ice-status');
        if (statusEl) statusEl.textContent = 'Status: Accepting...';

        // Now initiate the SFU handshake
        try {
            const iceConfig = [{ urls: 'stun:stun.l.google.com:19302' }];
            // Use 'all' instead of 'relay' for TCP-Mux tunnel support
            await RTC.init(iceConfig, 'all'); 
            
            RTC.onConnectionStateChange = (state) => {
                if (statusEl) statusEl.textContent = `Status: ${state}`;
            };

            RTC.onTrack = (stream, track) => {
                this.addStreamToUI('remote', stream);
            };
            
            RTC.pc.onicecandidate = (e) => {
                if (e.candidate && ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({
                        type: 'sfu_candidate',
                        room_id: msg.room_id,
                        from: API.userId || API.username,
                        candidate: e.candidate
                    }));
                }
            };

            const stream = await RTC.getUserMedia(this.callType === 'video', true);
            this.addStreamToUI('local', stream);
            
            const offer = await RTC.createOffer();
            ws.send(JSON.stringify({
                type: 'sfu_offer',
                room_id: msg.room_id,
                from: API.userId || API.username,
                sdp: offer.sdp
            }));
        } catch (err) {
            console.error('Call initialization failed:', err);
            if (statusEl) statusEl.textContent = `Error: ${err.message}`;
        }
    },

    async onReceiveCallOffer(msg) {
        const statusEl = document.getElementById('ice-status');
        if (statusEl) statusEl.textContent = 'Status: Processing Offer...';
        
        try {
            // Check if RTC is already initialized (e.g. we accepted an invite)
            if (!RTC.pc) {
                const iceConfig = [{ urls: 'stun:stun.l.google.com:19302' }];
                // Use 'all' for TCP-Mux tunnel support
                await RTC.init(iceConfig, 'all'); 
                
                RTC.onTrack = (stream, track) => {
                    this.addStreamToUI('remote', stream);
                };

                RTC.pc.onicecandidate = (e) => {
                    if (e.candidate && ws && ws.readyState === WebSocket.OPEN) {
                        ws.send(JSON.stringify({
                            type: 'sfu_candidate',
                            room_id: msg.room_id,
                            from: API.userId || API.username,
                            candidate: e.candidate
                        }));
                    }
                };
            }

            const isSFU = msg.type === 'sfu_offer' || (msg.payload && (atob(msg.payload) === 'sfu_relay' || msg.payload === 'sfu_relay'));
            
            // Handle the SDP offer
            const rewrittenSDP = this.rewriteSDP(msg.sdp);
            const answer = await RTC.handleOffer(rewrittenSDP);
            
            ws.send(JSON.stringify({
                type: isSFU ? 'sfu_answer' : 'answer',
                to: isSFU ? undefined : msg.from,
                room_id: isSFU ? msg.room_id : undefined,
                from: API.userId || API.username,
                sdp: answer.sdp
            }));
        } catch (err) {
            console.error('Call receive failed:', err);
        }
    },

    rewriteSDP(sdp) {
        if (!sdp) return sdp;
        // The server now uses NAT 1:1 mapping for port 10000.
        // We only rewrite if we detect a local IP hiding a tunnel.
        let tunnelHost = window.location.hostname;
        if (tunnelHost.includes('-8080.')) {
            tunnelHost = tunnelHost.replace('-8080.', '-10000.');
        } else if (tunnelHost.includes(':8080')) {
            tunnelHost = tunnelHost.replace(':8080', ':10000');
        }

        const rewritten = sdp.replace(/(\d+\.\d+\.\d+\.\d+|localhost|0\.0\.0\.0|\[::1\]|\[::\]) 10000/g, `${tunnelHost} 10000`);
        if (rewritten !== sdp) console.log('RTC: Rewrote SDP to use tunnel host:', tunnelHost);
        return rewritten;
    },

    rewriteICECandidate(c) {
        if (!c || !c.includes('10000')) return c;
        
        // If it already looks like a tunnel hostname, don't touch it
        if (c.includes('.visualstudio.com') || c.includes('.app.online') || c.includes('.devtunnels.ms')) return c;

        let tunnelHost = window.location.hostname;
        if (tunnelHost.includes('-8080.')) {
            tunnelHost = tunnelHost.replace('-8080.', '-10000.');
        } else if (tunnelHost.includes(':8080')) {
            tunnelHost = tunnelHost.replace(':8080', ':10000');
        } else if (tunnelHost.includes('-8080.inc1.')) {
            tunnelHost = tunnelHost.replace('-8080.inc1.', '-10000.inc1.');
        }
        
        // Final fallback: standard IP regex replacement for port 10000
        const IP_REGEX = /(\d+\.\d+\.\d+\.\d+|localhost|0\.0\.0\.0|\[::1\]|\[::\])/g;
        const newCandidate = c.replace(IP_REGEX, tunnelHost);
        console.log(`RTC: Candidate Port 10000 (Private) detected. Mapping to: ${tunnelHost}`);
        return newCandidate;
    },

    addStreamToUI(id, stream) {
        let video = document.getElementById(`video-${id}`);
        if (!video) {
            const container = document.createElement('div');
            container.className = 'video-container';
            
            // For audio-only calls, show an avatar placeholder
            if (this.callType === 'audio') {
                container.style.cssText = 'display:flex; align-items:center; justify-content:center; min-height:200px;';
                const label = id === 'local' ? 'You' : (currentChat ? currentChat.name : 'Peer');
                container.innerHTML = `
                    <div style="text-align:center; position: relative; z-index: 5;">
                        <div style="width:80px; height:80px; border-radius:50%; background:linear-gradient(135deg, var(--accent-blue), var(--accent-cyan)); display:flex; align-items:center; justify-content:center; font-size:32px; margin:0 auto 12px; color:white;">${label[0].toUpperCase()}</div>
                        <div style="color:white; font-size:14px; font-weight: 500;">${this.escapeHTML(label)}</div>
                        <div style="color:var(--text-secondary); font-size:12px; margin-top:4px;">Audio Call</div>
                    </div>
                `;
                const audio = document.createElement('audio');
                audio.id = `video-${id}`;
                audio.autoplay = true;
                if (id === 'local') audio.muted = true;
                audio.srcObject = stream;
                container.appendChild(audio);
                document.getElementById('video-grid').appendChild(container);
                return;
            }
            
            video = document.createElement('video');
            video.id = `video-${id}`;
            video.autoplay = true;
            video.playsInline = true;
            if (id === 'local') video.muted = true;
            container.appendChild(video);
            document.getElementById('video-grid').appendChild(container);
        }
        video.srcObject = stream;
        lucide.createIcons();
    },


    async toggleScreenshare() {
        const btn = document.getElementById('screenshare-btn');
        if (RTC.screenStream) {
            RTC.stopScreenShare();
            btn.classList.remove('active');
            btn.querySelector('i').setAttribute('data-lucide', 'monitor-up');
        } else {
            try {
                await RTC.startScreenShare();
                btn.classList.add('active');
                btn.querySelector('i').setAttribute('data-lucide', 'monitor-stop');
            } catch (e) {
                console.warn('Screen share failed/cancelled:', e);
            }
        }
        lucide.createIcons();
    },

    async showManageGroupModal() {
        if (!currentChat) return;
        const modal = document.getElementById('manage-group-modal');
        document.getElementById('manage-group-title').textContent = `Manage ${currentChat.name}`;
        document.getElementById('manage-group-error').textContent = '';
        modal.classList.remove('hidden');
        
        const members = await this.renderGroupMembers();
        
        // Show/hide delete button based on admin status
        const isAdmin = members && members.some(m => m.user_id === API.userId && m.role === 'admin');
        const deleteBtn = document.getElementById('delete-group-btn');
        if (isAdmin) {
            deleteBtn.classList.remove('hidden');
        } else {
            deleteBtn.classList.add('hidden');
        }

        // Update Add Member select list
        const select = document.getElementById('add-member-select');
        select.innerHTML = '<option value="">Select a contact...</option>';
        contacts.forEach(c => {
            if (!c.id.startsWith('group-')) {
                select.innerHTML += `<option value="${c.id}">${this.escapeHTML(c.name)}</option>`;
            }
        });
        
        lucide.createIcons();
    },

    async renderGroupMembers() {
        const container = document.getElementById('group-members-list');
        const adminControls = document.getElementById('admin-controls');
        container.innerHTML = '<div style="text-align:center; opacity:0.5;">Loading...</div>';
        
        try {
            const members = await API.getGroupMembers(currentChat.id);
            container.innerHTML = '';
            
            const isAdmin = members.some(m => m.user_id === API.userId && m.role === 'admin');
            if (isAdmin) {
                adminControls.classList.remove('hidden');
            } else {
                adminControls.classList.add('hidden');
            }

            members.forEach(m => {
                const item = document.createElement('div');
                item.style.cssText = "display:flex; justify-content:space-between; align-items:center; background:var(--bg-surface); padding:10px; border-radius:8px;";
                const isYou = m.user_id === API.userId;
                
                item.innerHTML = `
                    <div style="display:flex; align-items:center; gap:10px;">
                        <div class="avatar-mini" style="width:30px; height:30px; border-radius:50%; background:rgba(255,255,255,0.05); display:flex; align-items:center; justify-content:center; font-size:12px;">${m.username[0].toUpperCase()}</div>
                        <div>
                            <div style="font-size:14px;">${this.escapeHTML(m.username)} ${isYou ? '<span style="opacity:0.5;">(You)</span>' : ''}</div>
                            <div style="font-size:11px; opacity:0.5; color:var(--accent-cyan);">${m.role.toUpperCase()}</div>
                        </div>
                    </div>
                    ${isAdmin && !isYou ? `<button class="btn-icon" style="color:var(--danger);" onclick="App.removeGroupMember('${m.user_id}')"><i data-lucide="user-minus"></i></button>` : ''}
                `;
                container.appendChild(item);
            });
            lucide.createIcons();
            return members; // Return for caller to check admin status
        } catch (e) {
            container.innerHTML = `<div style="color:var(--danger);">Error: ${e.message}</div>`;
            return [];
        }
    },

    async addGroupMember() {
        const select = document.getElementById('add-member-select');
        const userId = select.value;
        const errEl = document.getElementById('manage-group-error');
        if (!userId) return;
        
        try {
            await API.addGroupMember(currentChat.id, userId);
            await this.renderGroupMembers();
            select.value = '';
        } catch (e) {
            errEl.textContent = e.message;
        }
    },

    async removeGroupMember(userId) {
        if (!confirm('Remove this member?')) return;
        try {
            await API.removeGroupMember(currentChat.id, userId);
            await this.renderGroupMembers();
        } catch (e) {
            alert('Failed: ' + e.message);
        }
    },

    async leaveGroup() {
        if (!currentChat) return;
        if (!confirm(`Are you sure you want to leave "${currentChat.name}"?`)) return;
        
        const errEl = document.getElementById('manage-group-error');
        try {
            await API.leaveGroup(currentChat.id);
            // Close modal and remove group from sidebar
            document.getElementById('manage-group-modal').classList.add('hidden');
            contacts = contacts.filter(c => c.id !== currentChat.id);
            currentChat = null;
            document.getElementById('chat-active').classList.add('hidden');
            document.getElementById('no-chat-state').classList.remove('hidden');
            this.renderContacts();
        } catch (e) {
            errEl.textContent = e.message;
        }
    },

    async deleteGroup() {
        if (!currentChat) return;
        if (!confirm(`Permanently delete "${currentChat.name}" and remove all members? This cannot be undone.`)) return;
        
        const errEl = document.getElementById('manage-group-error');
        try {
            await API.deleteGroup(currentChat.id);
            // Close modal and remove group from sidebar
            document.getElementById('manage-group-modal').classList.add('hidden');
            contacts = contacts.filter(c => c.id !== currentChat.id);
            currentChat = null;
            document.getElementById('chat-active').classList.add('hidden');
            document.getElementById('no-chat-state').classList.remove('hidden');
            this.renderContacts();
        } catch (e) {
            errEl.textContent = e.message;
        }
    },

      endCall(notifyPeer = true) {
        // Send hangup signal to remote peer if needed
        if (notifyPeer && ws && ws.readyState === WebSocket.OPEN && currentChat) {
            ws.send(JSON.stringify({
                type: 'hangup',
                to: currentChat.id,
                from: API.userId || API.username
            }));
        }

        // Stop screen share first (before closing PC)
        if (RTC.screenStream) {
            RTC.screenStream.getTracks().forEach(t => t.stop());
            RTC.screenStream = null;
        }
        RTC.close();
        
        // Reset call controls - setting innerHTML is safer than modifying existing icon elements
        // which Lucide may have already replaced with SVG
        const screenshareBtn = document.getElementById('screenshare-btn');
        if (screenshareBtn) {
            screenshareBtn.classList.remove('active');
            screenshareBtn.innerHTML = '<i data-lucide="monitor-up"></i>';
        }

        const micBtn = document.getElementById('toggle-mic');
        if (micBtn) {
            micBtn.classList.remove('muted');
            micBtn.innerHTML = '<i data-lucide="mic"></i>';
            micBtn.style.background = '';
            micBtn.style.color = '';
        }

        const vidBtn = document.getElementById('toggle-video');
        if (vidBtn) {
            vidBtn.classList.remove('muted');
            vidBtn.innerHTML = '<i data-lucide="video"></i>';
            vidBtn.style.background = '';
            vidBtn.style.color = '';
        }

        document.getElementById('call-overlay').classList.add('hidden');
        document.getElementById('video-grid').innerHTML = '';
        lucide.createIcons();
    },

    confirmDeleteAccount() { document.getElementById('confirm-modal').classList.remove('hidden'); },

    async executeDeleteAccount() {
        const btn = document.getElementById('confirm-execute');
        btn.disabled = true; btn.textContent = 'Deleting...';
        try {
            await API.deleteAccount();
            alert('Account deleted.');
            API.logout();
        } catch (err) {
            alert('Failed: ' + err.message);
            btn.disabled = false; btn.textContent = 'Delete Forever';
        }
    },

    sendTyping() { if (currentChat && ws) ws.send(JSON.stringify({ type: 'typing', to: currentChat.id, from: API.userId || API.username })); },

    showTyping(userId) {
        const el = document.getElementById('typing-indicator');
        el.textContent = `${userId} is typing...`;
        clearTimeout(el.timer);
        el.timer = setTimeout(() => el.textContent = '', 3000);
    },

    escapeHTML(str) { const p = document.createElement('p'); p.textContent = str; return p.innerHTML; }
};

window.onload = () => App.init();

const API = {
    // Robust baseUrl: if running via file:// or different port, fallback to localhost:8080
    baseUrl: (window.location.origin.startsWith('http') ? window.location.origin : 'http://localhost:8080') + '/api',
    token: localStorage.getItem('kuruvi_token'),
    username: localStorage.getItem('kuruvi_username'),
    userId: localStorage.getItem('kuruvi_user_id'),
    uniqueId: localStorage.getItem('kuruvi_unique_id'),

    async register(username, password, phone = '', email = '') {
        const res = await fetch(`${this.baseUrl}/auth/register`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password, phone, email })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.saveAuth(data);
        return data;
    },

    async resetPassword(identifier, newPassword) {
        const res = await fetch(`${this.baseUrl}/auth/reset-password`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ identifier, new_password: newPassword })
        });
        if (!res.ok) throw new Error(await res.text());
        return await res.json();
    },

    async login(username, password) {
        const res = await fetch(`${this.baseUrl}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.saveAuth(data);
        return data;
    },

    async loginByPhone(phone, password) {
        const res = await fetch(`${this.baseUrl}/auth/login/phone`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ phone, password })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.saveAuth(data);
        return data;
    },

    async loginByEmail(email, password) {
        const res = await fetch(`${this.baseUrl}/auth/login/email`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.saveAuth(data);
        return data;
    },

    saveAuth(data) {
        this.token = data.token;
        this.username = data.username;
        this.userId = data.user_id;
        this.uniqueId = data.unique_id;
        localStorage.setItem('kuruvi_token', data.token);
        localStorage.setItem('kuruvi_username', data.username);
        if (data.user_id) {
            localStorage.setItem('kuruvi_user_id', data.user_id);
        }
        if (data.unique_id) {
            localStorage.setItem('kuruvi_unique_id', data.unique_id);
        }
    },

    logout() {
        this.token = null;
        this.username = null;
        this.userId = null;
        this.uniqueId = null;
        localStorage.removeItem('kuruvi_token');
        localStorage.removeItem('kuruvi_username');
        localStorage.removeItem('kuruvi_user_id');
        localStorage.removeItem('kuruvi_unique_id');
        window.location.reload();
    },

    async updateUsername(newUsername) {
        const res = await fetch(`${this.baseUrl}/auth/username`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${this.token}`
            },
            body: JSON.stringify({ new_username: newUsername })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        
        // Refresh local cache with new username
        this.username = data.username;
        localStorage.setItem('kuruvi_username', data.username);
        return data;
    },

    async getProfile(userId) {
        const res = await fetch(`${this.baseUrl}/profile?user_id=${userId}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        return res.json();
    },

    async getPublicKey(userId) {
        const res = await fetch(`${this.baseUrl}/e2ee/pubkey?user_id=${userId}`);
        if (!res.ok) return null;
        const data = await res.json();
        
        // Decode Base64 string from Go JSON response into Uint8Array
        try {
            const binaryString = atob(data.public_key);
            const bytes = new Uint8Array(binaryString.length);
            for (let i = 0; i < binaryString.length; i++) {
                bytes[i] = binaryString.charCodeAt(i);
            }
            return bytes;
        } catch (e) {
            console.error('Failed to decode public key from server:', e);
            return null;
        }
    },

    async getContacts() {
        const res = await fetch(`${this.baseUrl}/contacts`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) {
            console.warn('getContacts failed:', res.status, res.statusText);
            return [];
        }
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    },

    async uploadPublicKey(userId, pubKey) {
        // Encode Uint8Array to Base64 string for Go []byte JSON unmarshaling
        const binaryString = Array.from(pubKey).map(b => String.fromCharCode(b)).join('');
        const base64Key = btoa(binaryString);

        await fetch(`${this.baseUrl}/e2ee/pubkey?user_id=${userId}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ public_key: base64Key })
        });
    },

    async createGroup(groupName, memberIds) {
        const res = await fetch(`${this.baseUrl}/group/create`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${this.token}`
            },
            body: JSON.stringify({ group_name: groupName, members: memberIds })
        });
        if (!res.ok) throw new Error(await res.text());
        return await res.json();
    },

    async getGroupMembers(roomId) {
        const res = await fetch(`${this.baseUrl}/group/members?room_id=${roomId}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) throw new Error(await res.text());
        return await res.json();
    },

    async addGroupMember(roomId, userId) {
        const res = await fetch(`${this.baseUrl}/group/members?room_id=${roomId}`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${this.token}`
            },
            body: JSON.stringify({ user_id: userId })
        });
        if (!res.ok) throw new Error(await res.text());
    },

    async removeGroupMember(roomId, userId) {
        const res = await fetch(`${this.baseUrl}/group/members?room_id=${roomId}&user_id=${userId}`, {
            method: 'DELETE',
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) throw new Error(await res.text());
    },

    async search(query) {
        const res = await fetch(`${this.baseUrl}/search?q=${encodeURIComponent(query)}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) return [];
        return await res.json();
    },

    async deleteGroup(roomId) {
        const res = await fetch(`${this.baseUrl}/group/delete?room_id=${roomId}`, {
            method: 'DELETE',
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) throw new Error(await res.text());
        return await res.json();
    },

    async leaveGroup(roomId) {
        const res = await fetch(`${this.baseUrl}/group/leave?room_id=${roomId}`, {
            method: 'POST',
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) throw new Error(await res.text());
        return await res.json();
    },

    async getHistory(contactId) {
        const res = await fetch(`${this.baseUrl}/messages/history?contact_id=${encodeURIComponent(contactId)}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) return [];
        return await res.json();
    },

    async addContact(contactId) {
        const res = await fetch(`${this.baseUrl}/contacts/add`, {
            method: 'POST',
            headers: { 
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ contact_id: contactId })
        });
        if (!res.ok) throw new Error(await res.text());
    },

    async uploadMedia(file) {
        const formData = new FormData();
        formData.append('file', file);
        const res = await fetch(`${this.baseUrl}/upload`, {
            method: 'POST',
            headers: { 'Authorization': `Bearer ${this.token}` },
            body: formData
        });
        if (!res.ok) throw new Error('Upload failed');
        return res.json();
    },

    async deleteAccount() {
        const res = await fetch(`${this.baseUrl}/auth/delete`, {
            method: 'DELETE',
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        if (!res.ok) throw new Error('Account deletion failed');
        return res.json();
    }
};

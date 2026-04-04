const API = {
    // Robust baseUrl: if running via file:// or different port, fallback to localhost:8080
    baseUrl: (window.location.origin.startsWith('http') ? window.location.origin : 'http://localhost:8080') + '/api',
    token: localStorage.getItem('kuruvi_token'),
    username: localStorage.getItem('kuruvi_username'),

    async register(username, password) {
        const res = await fetch(`${this.baseUrl}/auth/register`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.saveAuth(data);
        return data;
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

    saveAuth(data) {
        this.token = data.token;
        this.username = data.username;
        localStorage.setItem('kuruvi_token', data.token);
        localStorage.setItem('kuruvi_username', data.username);
    },

    logout() {
        this.token = null;
        this.username = null;
        localStorage.removeItem('kuruvi_token');
        localStorage.removeItem('kuruvi_username');
        window.location.reload();
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
        if (!res.ok) return [];
        return await res.json();
    },

    async uploadPublicKey(userId, pubKey) {
        await fetch(`${this.baseUrl}/e2ee/pubkey?user_id=${userId}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ public_key: Array.from(pubKey) })
        });
    }
};


const Crypto = {
    keyPair: null,
    remoteKeys: new Map(), // peerID -> CryptoKey

    async generateKeys() {
        // Check if we have existing keys in storage
        const storedKey = localStorage.getItem('kuruvi_keypair');
        if (storedKey) {
            try {
                const { public: pubB64, private: privB64 } = JSON.parse(storedKey);
                const pubRaw = new Uint8Array(atob(pubB64).split('').map(c => c.charCodeAt(0)));
                const privRaw = new Uint8Array(atob(privB64).split('').map(c => c.charCodeAt(0)));
                
                const publicKey = await crypto.subtle.importKey('raw', pubRaw, { name: 'X25519' }, true, []);
                const privateKey = await crypto.subtle.importKey('pkcs8', privRaw, { name: 'X25519' }, false, ['deriveBits', 'deriveKey']);
                
                this.keyPair = { publicKey, privateKey };
                return pubRaw;
            } catch (e) {
                console.warn('Failed to restore keys, generating new ones:', e);
            }
        }

        this.keyPair = await crypto.subtle.generateKey(
            { name: 'X25519' },
            true,
            ['deriveBits', 'deriveKey']
        );

        const pubRaw = await crypto.subtle.exportKey('raw', this.keyPair.publicKey);
        const privRaw = await crypto.subtle.exportKey('pkcs8', this.keyPair.privateKey);

        // Save to storage
        const keyPairB64 = {
            public: btoa(String.fromCharCode(...new Uint8Array(pubRaw))),
            private: btoa(String.fromCharCode(...new Uint8Array(privRaw)))
        };
        localStorage.setItem('kuruvi_keypair', JSON.stringify(keyPairB64));

        return new Uint8Array(pubRaw);
    },

    async importRemoteKey(peerID, rawKey) {
        const key = await crypto.subtle.importKey(
            'raw',
            rawKey,
            { name: 'X25519' },
            true,
            []
        );
        this.remoteKeys.set(peerID, key);
        return key;
    },

    async deriveSharedKey(remotePublicKey) {
        return crypto.subtle.deriveKey(
            { name: 'X25519', public: remotePublicKey },
            this.keyPair.privateKey,
            { name: 'AES-GCM', length: 256 },
            false,
            ['encrypt', 'decrypt']
        );
    },

    async encrypt(text, remotePeerID) {
        const remoteKey = this.remoteKeys.get(remotePeerID);
        if (!remoteKey) {
            const err = new Error('Missing public key');
            err.code = 'ERR_NO_KEY';
            throw err;
        }

        const sharedKey = await this.deriveSharedKey(remoteKey);
        const iv = crypto.getRandomValues(new Uint8Array(12));
        const encoded = new TextEncoder().encode(text);

        const ciphertext = await crypto.subtle.encrypt(
            { name: 'AES-GCM', iv },
            sharedKey,
            encoded
        );

        // Return combined iv + ciphertext
        const combined = new Uint8Array(iv.length + ciphertext.byteLength);
        combined.set(iv);
        combined.set(new Uint8Array(ciphertext), iv.length);
        return combined;
    },

    async decrypt(combinedData, remotePeerID) {
        const remoteKey = this.remoteKeys.get(remotePeerID);
        if (!remoteKey) {
            const err = new Error('Missing public key');
            err.code = 'ERR_NO_KEY';
            throw err;
        }

        const sharedKey = await this.deriveSharedKey(remoteKey);
        const data = new Uint8Array(combinedData);
        const iv = data.slice(0, 12);
        const ciphertext = data.slice(12);

        try {
            const decrypted = await crypto.subtle.decrypt(
                { name: 'AES-GCM', iv },
                sharedKey,
                ciphertext
            );
            return new TextDecoder().decode(decrypted);
        } catch (e) {
            const err = new Error('Decryption failed');
            err.code = 'ERR_DECRYPT_FAILED';
            throw err;
        }
    }
};

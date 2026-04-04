const Crypto = {
    keyPair: null,
    remoteKeys: new Map(), // peerID -> CryptoKey

    async generateKeys() {
        this.keyPair = await crypto.subtle.generateKey(
            { name: 'X25519' },
            true,
            ['deriveBits', 'deriveKey']
        );
        const pubRaw = await crypto.subtle.exportKey('raw', this.keyPair.publicKey);
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
        if (!remoteKey) throw new Error('No public key for peer');

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
        if (!remoteKey) throw new Error('No public key for peer');

        const sharedKey = await this.deriveSharedKey(remoteKey);
        const data = new Uint8Array(combinedData);
        const iv = data.slice(0, 12);
        const ciphertext = data.slice(12);

        const decrypted = await crypto.subtle.decrypt(
            { name: 'AES-GCM', iv },
            sharedKey,
            ciphertext
        );

        return new TextDecoder().decode(decrypted);
    }
};

package core

import "time"

// Config holds the configuration for a Mana application.
type Config struct {
	// Port is the HTTP server listen port.
	Port int `json:"port"`

	// Host is the HTTP server bind address.
	Host string `json:"host"`

	// EnableRTC enables WebRTC functionality.
	EnableRTC bool `json:"enable_rtc"`

	// EnableE2EE enables end-to-end encryption key exchange facilitation.
	EnableE2EE bool `json:"enable_e2ee"`

	// ReadBufferSize sets the WebSocket read buffer size.
	ReadBufferSize int `json:"read_buffer_size"`

	// WriteBufferSize sets the WebSocket write buffer size.
	WriteBufferSize int `json:"write_buffer_size"`

	// MaxMessageSize is the maximum WebSocket message size in bytes.
	// Messages exceeding this limit will be rejected.
	MaxMessageSize int64 `json:"max_message_size"`

	// EnableAuth enables JWT authentication on WebSocket connections.
	// When true, clients must provide a valid JWT token to connect.
	EnableAuth bool `json:"enable_auth"`

	// JWTSecret is the secret key for JWT signing.
	// Must be at least 32 bytes for production use.
	JWTSecret string `json:"-"`

	// JWTIssuer is the JWT issuer claim.
	JWTIssuer string `json:"jwt_issuer"`

	// JWTExpiry is the token validity duration.
	JWTExpiry time.Duration `json:"jwt_expiry"`

	// STUNServers is the list of STUN/TURN server URLs for WebRTC.
	STUNServers []string `json:"stun_servers"`

	// GracefulShutdownTimeout is the max time to wait for connections to drain.
	GracefulShutdownTimeout time.Duration `json:"graceful_shutdown_timeout"`

	// AllowedOrigins is the list of allowed WebSocket origins.
	// If empty, all origins are rejected (secure default).
	// Use ["*"] to allow all origins (not recommended for production).
	AllowedOrigins []string `json:"allowed_origins"`

	// RateLimitPerSecond is the max messages per second per connection.
	// 0 means unlimited (not recommended for production).
	RateLimitPerSecond int `json:"rate_limit_per_second"`

	// RateLimitBurst is the burst capacity for rate limiting.
	RateLimitBurst int `json:"rate_limit_burst"`

	// EnableTLS enables HTTPS/WSS using TLS certificates.
	EnableTLS bool `json:"enable_tls"`

	// TLSCertFile is the path to the TLS certificate file.
	TLSCertFile string `json:"tls_cert_file"`

	// TLSKeyFile is the path to the TLS private key file.
	TLSKeyFile string `json:"tls_key_file"`

	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration `json:"read_timeout"`

	// WriteTimeout is the maximum duration before timing out writes.
	WriteTimeout time.Duration `json:"write_timeout"`

	// IdleTimeout is the maximum time to wait for the next request.
	IdleTimeout time.Duration `json:"idle_timeout"`

	// MessageStorePath enables durable file-backed message persistence when set.
	MessageStorePath string `json:"message_store_path"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Port:            8080,
		Host:            "0.0.0.0",
		EnableRTC:       false,
		EnableE2EE:      false,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		MaxMessageSize:  65536, // 64KB
		EnableAuth:      false,
		JWTSecret:       "",
		JWTIssuer:       "mana",
		JWTExpiry:       24 * time.Hour,
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},
		GracefulShutdownTimeout: 15 * time.Second,
		AllowedOrigins:          []string{},
		RateLimitPerSecond:      100,
		RateLimitBurst:          200,
		EnableTLS:               false,
		ReadTimeout:             15 * time.Second,
		WriteTimeout:            15 * time.Second,
		IdleTimeout:             60 * time.Second,
	}
}

// Validate checks the config for security issues and returns warnings.
func (c *Config) Validate() []string {
	var warnings []string

	if c.EnableAuth && len(c.JWTSecret) < 32 {
		warnings = append(warnings, "SECURITY: JWTSecret should be at least 32 bytes for production use")
	}
	if c.EnableAuth && c.JWTSecret == "" {
		warnings = append(warnings, "CRITICAL: JWTSecret is empty with auth enabled — tokens cannot be signed")
	}
	if len(c.AllowedOrigins) == 0 {
		warnings = append(warnings, "WARNING: No AllowedOrigins set — all WebSocket origins will be rejected")
	}
	if c.MaxMessageSize <= 0 {
		warnings = append(warnings, "WARNING: MaxMessageSize not set — defaulting to 64KB")
		c.MaxMessageSize = 65536
	}
	if c.EnableTLS && (c.TLSCertFile == "" || c.TLSKeyFile == "") {
		warnings = append(warnings, "CRITICAL: TLS enabled but cert/key files not provided")
	}
	if c.RateLimitPerSecond <= 0 {
		warnings = append(warnings, "WARNING: Rate limiting disabled — vulnerable to flood attacks")
	}

	return warnings
}

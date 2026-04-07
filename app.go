package mana

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Aswanidev-vs/mana/auth"
	"github.com/Aswanidev-vs/mana/cluster"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/e2ee"
	"github.com/Aswanidev-vs/mana/notification"
	"github.com/Aswanidev-vs/mana/observ"
	"github.com/Aswanidev-vs/mana/product"
	"github.com/Aswanidev-vs/mana/room"
	"github.com/Aswanidev-vs/mana/rtc"
	"github.com/Aswanidev-vs/mana/settings"
	"github.com/Aswanidev-vs/mana/signaling"
	"github.com/Aswanidev-vs/mana/social"
	"github.com/Aswanidev-vs/mana/storage"
	"github.com/Aswanidev-vs/mana/storage/db"
	"github.com/Aswanidev-vs/mana/ws"

	"github.com/pion/webrtc/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// App is the main Mana application instance.
// It orchestrates all packages — it does NOT implement business logic itself.
type App struct {
	config core.Config

	// Components (each in its own package)
	roomManager *room.Manager
	signalHub   *signaling.Hub
	rtcManager  *rtc.Manager
	callManager *rtc.CallManager
	e2eeStore   e2ee.KeyStore
	sessionMgr  *e2ee.DefaultSessionManager
	router      *signaling.Router
	wsHandler   *ws.Handler
	notifHub    *notification.Hub

	// Auth & Security
	jwtAuth     *auth.JWTAuth
	rbac        *auth.RBAC
	rateLimiter *auth.RateLimiter

	// Observability
	logger  *observ.Logger
	metrics *observ.Metrics
	store   core.MessageStore
	account core.AccountStore
	profile core.ProfileStore
	contact core.ContactStore
	device  core.DeviceStore
	prefs   core.PreferenceStore
	rooms   core.RoomStore
	product *product.Store
	tracing *observ.Tracing
	cluster cluster.Backend
	backend *db.Backend

	// Logic Hooks
	onAccountCreated func(ctx context.Context, user core.User)
	onMessageStored  func(ctx context.Context, msg core.Message)

	// Session tracking
	mu           sync.RWMutex
	sessions     map[string]*room.UserSession
	userSessions map[string]map[string]*room.UserSession
	deviceCursor map[string]uint64

	// Event handlers (user-facing hooks)
	onMessage   func(core.Message)
	onJoin      func(string, core.User)
	onLeave     func(string, core.User)
	onCallStart func(core.CallEvent)
	onCallEnd   func(core.CallEvent)

	// HTTP
	server *http.Server
	mux    *http.ServeMux
}

// New creates a new Mana application with the given configuration.
func New(cfg core.Config) *App {
	logger := observ.NewLogger(observ.LevelInfo)
	metrics := observ.NewMetrics()
	store, err := storage.NewMessageStore(cfg.MessageStorePath)
	if err != nil {
		panic(fmt.Errorf("create message store: %w", err))
	}
	if warnings := cfg.Validate(); len(warnings) > 0 {
		for _, w := range warnings {
			logger.Warn("Config: %s", w)
		}
	}

	hub := signaling.NewHub()
	app := &App{
		config:       cfg,
		roomManager:  room.NewManager(),
		signalHub:    hub,
		rtcManager:   rtc.NewManagerWithConfig(cfg),
		callManager:  rtc.NewCallManager(),
		mux:          http.NewServeMux(),
		logger:       logger,
		metrics:      metrics,
		store:        store,
		notifHub:     notification.NewHub(logger),
		sessions:     make(map[string]*room.UserSession),
		userSessions: make(map[string]map[string]*room.UserSession),
		deviceCursor: make(map[string]uint64),
	}

	app.rtcManager.SetOnTrack(func(peerID, roomID, trackID string) {
		app.logger.Info("SFU: Track %s added to room %s by peer %s", trackID, roomID, peerID)
		
		// Broadcast to room that a new track is available
		_ = app.signalHub.Broadcast(context.Background(), roomID, "SYSTEM", core.Signal{
			Type:    core.SignalTrackAdded,
			From:    "SFU",
			RoomID:  roomID,
			Payload: []byte(trackID),
		})
	})

	// Default database initialization if DSN is provided
	if cfg.DatabaseDSN != "" && app.backend == nil {
		backend, err := db.NewBackend(cfg.DatabaseDriver, cfg.DatabaseDSN)
		if err != nil {
			logger.Error("failed to initialize centralized database: %v", err)
		} else {
			app.backend = backend
			app.initializeDefaultStores(backend)
		}
	}

	if cfg.EnableTracing {
		tracing, err := observ.NewTracing(observ.TracingConfig{
			ServiceName:    cfg.ServiceName,
			ServiceVersion: cfg.ServiceVersion,
			SampleRatio:    cfg.TraceSampleRatio,
		})
		if err != nil {
			logger.Error("enable tracing: %v", err)
		} else {
			app.tracing = tracing
		}
	}

	if cfg.PubSubBackend != "" {
		bus, err := cluster.NewBackendFromConfig(cfg)
		if err != nil {
			logger.Error("enable cluster backend %s: %v", cfg.PubSubBackend, err)
		} else {
			app.cluster = bus
			nodeID := cfg.ClusterNodeID
			if nodeID == "" {
				host, _ := os.Hostname()
				nodeID = fmt.Sprintf("%s-%d", host, os.Getpid())
			}
			if err := hub.SetCluster(nodeID, bus); err != nil {
				logger.Error("subscribe cluster backend %s: %v", cfg.PubSubBackend, err)
			} else {
				logger.Info("Cluster pub-sub enabled: backend=%s node=%s", bus.Kind(), nodeID)
			}
		}
	}

	// Wire hub lifecycle hooks
	hub.SetOnJoin(func(peerID, roomID string) {
		app.mu.RLock()
		session, ok := app.sessions[peerID]
		app.mu.RUnlock()
		if ok && app.onJoin != nil {
			app.onJoin(roomID, core.User{ID: session.UserID, Username: session.Username, Online: true})
		}
	})
	hub.SetOnLeave(func(peerID, roomID string) {
		app.mu.RLock()
		session, ok := app.sessions[peerID]
		app.mu.RUnlock()
		if ok && app.onLeave != nil {
			app.onLeave(roomID, core.User{ID: session.UserID, Username: session.Username, Online: false})
		}
	})

	// Auth
	if cfg.EnableAuth {
		app.jwtAuth = auth.NewJWTAuth(auth.Config{Secret: cfg.JWTSecret, Issuer: cfg.JWTIssuer, TokenExpiry: cfg.JWTExpiry})
		app.rbac = auth.NewRBAC()
		logger.Info("Authentication enabled (JWT + RBAC)")
	}

	// Rate limiting
	if cfg.RateLimitPerSecond > 0 {
		app.rateLimiter = auth.NewRateLimiter(cfg.RateLimitPerSecond, cfg.RateLimitBurst)
		logger.Info("Rate limiting: %d/sec, burst %d", cfg.RateLimitPerSecond, cfg.RateLimitBurst)
	}

	// E2EE
	if cfg.EnableE2EE && app.backend != nil {
		e2eeStore, err := e2ee.NewSQLKeyStore(app.backend, cfg.DatabaseTablePrefix)
		if err != nil {
			logger.Error("failed to initialize E2EE store: %v", err)
		} else {
			app.e2eeStore = e2eeStore
			app.sessionMgr = e2ee.NewSessionManager(e2eeStore)
			logger.Info("E2EE key exchange and session management enabled (SQL-backed)")
		}
	}

	// Message router (uses signaling package)
	app.router = signaling.NewRouter(signaling.RouterConfig{
		Hub:         hub,
		RoomManager: app.roomManager,
		CallManager: app.callManager,
		Logger:      app,
		RBAC:        app.rbacAdapter(),
		OnMessage:   app.handleFrameworkMessage,
		RoomStore:   app.rooms,
	})

	// RTC signal wiring
	if cfg.EnableRTC {
		app.setupRTCSignaling()
		logger.Info("WebRTC enabled: STUN=%v TURN=%d policy=%s", cfg.STUNServers, len(cfg.TURNServers), cfg.ICETransportPolicy)
	}

	// WebSocket handler (uses ws package)
	app.wsHandler = ws.NewHandler(ws.HandlerConfig{
		EnableAuth:     cfg.EnableAuth,
		JWTAuth:        app.jwtAuth,
		RateLimiter:    app.rateLimiter,
		MaxMessageSize: cfg.MaxMessageSize,
		AllowedOrigins: cfg.AllowedOrigins,
		OnConnect:      app.onWSConnect,
		OnDisconnect:   app.onWSDisconnect,
		OnMessage:      app.onWSMessage,
	})

	// Product store initialization
	pStore, err := product.NewStore(cfg.ProductStorePath, cfg.AttachmentDir)
	if err != nil {
		logger.Error("failed to initialize product store: %v", err)
	} else {
		app.product = pStore
	}

	return app
}

// --- Event hooks (user-facing API) ---

func (a *App) OnMessage(h func(core.Message))                  { a.onMessage = h }
func (a *App) OnUserJoin(h func(string, core.User))            { a.onJoin = h }
func (a *App) OnUserLeave(h func(string, core.User))           { a.onLeave = h }
func (a *App) OnCallStart(h func(core.CallEvent))              { a.onCallStart = h }
func (a *App) OnCallEnd(h func(core.CallEvent))                { a.onCallEnd = h }
func (a *App) OnSignal(t core.SignalType, h func(core.Signal)) { a.signalHub.On(t, h) }

// --- Component accessors ---

func (a *App) RoomManager() *room.Manager          { return a.roomManager }
func (a *App) SignalHub() *signaling.Hub           { return a.signalHub }
func (a *App) RTCManager() *rtc.Manager            { return a.rtcManager }
func (a *App) JWTAuth() *auth.JWTAuth              { return a.jwtAuth }
func (a *App) RBAC() *auth.RBAC                    { return a.rbac }
func (a *App) Metrics() *observ.Metrics            { return a.metrics }
func (a *App) Mux() *http.ServeMux                 { return a.mux }
func (a *App) E2EEStore() e2ee.KeyStore             { return a.e2eeStore }
func (a *App) CallManager() *rtc.CallManager       { return a.callManager }
func (a *App) Logger() *observ.Logger              { return a.logger }
func (a *App) NotificationHub() *notification.Hub  { return a.notifHub }
func (a *App) MessageStore() core.MessageStore { return a.store }
func (a *App) AccountStore() core.AccountStore { return a.account }
func (a *App) ProfileStore() core.ProfileStore { return a.profile }
func (a *App) ContactStore() core.ContactStore { return a.contact }
func (a *App) DeviceStore() core.DeviceStore   { return a.device }
func (a *App) PreferenceStore() core.PreferenceStore { return a.prefs }
func (a *App) RoomStore() core.RoomStore       { return a.rooms }
func (a *App) DBBackend() *db.Backend { return a.backend }
func (a *App) ProductStore() *product.Store { return a.product }

// --- Dependency Injection & Database Setters ---

func (a *App) WithDatabase(dbConn *sql.DB, driver string) *App {
	backend, err := db.NewBackendFromDB(dbConn, driver)
	if err != nil {
		a.logger.Error("WithDatabase: %v", err)
		return a
	}
	a.backend = backend
	a.initializeDefaultStores(backend)
	return a
}

func (a *App) initializeDefaultStores(backend *db.Backend) {
	prefix := a.config.DatabaseTablePrefix
	if a.store == nil {
		a.store, _ = storage.NewSQLMessageStoreWithPrefix(backend, prefix)
	}
	if a.account == nil {
		var err error
		a.account, err = auth.NewSQLAccountStoreWithPrefix(backend, prefix)
		if err != nil {
			a.logger.Error("Failed to initialize SQL Account Store: %v", err)
		}
	}
	if a.profile == nil {
		socialStore, err := social.NewSQLSocialStoreWithPrefix(backend, prefix)
		if err != nil {
			a.logger.Error("Failed to initialize SQL Social Store: %v", err)
		}
		a.profile = socialStore
		a.contact = socialStore
		// Default implementations for others if not provided
		a.prefs, _ = settings.NewSQLPreferenceStoreWithPrefix(backend, prefix)
	}
	if a.rooms == nil {
		a.rooms, _ = storage.NewSQLRoomStoreWithPrefix(backend, prefix)
	}
}

func (a *App) WithMessageStore(s core.MessageStore) *App { a.store = s; return a }
func (a *App) WithAccountStore(s core.AccountStore) *App { a.account = s; return a }
func (a *App) WithProfileStore(s core.ProfileStore) *App { a.profile = s; return a }
func (a *App) WithContactStore(s core.ContactStore) *App { a.contact = s; return a }
func (a *App) WithDeviceStore(s core.DeviceStore) *App   { a.device = s; return a }
func (a *App) WithPreferenceStore(s core.PreferenceStore) *App { a.prefs = s; return a }
func (a *App) WithRoomStore(s core.RoomStore) *App { a.rooms = s; return a }
func (a *App) WithProductStore(s *product.Store) *App { a.product = s; return a }

// --- Logic Hooks ---

func (a *App) OnAccountCreated(h func(context.Context, core.User))   { a.onAccountCreated = h }
func (a *App) OnMessageStored(h func(context.Context, core.Message)) { a.onMessageStored = h }

// --- Server lifecycle ---

// Start begins listening for connections and blocks until shutdown.
func (a *App) Start() error {
	addr := fmt.Sprintf("%s:%d", a.config.Host, a.config.Port)

	// Register routes
	a.mux.Handle("/ws", a.wsHandler)
	a.mux.HandleFunc("/health", observ.HealthHandler(
		observ.HealthConfig{EnableAuth: a.config.EnableAuth, JWTAuth: a.jwtAuth},
		func() int { return len(a.roomManager.List()) },
		func() int { return a.signalHub.PeerCount() },
		func() int { a.mu.RLock(); n := len(a.sessions); a.mu.RUnlock(); return n },
	))
	a.mux.HandleFunc("/metrics", observ.MetricsHandler(
		observ.MetricsConfig{EnableAuth: a.config.EnableAuth, JWTAuth: a.jwtAuth},
		a.metrics,
		func() int64 { return int64(len(a.roomManager.List())) },
		func() int64 { return int64(a.signalHub.PeerCount()) },
		func() int64 { return int64(a.callManager.ActiveCallCount()) },
	))
	if a.tracing != nil {
		a.mux.Handle("/debug/traces", a.tracing.Handler())
	}

	handler := http.Handler(a.mux)
	if a.tracing != nil {
		handler = a.tracing.HTTPMiddleware(handler)
	}

	a.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  a.config.ReadTimeout,
		WriteTimeout: a.config.WriteTimeout,
		IdleTimeout:  a.config.IdleTimeout,
	}

	a.logger.Info("Mana server starting on %s", addr)

	if a.config.EnableTLS {
		return a.server.ListenAndServeTLS(a.config.TLSCertFile, a.config.TLSKeyFile)
	}
	return a.server.ListenAndServe()
}

// StartWithGracefulShutdown starts the server and handles OS signals.
func (a *App) StartWithGracefulShutdown() error {
	errCh := make(chan error, 1)
	go func() { errCh <- a.Start() }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		a.logger.Info("Received %v, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), a.config.GracefulShutdownTimeout)
		defer cancel()
		return a.Shutdown(ctx)
	}
}

// Shutdown gracefully stops the server.
func (a *App) Shutdown(ctx context.Context) error {
	a.mu.RLock()
	for _, s := range a.sessions {
		s.Conn.Close()
	}
	a.mu.RUnlock()

	// E2EE cleanup is handled by the KeyStore (persistent storage)

	if a.server != nil {
		err := a.server.Shutdown(ctx)
		if a.cluster != nil {
			_ = a.cluster.Close()
		}
		if a.tracing != nil {
			_ = a.tracing.Shutdown(ctx)
		}
		return err
	}
	return nil
}

// --- WebSocket callbacks (wired by ws.Handler) ---

func (a *App) onWSConnect(peerID, username, userRole string, conn ws.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	userID, deviceID := splitSessionID(peerID)
	var span trace.Span
	if a.tracing != nil {
		ctx, span = a.tracing.StartSpan(ctx, "ws.connect",
			attribute.String("session.id", peerID),
			attribute.String("user.id", userID),
			attribute.String("device.id", deviceID),
		)
	}
	if span != nil {
		defer span.End()
	}

	peer := &signaling.Peer{ID: peerID, UserID: userID, DeviceID: deviceID, Username: username, Conn: conn, Context: ctx, Cancel: cancel}
	session := room.NewDeviceSession(peerID, userID, username, deviceID, conn)
	if a.product != nil {
		_ = a.product.TouchDevice(userID, deviceID)
		_ = a.product.UpsertProfile(product.Profile{UserID: userID, DisplayName: username})
	}

	a.mu.Lock()
	if old, ok := a.sessions[peerID]; ok {
		old.Conn.Close()
	}
	a.sessions[peerID] = session
	if a.userSessions[userID] == nil {
		a.userSessions[userID] = make(map[string]*room.UserSession)
	}
	a.userSessions[userID][peerID] = session
	a.mu.Unlock()

	a.signalHub.RegisterPeer(peer)
	a.notifHub.Register(userID, peerID, conn)
	a.replayOfflineSync(session, 0, "reconnect", a.config.ReplayBatchSize)
	a.metrics.IncConnections()
	a.logger.Info("User %s (%s) connected [role=%s device=%s]", userID, username, userRole, deviceID)
}

func (a *App) onWSDisconnect(peerID string) {
	userID, deviceID := splitSessionID(peerID)
	a.signalHub.UnregisterPeer(peerID)
	a.mu.Lock()
	delete(a.sessions, peerID)
	if sessions := a.userSessions[userID]; sessions != nil {
		delete(sessions, peerID)
		if len(sessions) == 0 {
			delete(a.userSessions, userID)
		}
	}
	a.mu.Unlock()
	a.notifHub.Unregister(userID, peerID)
	a.metrics.DecConnections()
	a.logger.Info("User %s disconnected [device=%s]", userID, deviceID)
}

func (a *App) onWSMessage(peerID, username, userRole string, data []byte) {
	a.mu.RLock()
	session, ok := a.sessions[peerID]
	a.mu.RUnlock()
	if !ok {
		return
	}

	a.metrics.AddBytesReceived(int64(len(data)))
	a.metrics.IncMessages()

	ctx := context.Background()
	var span trace.Span
	if a.tracing != nil {
		ctx, span = a.tracing.StartSpan(ctx, "ws.message",
			attribute.String("session.id", peerID),
			attribute.Int("message.bytes", len(data)),
		)
	}
	if span != nil {
		defer span.End()
	}
	peer := &signaling.Peer{ID: peerID, UserID: session.UserID, DeviceID: session.DeviceID, Username: username, Conn: session.Conn, Context: ctx}

	// Send ack if the message contains an ack_id
	var sig core.Signal
	if json.Unmarshal(data, &sig) == nil && sig.Type != "" {
		if sig.Type == core.SignalSyncRequest {
			var req core.SyncRequest
			if err := json.Unmarshal(data, &req); err == nil {
				a.replayOfflineSync(session, req.Cursor, "client_request", req.Limit)
				return
			}
		}
		if sig.AckID != "" {
			ackData, _ := json.Marshal(core.AckMessage{Type: "ack", AckID: sig.AckID})
			session.Conn.Write(ctx, ackData)
		}
		a.router.HandleSignal(ctx, peer, userRole, sig)
		return
	}

	a.router.HandleIncoming(ctx, peer, userRole, data)
}

func (a *App) handleFrameworkMessage(msg core.Message) {
	recipients := a.messageRecipients(msg)
	conversationID := messageConversationID(msg)
	stored, err := a.store.SaveMessage(context.Background(), msg, recipients)
	if err != nil {
		a.logger.Error("persist message %s: %v", msg.ID, err)
	} else {
		msg = stored
		// Trigger logic hook if registered
		if a.onMessageStored != nil {
			a.onMessageStored(context.Background(), msg)
		}
		if a.product != nil {
			_ = a.product.UpsertConversation(product.Conversation{
				ID:           conversationID,
				IsGroup:      msg.RoomID != "",
				Participants: uniqueStrings(append([]string{msg.SenderID}, recipients...)),
				Title:        msg.RoomID,
			})
			_ = a.product.AddMessage(conversationID, msg)
		}
		for _, recipient := range recipients {
			if a.isUserOnline(recipient) {
				_ = a.store.MarkDelivered(context.Background(), msg.ID, recipient)
				if a.product != nil {
					_ = a.product.MarkDelivered(msg.ID, recipient)
				}
			}
		}
	}

	if a.onMessage != nil {
		a.onMessage(msg)
	}
}

func (a *App) messageRecipients(msg core.Message) []string {
	if msg.TargetID != "" {
		return []string{msg.TargetID}
	}
	if msg.RoomID == "" {
		return nil
	}
	rm, err := a.roomManager.Get(msg.RoomID)
	if err != nil {
		return nil
	}
	members := rm.Members()
	recipients := make([]string, 0, len(members))
	for _, member := range members {
		if member.ID == msg.SenderID {
			continue
		}
		recipients = append(recipients, member.ID)
	}
	return recipients
}

func (a *App) isUserOnline(userID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.userSessions[userID]) > 0
}

func (a *App) replayOfflineSync(session *room.UserSession, requestedCursor uint64, reason string, limit int) {
	if session == nil || a.store == nil {
		return
	}

	a.mu.RLock()
	cursor := a.deviceCursor[deviceKey(session.UserID, session.DeviceID)]
	a.mu.RUnlock()
	if requestedCursor > cursor {
		cursor = requestedCursor
	}

	messages, hasMore := a.store.SyncForUserAfterSequence(context.Background(), session.UserID, cursor, limit)
	pending := a.store.PendingForUser(context.Background(), session.UserID)
	if len(messages) == 0 && len(pending) > 0 {
		messages = pending
	}
	messages = dedupeMessages(messages)
	if len(messages) == 0 {
		return
	}

	payload, err := json.Marshal(core.DeviceSyncBatch{
		Type:      string(core.SignalSync),
		SessionID: session.SessionID,
		DeviceID:  session.DeviceID,
		Cursor:    maxMessageSequence(messages, cursor),
		HasMore:   hasMore,
		Reason:    reason,
		Messages:  messages,
		Timestamp: time.Now(),
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.Conn.Write(ctx, payload); err == nil {
		maxSeq := cursor
		for _, message := range messages {
			_ = a.store.MarkDelivered(context.Background(), message.ID, session.UserID)
			if message.Sequence > maxSeq {
				maxSeq = message.Sequence
			}
		}
		a.mu.Lock()
		a.deviceCursor[deviceKey(session.UserID, session.DeviceID)] = maxSeq
		a.mu.Unlock()
	}
}

// --- RTC signaling (extracted from app.go) ---

func (a *App) setupRTCSignaling() {
	a.callManager.OnCallEvent(func(event core.CallEvent) {
		switch event.Status {
		case "", "started":
			if a.onCallStart != nil {
				a.onCallStart(event)
			}
		case "ended":
			if a.onCallEnd != nil {
				a.onCallEnd(event)
			}
		}
	})

	a.rtcManager.SetOnTrack(func(peerID, roomID, trackID string) {
		ctx := context.Background()
		a.signalHub.BroadcastToRoom(ctx, roomID, peerID, core.Signal{
			Type: "track_added", From: peerID, RoomID: roomID, Payload: []byte(trackID),
		})
	})
	a.rtcManager.SetOnRecoveryNeeded(func(peerID, roomID, reason string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		offer, err := a.rtcManager.RestartICE(peerID)
		if err != nil {
			return
		}
		_ = a.signalHub.Send(ctx, core.Signal{
			Type:    core.SignalOffer,
			From:    "SFU",
			To:      peerID,
			RoomID:  roomID,
			SDP:     offer.SDP,
			Payload: []byte(reason),
		})
	})

	a.signalHub.On(core.SignalOffer, func(sig core.Signal) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var sdp webrtc.SessionDescription
		if err := json.Unmarshal([]byte(sig.SDP), &sdp); err != nil {
			return
		}
		answer, err := a.rtcManager.HandleOffer(ctx, sig.From, sig.From, sig.RoomID, sdp)
		if err != nil {
			return
		}
		a.signalHub.Send(ctx, core.Signal{Type: core.SignalAnswer, From: "SFU", To: sig.From, RoomID: sig.RoomID, SDP: answer.SDP})
	})

	a.signalHub.On(core.SignalSFUOffer, func(sig core.Signal) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		// Ensure user is in the signaling room so they receive track_added notifications
		a.signalHub.AddPeerToRoom(sig.RoomID, sig.From)
		
		userID, _ := splitSessionID(sig.From)
		offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sig.SDP}
		
		// 1. Handle for the SFU (internal)
		answer, err := a.rtcManager.HandleOffer(ctx, sig.From, userID, sig.RoomID, offer)
		if err != nil {
			a.logger.Error("SFU: Error handling offer from %s: %v", sig.From, err)
			return
		}
		
		// 2. Respond to the sender with the SFU answer
		_ = a.signalHub.Send(ctx, core.Signal{
			Type:   core.SignalSFUAnswer,
			From:   "SYSTEM",
			To:     sig.From,
			RoomID: sig.RoomID,
			SDP:    answer.SDP,
		})

		// 3. RELAY a notification to the target peer if this is a 1:1 call
		// This triggers the "Incoming Call" popup WITHOUT starting the WebRTC handshake yet.
		if sig.To != "" {
			a.logger.Info("SFU: Relaying call invite from %s to %s", sig.From, sig.To)
			_ = a.signalHub.Send(ctx, core.Signal{
				Type:    core.SignalCallStart, // Use call_start for notification only
				From:    sig.From,
				To:      sig.To,
				RoomID:  sig.RoomID,
				Payload: []byte(sig.Payload), // Relay the payload (which contains call type)
			})
		}
	})

	a.signalHub.On(core.SignalSFUAnswer, func(sig core.Signal) {
		answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sig.SDP}
		_ = a.rtcManager.HandleAnswer(sig.From, answer)
	})

	a.signalHub.On(core.SignalSFUCandidate, func(sig core.Signal) {
		var candidate webrtc.ICECandidateInit
		if str, ok := sig.Candidate.(string); ok {
			json.Unmarshal([]byte(str), &candidate)
		} else {
			raw, _ := json.Marshal(sig.Candidate)
			json.Unmarshal(raw, &candidate)
		}
		a.rtcManager.HandleICECandidate(sig.From, candidate)
	})

	a.signalHub.On(core.SignalCandidate, func(sig core.Signal) {
		// Only SFU handles signals without a target (broadcast to room/manager)
		if sig.To != "" {
			return
		}

		var candidate webrtc.ICECandidateInit
		if str, ok := sig.Candidate.(string); ok {
			json.Unmarshal([]byte(str), &candidate)
		} else {
			raw, _ := json.Marshal(sig.Candidate)
			json.Unmarshal(raw, &candidate)
		}
		a.rtcManager.HandleICECandidate(sig.From, candidate)
	})

	a.signalHub.On(core.SignalSubscribe, func(sig core.Signal) {
		trackID := string(sig.Payload)
		err := a.rtcManager.Subscribe(sig.From, trackID)
		if err != nil {
			a.logger.Error("SFU: Failed to subscribe peer %s to track %s: %v", sig.From, trackID, err)
			return
		}
		
		// Subscribing creates a new track which requires an SDP renegotiation. Let's restart ICE/offer process.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		offer, err := a.rtcManager.RestartICE(sig.From)
		if err == nil {
			_ = a.signalHub.Send(ctx, core.Signal{
				Type:   core.SignalSFUOffer,
				From:   "SYSTEM",
				To:     sig.From,
				RoomID: sig.RoomID,
				SDP:    offer.SDP,
			})
		}
	})

	a.signalHub.On(core.SignalICERestart, func(sig core.Signal) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		offer, err := a.rtcManager.RestartICE(sig.From)
		if err != nil {
			return
		}
		_ = a.signalHub.Send(ctx, core.Signal{
			Type:   core.SignalOffer,
			From:   "SFU",
			To:     sig.From,
			RoomID: sig.RoomID,
			SDP:    offer.SDP,
		})
	})
	a.signalHub.On(core.SignalNetworkChange, func(sig core.Signal) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		offer, err := a.rtcManager.RestartICE(sig.From)
		if err != nil {
			return
		}
		_ = a.signalHub.Send(ctx, core.Signal{
			Type:   core.SignalOffer,
			From:   "SFU",
			To:     sig.From,
			RoomID: sig.RoomID,
			SDP:    offer.SDP,
		})
	})

	a.signalHub.On(core.SignalKeyExchange, func(sig core.Signal) {
		if a.e2eeStore == nil {
			return
		}
		// Skip unmarshaling if this is a legacy base64 key swap instead of an X3DH bundle
		if len(sig.Payload) == 0 || sig.Payload[0] != '{' {
			// Older clients (like Kuruvi's toy ECDH) send purely binary keys over key_exchange.
			return
		}

		// sig.Payload should be a JSON-serialized PublicPreKeyBundle
		var bundle e2ee.PublicPreKeyBundle
		if err := json.Unmarshal(sig.Payload, &bundle); err != nil {
			a.logger.Error("E2EE: failed to unmarshal bundle from %s: %v", sig.From, err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Save the bundle using a composite key: userID:deviceID
		userID, _ := splitSessionID(sig.From)
		if bundle.DeviceID == "" {
			_, bundle.DeviceID = splitSessionID(sig.From)
		}
		
		target := userID
		if bundle.DeviceID != "" {
			target = userID + ":" + bundle.DeviceID
		}

		if err := a.e2eeStore.SavePreKeyBundle(ctx, target, &bundle); err != nil {
			a.logger.Error("E2EE: failed to save bundle for %s: %v", target, err)
			return
		}

		a.logger.Info("E2EE: saved prekey bundle for %s (device: %s, OPKs: %d)", userID, bundle.DeviceID, len(bundle.OneTimePreKeys))
	})

	a.signalHub.On(core.SignalGetPreKeyBundle, func(sig core.Signal) {
		if a.e2eeStore == nil || sig.To == "" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// If a specific device is requested (sig.To is userID:deviceID)
		// Otherwise, find all devices for that user (Fan-out discovery)
		targetUserID, targetDeviceID := splitSessionID(sig.To)
		
		var bundles []*e2ee.PublicPreKeyBundle
		if targetDeviceID != "" {
			bundle, err := a.e2eeStore.LoadPreKeyBundle(ctx, sig.To)
			if err == nil && bundle != nil {
				bundles = append(bundles, bundle)
			}
		} else {
			// Find all devices for this user
			if a.device != nil {
				devices, _ := a.device.GetDevices(ctx, targetUserID)
				for _, d := range devices {
					bundle, err := a.e2eeStore.LoadPreKeyBundle(ctx, targetUserID+":"+d.DeviceID)
					if err == nil && bundle != nil {
						bundles = append(bundles, bundle)
					}
				}
			}
		}

		if len(bundles) == 0 {
			return
		}

		// Return bundles to the requester
		payload, _ := json.Marshal(bundles)
		_ = a.signalHub.Send(ctx, core.Signal{
			Type:    core.SignalGetPreKeyBundle,
			From:    "SYSTEM",
			To:      sig.From,
			Payload: payload,
		})
	})

	a.signalHub.On(core.SignalPreKeyRefill, func(sig core.Signal) {
		if a.e2eeStore == nil {
			return
		}
		var bundle e2ee.PublicPreKeyBundle
		if err := json.Unmarshal(sig.Payload, &bundle); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		userID, deviceID := splitSessionID(sig.From)
		target := userID + ":" + deviceID
		
		// Load existing bundle, add new OPKs, and save
		existing, _ := a.e2eeStore.LoadPreKeyBundle(ctx, target)
		if existing == nil {
			existing = &bundle
		} else {
			if existing.OneTimePreKeys == nil {
				existing.OneTimePreKeys = make(map[uint32]*ecdh.PublicKey)
			}
			for id, key := range bundle.OneTimePreKeys {
				existing.OneTimePreKeys[id] = key
			}
		}
		_ = a.e2eeStore.SavePreKeyBundle(ctx, target, existing)
		a.logger.Info("E2EE: refilled OPKs for %s (total: %d)", target, len(existing.OneTimePreKeys))
	})

	a.signalHub.On(core.SignalEncryptedFanout, func(sig core.Signal) {
		var fanout core.EncryptedFanout
		if err := json.Unmarshal(sig.Payload, &fanout); err != nil {
			a.logger.Error("E2EE: failed to unmarshal fanout from %s: %v", sig.From, err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// sig.To represents the destination UserID. We fan out to each device in the payload.
		targetUserID := sig.To
		for deviceID, encPayload := range fanout.Payloads {
			targetPeerID := targetUserID + ":" + deviceID
			
			// Reconstruct a standard message signal for the target device
			deviceSig := core.Signal{
				Type:      core.SignalMessage,
				From:      sig.From,
				To:        targetPeerID,
				Payload:   encPayload,
				Timestamp: time.Now(),
			}

			// Deliver via Signal Hub
			if err := a.signalHub.Send(ctx, deviceSig); err != nil {
				a.logger.Error("E2EE: failed to deliver fanout to %s: %v", targetPeerID, err)
			}
		}
	})
}

// --- Logger interface (for signaling.Router) ---

func (a *App) Info(format string, args ...interface{})  { a.logger.Info(format, args...) }
func (a *App) Error(format string, args ...interface{}) { a.logger.Error(format, args...) }
func (a *App) Warn(format string, args ...interface{})  { a.logger.Warn(format, args...) }
func (a *App) Debug(format string, args ...interface{}) { a.logger.Debug(format, args...) }

// rbacAdapter adapts auth.RBAC to signaling.Authorizer interface.
func (a *App) rbacAdapter() *rbacAdapterImpl {
	if a.rbac == nil {
		return nil
	}
	return &rbacAdapterImpl{rbac: a.rbac}
}

type rbacAdapterImpl struct {
	rbac *auth.RBAC
}

func (r *rbacAdapterImpl) Authorize(role string, perm string) bool {
	if r == nil || r.rbac == nil {
		return true
	}
	return r.rbac.Authorize(auth.Role(role), auth.Permission(perm))
}

func splitSessionID(sessionID string) (userID, deviceID string) {
	parts := strings.SplitN(sessionID, "::", 2)
	userID = parts[0]
	if len(parts) == 2 {
		deviceID = parts[1]
	}
	return userID, deviceID
}

func deviceKey(userID, deviceID string) string {
	return userID + "::" + deviceID
}

func dedupeMessages(messages []core.Message) []core.Message {
	seen := make(map[string]struct{}, len(messages))
	result := make([]core.Message, 0, len(messages))
	for _, message := range messages {
		if _, ok := seen[message.ID]; ok {
			continue
		}
		seen[message.ID] = struct{}{}
		result = append(result, message)
	}
	return result
}

func maxMessageSequence(messages []core.Message, fallback uint64) uint64 {
	maxSeq := fallback
	for _, message := range messages {
		if message.Sequence > maxSeq {
			maxSeq = message.Sequence
		}
	}
	return maxSeq
}

func messageConversationID(msg core.Message) string {
	if msg.RoomID != "" {
		return msg.RoomID
	}
	if msg.TargetID == "" {
		return "direct:" + msg.SenderID
	}
	left, right := msg.SenderID, msg.TargetID
	if right < left {
		left, right = right, left
	}
	return "dm:" + left + ":" + right
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

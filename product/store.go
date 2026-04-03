package product

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
)

type Profile struct {
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	About       string    `json:"about,omitempty"`
	Status      string    `json:"status,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type NotificationPreferences struct {
	MuteAll            bool `json:"mute_all"`
	MuteCalls          bool `json:"mute_calls"`
	MuteMessages       bool `json:"mute_messages"`
	PushEnabled        bool `json:"push_enabled"`
	ShowMessagePreview bool `json:"show_message_preview"`
}

type Device struct {
	DeviceID   string    `json:"device_id"`
	Label      string    `json:"label,omitempty"`
	Platform   string    `json:"platform,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at"`
	LinkedAt   time.Time `json:"linked_at"`
}

type Attachment struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id,omitempty"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Path        string    `json:"path,omitempty"`
	URL         string    `json:"url,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

type MessageState struct {
	Message        core.Message         `json:"message"`
	ConversationID string               `json:"conversation_id"`
	EditedAt       time.Time            `json:"edited_at,omitempty"`
	DeletedAt      time.Time            `json:"deleted_at,omitempty"`
	DeletedFor     map[string]time.Time `json:"deleted_for,omitempty"`
	DeliveredTo    map[string]time.Time `json:"delivered_to,omitempty"`
	ReadBy         map[string]time.Time `json:"read_by,omitempty"`
	Attachments    []Attachment         `json:"attachments,omitempty"`
}

type Draft struct {
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"user_id"`
	Text           string    `json:"text"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RetentionPolicy struct {
	MaxMessages    int           `json:"max_messages,omitempty"`
	MaxAge         time.Duration `json:"max_age,omitempty"`
	KeepDeletedFor time.Duration `json:"keep_deleted_for,omitempty"`
}

type Conversation struct {
	ID              string           `json:"id"`
	Title           string           `json:"title,omitempty"`
	IsGroup         bool             `json:"is_group"`
	Participants    []string         `json:"participants"`
	LastMessageID   string           `json:"last_message_id,omitempty"`
	LastMessageAt   time.Time        `json:"last_message_at,omitempty"`
	LastMessageText string           `json:"last_message_text,omitempty"`
	Unread          map[string]int   `json:"unread"`
	ArchivedBy      map[string]bool  `json:"archived_by,omitempty"`
	MutedBy         map[string]bool  `json:"muted_by,omitempty"`
	Drafts          map[string]Draft `json:"drafts,omitempty"`
	Retention       RetentionPolicy  `json:"retention,omitempty"`
}

type Report struct {
	ID         string    `json:"id"`
	ReporterID string    `json:"reporter_id"`
	TargetID   string    `json:"target_id,omitempty"`
	RoomID     string    `json:"room_id,omitempty"`
	MessageID  string    `json:"message_id,omitempty"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
	Status     string    `json:"status"`
}

type AdminStats struct {
	Users         int `json:"users"`
	Conversations int `json:"conversations"`
	Messages      int `json:"messages"`
	Reports       int `json:"reports"`
	BlockedEdges  int `json:"blocked_edges"`
}

type Snapshot struct {
	Profiles             map[string]Profile                 `json:"profiles"`
	Preferences          map[string]NotificationPreferences `json:"preferences"`
	Contacts             map[string]map[string]bool         `json:"contacts"`
	Blocked              map[string]map[string]bool         `json:"blocked"`
	Devices              map[string]map[string]Device       `json:"devices"`
	Conversations        map[string]*Conversation           `json:"conversations"`
	Messages             map[string]*MessageState           `json:"messages"`
	Attachments          map[string]Attachment              `json:"attachments"`
	Reports              map[string]Report                  `json:"reports"`
	ConversationMessages map[string][]string                `json:"conversation_messages"`
}

type Store struct {
	mu sync.RWMutex

	path          string
	attachmentDir string

	profiles             map[string]Profile
	preferences          map[string]NotificationPreferences
	contacts             map[string]map[string]bool
	blocked              map[string]map[string]bool
	devices              map[string]map[string]Device
	conversations        map[string]*Conversation
	messages             map[string]*MessageState
	attachments          map[string]Attachment
	reports              map[string]Report
	conversationMessages map[string][]string
}

func NewStore(path, attachmentDir string) (*Store, error) {
	s := &Store{
		path:                 path,
		attachmentDir:        attachmentDir,
		profiles:             make(map[string]Profile),
		preferences:          make(map[string]NotificationPreferences),
		contacts:             make(map[string]map[string]bool),
		blocked:              make(map[string]map[string]bool),
		devices:              make(map[string]map[string]Device),
		conversations:        make(map[string]*Conversation),
		messages:             make(map[string]*MessageState),
		attachments:          make(map[string]Attachment),
		reports:              make(map[string]Report),
		conversationMessages: make(map[string][]string),
	}
	if path != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	if attachmentDir != "" {
		if err := os.MkdirAll(attachmentDir, 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) UpsertProfile(profile Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if profile.UserID == "" {
		return fmt.Errorf("user_id required")
	}
	if profile.UpdatedAt.IsZero() {
		profile.UpdatedAt = time.Now()
	}
	s.profiles[profile.UserID] = profile
	return s.persistLocked()
}

func (s *Store) Profile(userID string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[userID]
	return profile, ok
}

func (s *Store) SetNotificationPreferences(userID string, prefs NotificationPreferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preferences[userID] = prefs
	return s.persistLocked()
}

func (s *Store) NotificationPreferences(userID string) NotificationPreferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if prefs, ok := s.preferences[userID]; ok {
		return prefs
	}
	return NotificationPreferences{PushEnabled: true, ShowMessagePreview: true}
}

func (s *Store) AddContact(userID, contactID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureEdge(s.contacts, userID, contactID)
	ensureEdge(s.contacts, contactID, userID)
	return s.persistLocked()
}

func (s *Store) Contacts(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedKeys(s.contacts[userID])
}

func (s *Store) BlockUser(userID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureEdge(s.blocked, userID, targetID)
	return s.persistLocked()
}

func (s *Store) IsBlocked(userID, targetID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blocked[userID] != nil && s.blocked[userID][targetID]
}

func (s *Store) Report(report Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if report.ID == "" {
		report.ID = fmt.Sprintf("report-%d", time.Now().UnixNano())
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now()
	}
	if report.Status == "" {
		report.Status = "open"
	}
	s.reports[report.ID] = report
	return s.persistLocked()
}

func (s *Store) Reports() []Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reports := make([]Report, 0, len(s.reports))
	for _, report := range s.reports {
		reports = append(reports, report)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].CreatedAt.After(reports[j].CreatedAt) })
	return reports
}

func (s *Store) RegisterDevice(userID string, device Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if device.DeviceID == "" {
		return fmt.Errorf("device_id required")
	}
	if s.devices[userID] == nil {
		s.devices[userID] = make(map[string]Device)
	}
	now := time.Now()
	if device.LinkedAt.IsZero() {
		device.LinkedAt = now
	}
	device.LastSeenAt = now
	s.devices[userID][device.DeviceID] = device
	return s.persistLocked()
}

func (s *Store) TouchDevice(userID, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices[userID] == nil {
		s.devices[userID] = make(map[string]Device)
	}
	device := s.devices[userID][deviceID]
	device.DeviceID = deviceID
	if device.LinkedAt.IsZero() {
		device.LinkedAt = time.Now()
	}
	device.LastSeenAt = time.Now()
	s.devices[userID][deviceID] = device
	return s.persistLocked()
}

func (s *Store) Devices(userID string) []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]Device, 0, len(s.devices[userID]))
	for _, device := range s.devices[userID] {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].LastSeenAt.After(devices[j].LastSeenAt) })
	return devices
}

func (s *Store) UpsertConversation(conv Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureConversationLocked(&conv)
	return s.persistLocked()
}

func (s *Store) ConversationsForUser(userID string) []Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Conversation, 0)
	for _, conv := range s.conversations {
		if contains(conv.Participants, userID) {
			out = append(out, cloneConversation(conv))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastMessageAt.After(out[j].LastMessageAt) })
	return out
}

func (s *Store) AddMessage(conversationID string, msg core.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv := s.conversations[conversationID]
	if conv == nil {
		conv = &Conversation{ID: conversationID, Participants: participantsFromMessage(msg), Unread: make(map[string]int)}
		s.ensureConversationLocked(conv)
	}

	state := &MessageState{
		Message:        msg,
		ConversationID: conversationID,
		DeliveredTo:    make(map[string]time.Time),
		ReadBy:         make(map[string]time.Time),
		DeletedFor:     make(map[string]time.Time),
	}
	s.messages[msg.ID] = state
	s.conversationMessages[conversationID] = append(s.conversationMessages[conversationID], msg.ID)

	conv.LastMessageID = msg.ID
	conv.LastMessageAt = msg.Timestamp
	conv.LastMessageText = string(msg.Payload)
	if conv.Unread == nil {
		conv.Unread = make(map[string]int)
	}
	for _, participant := range conv.Participants {
		if participant == msg.SenderID {
			continue
		}
		conv.Unread[participant]++
	}

	s.applyRetentionLocked(conv)
	return s.persistLocked()
}

func (s *Store) MarkDelivered(messageID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg := s.messages[messageID]; msg != nil {
		if msg.DeliveredTo == nil {
			msg.DeliveredTo = make(map[string]time.Time)
		}
		msg.DeliveredTo[userID] = time.Now()
	}
	return s.persistLocked()
}

func (s *Store) MarkRead(conversationID, userID string, upToSequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, messageID := range s.conversationMessages[conversationID] {
		msg := s.messages[messageID]
		if msg == nil || msg.Message.Sequence > upToSequence {
			continue
		}
		if msg.ReadBy == nil {
			msg.ReadBy = make(map[string]time.Time)
		}
		msg.ReadBy[userID] = time.Now()
	}
	if conv := s.conversations[conversationID]; conv != nil {
		if conv.Unread == nil {
			conv.Unread = make(map[string]int)
		}
		conv.Unread[userID] = 0
	}
	return s.persistLocked()
}

func (s *Store) EditMessage(messageID string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.messages[messageID]
	if msg == nil {
		return fmt.Errorf("message not found")
	}
	msg.Message.Payload = payload
	msg.EditedAt = time.Now()
	if conv := s.conversations[msg.ConversationID]; conv != nil && conv.LastMessageID == messageID {
		conv.LastMessageText = string(payload)
	}
	return s.persistLocked()
}

func (s *Store) DeleteMessage(messageID string, forUser string, revoke bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.messages[messageID]
	if msg == nil {
		return fmt.Errorf("message not found")
	}
	if revoke {
		msg.DeletedAt = time.Now()
		msg.Message.Payload = []byte("[deleted]")
	} else if forUser != "" {
		if msg.DeletedFor == nil {
			msg.DeletedFor = make(map[string]time.Time)
		}
		msg.DeletedFor[forUser] = time.Now()
	}
	return s.persistLocked()
}

func (s *Store) SetDraft(conversationID, userID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv := s.ensureConversationLocked(&Conversation{ID: conversationID, Participants: []string{userID}})
	if conv.Drafts == nil {
		conv.Drafts = make(map[string]Draft)
	}
	conv.Drafts[userID] = Draft{ConversationID: conversationID, UserID: userID, Text: text, UpdatedAt: time.Now()}
	return s.persistLocked()
}

func (s *Store) AddAttachment(att Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if att.ID == "" {
		att.ID = fmt.Sprintf("att-%d", time.Now().UnixNano())
	}
	if att.UploadedAt.IsZero() {
		att.UploadedAt = time.Now()
	}
	if att.Path != "" && s.attachmentDir != "" && !filepath.IsAbs(att.Path) {
		att.Path = filepath.Join(s.attachmentDir, att.Path)
	}
	s.attachments[att.ID] = att
	if att.MessageID != "" && s.messages[att.MessageID] != nil {
		s.messages[att.MessageID].Attachments = append(s.messages[att.MessageID].Attachments, att)
	}
	return s.persistLocked()
}

func (s *Store) Backup() (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked(), nil
}

func (s *Store) Restore(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = snapshot.Profiles
	s.preferences = snapshot.Preferences
	s.contacts = snapshot.Contacts
	s.blocked = snapshot.Blocked
	s.devices = snapshot.Devices
	s.conversations = snapshot.Conversations
	s.messages = snapshot.Messages
	s.attachments = snapshot.Attachments
	s.reports = snapshot.Reports
	s.conversationMessages = snapshot.ConversationMessages
	s.ensureMapsLocked()
	return s.persistLocked()
}

func (s *Store) AdminStats() AdminStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := AdminStats{
		Users:         len(s.profiles),
		Conversations: len(s.conversations),
		Messages:      len(s.messages),
		Reports:       len(s.reports),
	}
	for _, edges := range s.blocked {
		stats.BlockedEdges += len(edges)
	}
	return stats
}

func (s *Store) SetRetention(conversationID string, policy RetentionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv := s.ensureConversationLocked(&Conversation{ID: conversationID})
	conv.Retention = policy
	s.applyRetentionLocked(conv)
	return s.persistLocked()
}

func (s *Store) ensureConversationLocked(conv *Conversation) *Conversation {
	if conv == nil {
		return nil
	}
	existing := s.conversations[conv.ID]
	if existing != nil {
		if len(conv.Participants) > 0 {
			existing.Participants = uniqueStrings(append(existing.Participants, conv.Participants...))
		}
		if conv.Title != "" {
			existing.Title = conv.Title
		}
		if conv.IsGroup {
			existing.IsGroup = true
		}
		if existing.Unread == nil {
			existing.Unread = make(map[string]int)
		}
		return existing
	}
	if conv.Unread == nil {
		conv.Unread = make(map[string]int)
	}
	if conv.Participants == nil {
		conv.Participants = []string{}
	}
	copyConv := *conv
	s.conversations[conv.ID] = &copyConv
	return &copyConv
}

func (s *Store) applyRetentionLocked(conv *Conversation) {
	if conv == nil {
		return
	}
	ids := s.conversationMessages[conv.ID]
	if conv.Retention.MaxAge > 0 {
		cutoff := time.Now().Add(-conv.Retention.MaxAge)
		filtered := ids[:0]
		for _, id := range ids {
			if msg := s.messages[id]; msg != nil && msg.Message.Timestamp.Before(cutoff) {
				delete(s.messages, id)
				continue
			}
			filtered = append(filtered, id)
		}
		ids = filtered
	}
	if conv.Retention.MaxMessages > 0 && len(ids) > conv.Retention.MaxMessages {
		toDelete := ids[:len(ids)-conv.Retention.MaxMessages]
		for _, id := range toDelete {
			delete(s.messages, id)
		}
		ids = ids[len(ids)-conv.Retention.MaxMessages:]
	}
	s.conversationMessages[conv.ID] = ids
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	s.profiles = snapshot.Profiles
	s.preferences = snapshot.Preferences
	s.contacts = snapshot.Contacts
	s.blocked = snapshot.Blocked
	s.devices = snapshot.Devices
	s.conversations = snapshot.Conversations
	s.messages = snapshot.Messages
	s.attachments = snapshot.Attachments
	s.reports = snapshot.Reports
	s.conversationMessages = snapshot.ConversationMessages
	s.ensureMapsLocked()
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.snapshotLocked(), "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) snapshotLocked() Snapshot {
	return Snapshot{
		Profiles:             s.profiles,
		Preferences:          s.preferences,
		Contacts:             s.contacts,
		Blocked:              s.blocked,
		Devices:              s.devices,
		Conversations:        s.conversations,
		Messages:             s.messages,
		Attachments:          s.attachments,
		Reports:              s.reports,
		ConversationMessages: s.conversationMessages,
	}
}

func (s *Store) ensureMapsLocked() {
	if s.profiles == nil {
		s.profiles = make(map[string]Profile)
	}
	if s.preferences == nil {
		s.preferences = make(map[string]NotificationPreferences)
	}
	if s.contacts == nil {
		s.contacts = make(map[string]map[string]bool)
	}
	if s.blocked == nil {
		s.blocked = make(map[string]map[string]bool)
	}
	if s.devices == nil {
		s.devices = make(map[string]map[string]Device)
	}
	if s.conversations == nil {
		s.conversations = make(map[string]*Conversation)
	}
	if s.messages == nil {
		s.messages = make(map[string]*MessageState)
	}
	if s.attachments == nil {
		s.attachments = make(map[string]Attachment)
	}
	if s.reports == nil {
		s.reports = make(map[string]Report)
	}
	if s.conversationMessages == nil {
		s.conversationMessages = make(map[string][]string)
	}
}

func participantsFromMessage(msg core.Message) []string {
	parts := []string{msg.SenderID}
	if msg.TargetID != "" {
		parts = append(parts, msg.TargetID)
	}
	return uniqueStrings(parts)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ensureEdge(graph map[string]map[string]bool, left, right string) {
	if graph[left] == nil {
		graph[left] = make(map[string]bool)
	}
	graph[left][right] = true
}

func cloneConversation(conv *Conversation) Conversation {
	if conv == nil {
		return Conversation{}
	}
	out := *conv
	out.Participants = append([]string(nil), conv.Participants...)
	out.Unread = make(map[string]int, len(conv.Unread))
	for k, v := range conv.Unread {
		out.Unread[k] = v
	}
	return out
}

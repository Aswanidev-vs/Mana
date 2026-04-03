package product

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Aswanidev-vs/mana/core"
)

func TestStoreConversationLifecycle(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "product.json"), "")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if err := store.UpsertConversation(Conversation{
		ID:           "dm:alice:bob",
		Participants: []string{"alice", "bob"},
	}); err != nil {
		t.Fatalf("upsert conversation: %v", err)
	}

	msg := core.Message{
		ID:        "m1",
		SenderID:  "alice",
		TargetID:  "bob",
		Payload:   []byte("hello"),
		Timestamp: time.Now(),
		Sequence:  1,
	}
	if err := store.AddMessage("dm:alice:bob", msg); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := store.MarkDelivered("m1", "bob"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if err := store.MarkRead("dm:alice:bob", "bob", 1); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if err := store.EditMessage("m1", []byte("hello edited")); err != nil {
		t.Fatalf("edit message: %v", err)
	}

	conversations := store.ConversationsForUser("bob")
	if len(conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(conversations))
	}
	if conversations[0].Unread["bob"] != 0 {
		t.Fatalf("expected unread reset, got %d", conversations[0].Unread["bob"])
	}
}

func TestStoreProfilesContactsDevicesAndBackup(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "product.json"), "")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if err := store.UpsertProfile(Profile{UserID: "alice", DisplayName: "Alice"}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if err := store.SetNotificationPreferences("alice", NotificationPreferences{PushEnabled: true}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	if err := store.AddContact("alice", "bob"); err != nil {
		t.Fatalf("add contact: %v", err)
	}
	if err := store.RegisterDevice("alice", Device{DeviceID: "phone", Label: "Alice Phone"}); err != nil {
		t.Fatalf("register device: %v", err)
	}
	if err := store.Report(Report{ReporterID: "alice", TargetID: "bob", Reason: "spam"}); err != nil {
		t.Fatalf("report: %v", err)
	}

	backup, err := store.Backup()
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	restored, err := NewStore("", "")
	if err != nil {
		t.Fatalf("new restored store: %v", err)
	}
	if err := restored.Restore(backup); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, ok := restored.Profile("alice"); !ok {
		t.Fatal("expected restored profile")
	}
	if len(restored.Contacts("alice")) != 1 {
		t.Fatalf("expected restored contact, got %v", restored.Contacts("alice"))
	}
	if len(restored.Devices("alice")) != 1 {
		t.Fatalf("expected restored device, got %v", restored.Devices("alice"))
	}
	if len(restored.Reports()) != 1 {
		t.Fatalf("expected restored report, got %d", len(restored.Reports()))
	}
}

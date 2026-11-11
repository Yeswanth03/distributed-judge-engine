// Package model holds the User Service's domain types and the wire formats it shares with other services
// (see docs/contracts.md section 7).
package model

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

func (r Role) Valid() bool { return r == RoleUser || r == RoleAdmin }

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

func (s Status) Valid() bool { return s == StatusActive || s == StatusDisabled }

// User is one row of the users table. PasswordHash is nil for an account that only signs in with Google or
// GitHub. Version goes up on every change and travels with each event, so consumers can drop stale ones.
type User struct {
	ID            uuid.UUID
	Email         string
	Username      string
	DisplayName   string
	PasswordHash  *string
	Role          Role
	Status        Status
	EmailVerified bool
	FailedLogins  int
	LockedUntil   *time.Time
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Session is one signed-in device. Its refresh tokens are stored separately, hashed.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	UserAgent  string
	IP         string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

// Event types, also the routing keys on the "users" topic exchange.
const (
	EventUserRegistered    = "user.registered"
	EventUserUpdated       = "user.updated"
	EventUserRoleChanged   = "user.role_changed"
	EventUserStatusChanged = "user.status_changed"
)

// UserEvent is what other services learn about a user. It never carries the email address or anything about
// credentials: consumers only need to show a name and react to roles and bans.
type UserEvent struct {
	EventID     uuid.UUID `json:"event_id"`
	Type        string    `json:"type"`
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        Role      `json:"role"`
	Status      Status    `json:"status"`
	Version     int64     `json:"version"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// EventFor describes u's current state as an event of the given type.
func EventFor(eventType string, u *User) UserEvent {
	return UserEvent{
		EventID: uuid.New(), Type: eventType, UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		Role: u.Role, Status: u.Status, Version: u.Version, OccurredAt: u.UpdatedAt.UTC(),
	}
}

// OutboxEvent is one row of the outbox_events table: an event waiting to be published to RabbitMQ.
type OutboxEvent struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	EventType   string
	Payload     []byte
	RetryCount  int
}

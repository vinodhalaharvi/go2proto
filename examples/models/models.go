// Package models contains example domain types.
package models

import (
	"context"
	"time"
)

// Status represents user status.
type Status int

const (
	StatusUnknown Status = iota
	StatusActive
	StatusInactive
)

// User represents a user.
type User struct {
	ID        string            `json:"id"`
	Email     string            `json:"email"`
	Name      string            `json:"name"`
	Status    Status            `json:"status"`
	Tags      map[string]string `json:"tags"`
	CreatedAt time.Time         `json:"created_at"`
}

// +go2proto:service
// UserService defines user operations.
type UserService interface {
	GetUser(ctx context.Context, id string) (*User, error)
	CreateUser(ctx context.Context, user *User) (*User, error)
}

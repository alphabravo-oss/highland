package storage

import (
	"context"
	"time"
)

type ContextOperationRecord struct {
	ID          string
	ProviderID  string
	ActionID    string
	Phase       string
	TargetKind  string
	Namespace   string
	TargetName  string
	TargetUID   string
	RequestedAt time.Time
	ObservedAt  time.Time
	Message     string
}

type ContextAuditRecord struct {
	ID          string
	ProviderID  string
	Action      string
	Result      string
	Message     string
	OperationID string
	TargetKind  string
	Namespace   string
	TargetName  string
	TargetUID   string
	ObservedAt  time.Time
}

type ContextOperationSource func(context.Context, int) ([]ContextOperationRecord, error)
type ContextAuditSource func(int) []ContextAuditRecord

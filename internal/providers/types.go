package providers

import (
	"context"

	"github.com/SlimaineB/hop/internal/config"
)

type Ticket struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status,omitempty"`
	URL      string `json:"url,omitempty"`
	Updated  string `json:"updated,omitempty"`
}

type TicketDraft struct {
	Title  string
	Body   string
	Fields map[string]string
}

type Field struct {
	Key      string
	Label    string
	Type     string
	Required bool
	Options  []string
}

type TicketProvider interface {
	ID() string
	Type() string
	Keywords() []string
	IsKey(value string) bool
	Search(ctx context.Context, query string, limit int) ([]Ticket, error)
	Get(ctx context.Context, key string) (Ticket, error)
	Create(ctx context.Context, draft TicketDraft) (Ticket, error)
	URL(key string) string
}

// KeyNormalizer canonicalizes provider-specific ticket keys. Providers should
// implement it when a key can be written in several forms (URL, short ID...).
type KeyNormalizer interface {
	NormalizeKey(value string) (string, bool)
}

type Factory func(id string, cfg config.ProviderConfig) (TicketProvider, error)

package port

import "context"

type DDNSRecord struct {
	Type    string
	Name    string
	Content string
	TTL     int
	Proxied bool
}

type DDNSProvider interface {
	GetCurrentIP(ctx context.Context) (string, error)
	GetDNSRecord(ctx context.Context) (DDNSRecord, error)
	UpdateDNSRecord(ctx context.Context, ip string) (DDNSRecord, error)
}

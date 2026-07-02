package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnview/internal/config"
)

func TestValidateIPForRecord(t *testing.T) {
	tests := []struct {
		name       string
		ip         string
		recordType string
		wantErr    bool
	}{
		{name: "ipv4 a", ip: "8.8.8.8", recordType: "A", wantErr: false},
		{name: "ipv6 aaaa", ip: "2001:4860:4860::8888", recordType: "AAAA", wantErr: false},
		{name: "ipv6 for a", ip: "2001:4860:4860::8888", recordType: "A", wantErr: true},
		{name: "ipv4 for aaaa", ip: "8.8.8.8", recordType: "AAAA", wantErr: true},
		{name: "private", ip: "192.168.1.10", recordType: "A", wantErr: true},
		{name: "invalid", ip: "not-an-ip", recordType: "A", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIPForRecord(tt.ip, tt.recordType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateIPForRecord() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeRecordType(t *testing.T) {
	got, err := normalizeRecordType("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "A" {
		t.Fatalf("empty record type = %q, want A", got)
	}
	if _, err := normalizeRecordType("TXT"); err == nil {
		t.Fatalf("unsupported record type should fail")
	}
}

func TestGetDNSRecordValidatesCloudflareResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/zones/zone/dns_records/record" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"type":"A","name":"vpn.example.com","content":"8.8.8.8","ttl":1,"proxied":false}}`))
	}))
	defer server.Close()

	provider := testProvider(server.URL)
	record, err := provider.GetDNSRecord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "8.8.8.8" || record.Name != "vpn.example.com" || record.Type != "A" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestUpdateDNSRecordSendsPatchAndParsesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["content"] != "8.8.4.4" || payload["name"] != "vpn.example.com" || payload["type"] != "A" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"type":"A","name":"vpn.example.com","content":"8.8.4.4","ttl":1,"proxied":false}}`))
	}))
	defer server.Close()

	provider := testProvider(server.URL)
	record, err := provider.UpdateDNSRecord(context.Background(), "8.8.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "8.8.4.4" {
		t.Fatalf("updated content = %q", record.Content)
	}
}

func TestCloudflareSuccessFalseFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"bad request"}],"result":{}}`))
	}))
	defer server.Close()

	provider := testProvider(server.URL)
	if _, err := provider.GetDNSRecord(context.Background()); err == nil {
		t.Fatalf("success=false should fail")
	}
}

func testProvider(apiBaseURL string) *Provider {
	provider := New(&config.DDNSConfig{
		Provider:      "cloudflare",
		Domain:        "vpn.example.com",
		ZoneID:        "zone",
		RecordID:      "record",
		APIToken:      "token",
		RecordType:    "A",
		TTL:           1,
		IPCheckURLs:   []string{"https://example.com/ip"},
		CheckInterval: "5m",
	})
	provider.apiBaseURL = apiBaseURL
	return provider
}

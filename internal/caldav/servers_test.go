package caldav

import (
	"strings"
	"testing"
)

func TestParseServers(t *testing.T) {
	servers, err := ParseServers("personal=https://cal.example.com/dav, work=https://user:secret@work.example.com/dav/calendars/team/")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(servers))
	}

	if servers[0].Name != "personal" || servers[1].Name != "work" {
		t.Errorf("names = %q, %q, want personal and work", servers[0].Name, servers[1].Name)
	}

	if got := servers[1].URL.String(); got != "https://user:secret@work.example.com/dav/calendars/team/" {
		t.Errorf("URL = %q, want the credentials and path kept", got)
	}
}

func TestParseServersKeepsAURLThatContainsAnEqualsSign(t *testing.T) {
	servers, err := ParseServers("nextcloud=https://cloud.example.com/remote.php/dav/?x=1")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	if got := servers[0].URL.Query().Get("x"); got != "1" {
		t.Errorf("query = %q, want the URL split only on its first =", got)
	}
}

func TestParseServersOfAnEmptyList(t *testing.T) {
	servers, err := ParseServers("")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	if len(servers) != 0 {
		t.Errorf("servers = %v, want none", servers)
	}
}

func TestParseServersRejectsBadEntries(t *testing.T) {
	tests := map[string]string{
		"no url":         "personal",
		"empty name":     "=https://cal.example.com/dav",
		"empty url":      "personal=",
		"duplicate name": "personal=https://a.example.com,personal=https://b.example.com",
		"no scheme":      "personal=cal.example.com/dav",
		"wrong scheme":   "personal=ftp://cal.example.com/dav",
		"no host":        "personal=https:///dav",
	}

	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseServers(spec); err == nil {
				t.Error("ParseServers() error = nil, want the entry rejected")
			}
		})
	}
}

func TestParseServersNamesTheOffendingEntry(t *testing.T) {
	_, err := ParseServers("personal=https://a.example.com,broken")
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("ParseServers() error = %v, want it to quote the entry", err)
	}
}

func TestParseServersExplainsAReservedPasswordCharacter(t *testing.T) {
	_, err := ParseServers("personal=https://user:a/b@cal.example.com/dav")
	if err == nil || !strings.Contains(err.Error(), "percent-encode") {
		t.Errorf("ParseServers() error = %v, want the percent-encoding hint", err)
	}
}

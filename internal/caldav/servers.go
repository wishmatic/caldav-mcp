package caldav

import (
	"fmt"
	"net/url"
	"strings"
)

// Server is one configured CalDAV endpoint. Credentials, when the server needs
// them, belong in the URL's userinfo: https://user:password@host/path.
type Server struct {
	Name string
	URL  *url.URL
}

// ParseServers reads the name=url,name=url form of CALDAV_SERVERS. A URL is
// taken to be either a server root or a single calendar collection; the fetcher
// works out which.
func ParseServers(spec string) ([]Server, error) {
	var servers []Server

	seen := make(map[string]bool)

	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		name, rawURL, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("CALDAV_SERVERS entry %q is not name=url", entry)
		}

		name, rawURL = strings.TrimSpace(name), strings.TrimSpace(rawURL)
		if name == "" {
			return nil, fmt.Errorf("CALDAV_SERVERS entry %q has no name", entry)
		}

		if seen[name] {
			return nil, fmt.Errorf("CALDAV_SERVERS names %q twice; names identify a server in the cache", name)
		}

		endpoint, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf(
				"CALDAV_SERVERS entry %q: %w; percent-encode a reserved character in a password, as in a%%2Fb",
				entry, err,
			)
		}

		// A subscription link is usually copied out of a browser as webcal://,
		// which is the same address read over https.
		if endpoint.Scheme == "webcal" {
			endpoint.Scheme = "https"
		}

		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return nil, fmt.Errorf("CALDAV_SERVERS entry %q must be an http or https URL", entry)
		}

		if endpoint.Host == "" {
			return nil, fmt.Errorf("CALDAV_SERVERS entry %q has no host", entry)
		}

		seen[name] = true

		servers = append(servers, Server{Name: name, URL: endpoint})
	}

	return servers, nil
}

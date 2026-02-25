package flatpak

import (
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"time"
)

// appstreamXML represents the root of the Flatpak appstream XML document.
type appstreamXML struct {
	XMLName    xml.Name       `xml:"components"`
	Origin     string         `xml:"origin,attr"`
	Components []componentXML `xml:"component"`
}

// componentXML represents a single application in the appstream.
type componentXML struct {
	Type        string      `xml:"type,attr"`
	ID          string      `xml:"id"`
	Name        string      `xml:"name"`
	Summary     string      `xml:"summary"`
	Description string      `xml:"description"`
	Metadata    metadataXML `xml:"metadata"`
	Version     string      `xml:"releases>release>version"`
}

// metadataXML holds additional key-value metadata.
type metadataXML struct {
	Values []valueXML `xml:"value"`
}

// valueXML represents a single metadata key-value pair.
type valueXML struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// ParseAppStreamFile parses a Flatpak appstream XML file (optionally gzip compressed).
func ParseAppStreamFile(path string) ([]App, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file

	// Check if file is gzip compressed by reading the magic bytes
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() > 2 {
		header := make([]byte, 2)
		if _, err := file.Read(header); err != nil {
			return nil, err
		}
		// Gzip files start with magic bytes 0x1f 0x8b
		if header[0] == 0x1f && header[1] == 0x8b {
			file.Seek(0, os.SEEK_SET)
			gzReader, err := gzip.NewReader(file)
			if err != nil {
				return nil, err
			}
			defer gzReader.Close()
			reader = gzReader
		}
	}

	return ParseAppStream(reader)
}

// ParseAppStream parses Flatpak appstream XML from any reader.
func ParseAppStream(reader io.Reader) ([]App, error) {
	decoder := xml.NewDecoder(reader)
	var appstream appstreamXML
	if err := decoder.Decode(&appstream); err != nil {
		return nil, err
	}

	apps := make([]App, 0, len(appstream.Components))
	for _, comp := range appstream.Components {
		if comp.Type != "desktop-application" {
			continue
		}
		if comp.ID == "" {
			continue
		}

		// Extract origin from metadata if not set on root
		origin := appstream.Origin
		for _, v := range comp.Metadata.Values {
			if v.Key == "flatpak::origin" {
				origin = v.Value
				break
			}
		}

		// Clean up description - take first paragraph
		desc := cleanDescription(comp.Description)

		apps = append(apps, App{
			ID:          comp.ID,
			Name:        comp.Name,
			Summary:     comp.Summary,
			Description: desc,
			Version:     comp.Version,
			Origin:      origin,
		})
	}

	return apps, nil
}

// cleanDescription extracts a clean single-line description from the XML content.
func cleanDescription(desc string) string {
	if desc == "" {
		return ""
	}

	// Flatpak appstream descriptions are often HTML-like with <p> tags
	// Take the first paragraph only
	desc = strings.TrimSpace(desc)

	// Handle simple <p>...</p> patterns
	if strings.HasPrefix(desc, "<p>") {
		if idx := strings.Index(desc, "</p>"); idx > 0 {
			desc = desc[3:idx]
		}
	}

	// Remove remaining HTML tags
	desc = stripTags(desc)

	// Collapse whitespace
	desc = strings.Join(strings.Fields(desc), " ")

	return desc
}

// stripTags removes HTML/XML tags from a string.
func stripTags(s string) string {
	inTag := false
	result := strings.Builder{}
	result.Grow(len(s))

	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// FindCachePaths returns all possible Flatpak appstream cache locations.
// Flatpak stores appstream metadata in:
// - System: /var/lib/flatpak/appstream/<remote>/x86_64/../appstream.xml.gz
// - User: ~/.local/share/flatpak/appstream/<remote>/../appstream.xml.gz
func FindCachePaths() []string {
	home := os.Getenv("HOME")
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" && home != "" {
		xdgData = home + "/.local/share"
	}

	var paths []string

	// User-level cache paths
	if xdgData != "" {
		userBase := xdgData + "/flatpak/appstream"
		paths = append(paths,
			userBase+"/flathub/x86_64/appstream.xml.gz",
			userBase+"/flathub/i386/appstream.xml.gz",
			userBase+"/flathub/aarch64/appstream.xml.gz",
			userBase+"/flathub/armhf/appstream.xml.gz",
			userBase+"/flathub/appstream.xml.gz",
		)
		// Also check for unpacked directories
		paths = append(paths,
			userBase+"/flathub/x86_64/appstream.xml",
			userBase+"/flathub/appstream.xml",
		)
	}

	// System-level cache paths (common on Linux)
	paths = append(paths,
		"/var/lib/flatpak/appstream/flathub/x86_64/appstream.xml.gz",
		"/var/lib/flatpak/appstream/flathub/i386/appstream.xml.gz",
		"/var/lib/flatpak/appstream/flathub/aarch64/appstream.xml.gz",
		"/var/lib/flatpak/appstream/flathub/armhf/appstream.xml.gz",
		"/var/lib/flatpak/appstream/flathub/appstream.xml.gz",
		// Unpacked versions
		"/var/lib/flatpak/appstream/flathub/x86_64/appstream.xml",
		"/var/lib/flatpak/appstream/flathub/appstream.xml",
	)

	return paths
}

// CacheAge returns the age of the cache file at the given path.
func CacheAge(path string) (time.Duration, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return time.Since(info.ModTime()), nil
}

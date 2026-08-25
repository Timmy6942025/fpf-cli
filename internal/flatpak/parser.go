package flatpak

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("path must be absolute: %q", path)
	}
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path contains ..: %q", path)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("path is symlink: %q", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	// Check if file is gzip compressed by reading the magic bytes
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Size() > 50<<20 {
		return nil, fmt.Errorf("appstream file too large: %d bytes", info.Size())
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("appstream file empty: %q", path)
	}

	var reader io.Reader = file

	if info.Size() > 2 {
		header := make([]byte, 2)
		if _, err := file.Read(header); err != nil {
			return nil, fmt.Errorf("read header %q: %w", path, err)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek %q: %w", path, err)
		}
		// Gzip files start with magic bytes 0x1f 0x8b
		if header[0] == 0x1f && header[1] == 0x8b {
			gzReader, err := gzip.NewReader(file)
			if err != nil {
				return nil, fmt.Errorf("gzip %q: %w", path, err)
			}
			defer gzReader.Close()
			reader = gzReader
		}
	}

	apps, err := ParseAppStream(reader)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return apps, nil
}

// ParseAppStream parses Flatpak appstream XML from any reader.
// Input is capped at 10MB and any DOCTYPE declaration is rejected outright:
// appstream files never contain one, and DTDs are the vector for
// entity-expansion ("billion laughs") attacks.
func ParseAppStream(reader io.Reader) ([]App, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	data, err := io.ReadAll(io.LimitReader(reader, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(data) == 10<<20 {
		return nil, fmt.Errorf("input exceeds 10MB limit")
	}
	if bytes.Contains(data, []byte("<!DOCTYPE")) || bytes.Contains(data, []byte("<!ENTITY")) {
		return nil, fmt.Errorf("DTD declarations are not allowed in appstream XML")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var appstream appstreamXML
	if err := decoder.Decode(&appstream); err != nil {
		return nil, fmt.Errorf("xml decode: %w", err)
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
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fallback to HOME env
		home = strings.TrimSpace(os.Getenv("HOME"))
	}
	xdgData := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if xdgData != "" {
		if !filepath.IsAbs(xdgData) || strings.Contains(xdgData, "..") {
			xdgData = ""
		}
	}
	if xdgData == "" && home != "" {
		if filepath.IsAbs(home) && !strings.Contains(home, "..") {
			xdgData = filepath.Join(home, ".local", "share")
		}
	}
	if xdgData != "" {
		xdgData = filepath.Clean(xdgData)
	}

	var paths []string

	// User-level cache paths
	if xdgData != "" && xdgData != "." {
		userBase := filepath.Join(xdgData, "flatpak", "appstream")
		paths = append(paths,
			filepath.Join(userBase, "flathub", "x86_64", "appstream.xml.gz"),
			filepath.Join(userBase, "flathub", "i386", "appstream.xml.gz"),
			filepath.Join(userBase, "flathub", "aarch64", "appstream.xml.gz"),
			filepath.Join(userBase, "flathub", "armhf", "appstream.xml.gz"),
			filepath.Join(userBase, "flathub", "appstream.xml.gz"),
		)
		// Also check for unpacked directories
		paths = append(paths,
			filepath.Join(userBase, "flathub", "x86_64", "appstream.xml"),
			filepath.Join(userBase, "flathub", "appstream.xml"),
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

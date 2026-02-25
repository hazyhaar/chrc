// CLAUDE:SUMMARY RSS 2.0 and Atom 1.0 parser with auto-detection from XML root element.
// Package feed parses RSS 2.0 and Atom 1.0 feeds using encoding/xml.
//
// Auto-detects format from the XML root element:
//   - <rss ...> → RSS 2.0
//   - <feed ...> → Atom 1.0
package feed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Entry represents one item in a feed.
type Entry struct {
	GUID        string `json:"guid"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Published   string `json:"published"`
	Author      string `json:"author"`
}

// Feed represents a parsed RSS or Atom feed.
type Feed struct {
	Title   string  `json:"title"`
	Link    string  `json:"link"`
	Entries []Entry `json:"entries"`
}

// Parse auto-detects and parses RSS 2.0 or Atom 1.0 XML.
func Parse(data []byte) (*Feed, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("feed: empty data")
	}

	// Detect format by scanning for root element.
	format := detectFormat(trimmed)
	switch format {
	case "rss":
		return parseRSS(data)
	case "atom":
		return parseAtom(data)
	default:
		return nil, fmt.Errorf("feed: unknown format (expected <rss> or <feed>)")
	}
}

func detectFormat(data []byte) string {
	// Look for the first element after the XML declaration.
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			name := strings.ToLower(se.Name.Local)
			if name == "rss" || name == "rdf" {
				return "rss"
			}
			if name == "feed" {
				return "atom"
			}
			return ""
		}
	}
}

// --- RSS 2.0 ---

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Link  string    `xml:"link"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Content     string `xml:"encoded"` // content:encoded
	PubDate     string `xml:"pubDate"`
	Author      string `xml:"author"`
	Creator     string `xml:"creator"` // dc:creator
}

func parseRSS(data []byte) (*Feed, error) {
	var root rssRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("feed: parse rss: %w", err)
	}

	ch := root.Channel
	feed := &Feed{
		Title:   strings.TrimSpace(ch.Title),
		Link:    strings.TrimSpace(ch.Link),
		Entries: make([]Entry, 0, len(ch.Items)),
	}

	for _, item := range ch.Items {
		author := strings.TrimSpace(item.Author)
		if author == "" {
			author = strings.TrimSpace(item.Creator)
		}

		guid := strings.TrimSpace(item.GUID)
		if guid == "" {
			guid = strings.TrimSpace(item.Link)
		}

		feed.Entries = append(feed.Entries, Entry{
			GUID:        guid,
			Title:       strings.TrimSpace(item.Title),
			Link:        strings.TrimSpace(item.Link),
			Description: strings.TrimSpace(item.Description),
			Content:     strings.TrimSpace(item.Content),
			Published:   strings.TrimSpace(item.PubDate),
			Author:      author,
		})
	}

	return feed, nil
}

// --- Atom 1.0 ---

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomEntry struct {
	ID        string       `xml:"id"`
	Title     string       `xml:"title"`
	Links     []atomLink   `xml:"link"`
	Summary   string       `xml:"summary"`
	Content   atomContent  `xml:"content"`
	Published string       `xml:"published"`
	Updated   string       `xml:"updated"`
	Authors   []atomAuthor `xml:"author"`
}

type atomContent struct {
	Body string `xml:",chardata"`
	Type string `xml:"type,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

func parseAtom(data []byte) (*Feed, error) {
	var root atomFeed
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("feed: parse atom: %w", err)
	}

	feed := &Feed{
		Title:   strings.TrimSpace(root.Title),
		Link:    atomSelfLink(root.Links),
		Entries: make([]Entry, 0, len(root.Entries)),
	}

	for _, entry := range root.Entries {
		link := atomEntryLink(entry.Links)
		guid := strings.TrimSpace(entry.ID)
		if guid == "" {
			guid = link
		}

		published := strings.TrimSpace(entry.Published)
		if published == "" {
			published = strings.TrimSpace(entry.Updated)
		}

		var author string
		if len(entry.Authors) > 0 {
			author = strings.TrimSpace(entry.Authors[0].Name)
		}

		feed.Entries = append(feed.Entries, Entry{
			GUID:        guid,
			Title:       strings.TrimSpace(entry.Title),
			Link:        link,
			Description: strings.TrimSpace(entry.Summary),
			Content:     strings.TrimSpace(entry.Content.Body),
			Published:   published,
			Author:      author,
		})
	}

	return feed, nil
}

func atomSelfLink(links []atomLink) string {
	// Prefer rel="alternate", then first href.
	for _, l := range links {
		if l.Rel == "alternate" || l.Rel == "" {
			return strings.TrimSpace(l.Href)
		}
	}
	if len(links) > 0 {
		return strings.TrimSpace(links[0].Href)
	}
	return ""
}

func atomEntryLink(links []atomLink) string {
	for _, l := range links {
		if l.Rel == "alternate" || l.Rel == "" {
			return strings.TrimSpace(l.Href)
		}
	}
	if len(links) > 0 {
		return strings.TrimSpace(links[0].Href)
	}
	return ""
}

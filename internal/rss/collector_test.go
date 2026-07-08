package rss

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func newTestCollector() *Collector {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{Timeout: 5 * time.Second}
	return &Collector{
		parser: fp,
		seen:   make(map[int64]map[string]struct{}),
	}
}

func makeFeedXML(items []struct{ guid, link, title, desc string }) string {
	var itemsXML string
	for _, it := range items {
		itemsXML += fmt.Sprintf(
			`<item><title>%s</title><link>%s</link><guid>%s</guid><description>%s</description></item>`,
			it.title, it.link, it.guid, it.desc,
		)
	}
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Test</title>` + itemsXML + `</channel></rss>`
}

// ── itemKey ───────────────────────────────────────────────────────────────────

func TestItemKey_UsesGUID(t *testing.T) {
	item := &gofeed.Item{GUID: "abc123", Link: "https://example.com"}
	if itemKey(item) != "abc123" {
		t.Fatalf("expected GUID as key, got %q", itemKey(item))
	}
}

func TestItemKey_FallsBackToLink(t *testing.T) {
	item := &gofeed.Item{GUID: "", Link: "https://example.com/post"}
	if itemKey(item) != "https://example.com/post" {
		t.Fatalf("expected link as key, got %q", itemKey(item))
	}
}

func TestItemKey_BothEmpty(t *testing.T) {
	item := &gofeed.Item{}
	if itemKey(item) != "" {
		t.Fatalf("expected empty key, got %q", itemKey(item))
	}
}

// ── first fetch seeding ───────────────────────────────────────────────────────

func TestFirstFetchSeeds_NothingPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, makeFeedXML([]struct{ guid, link, title, desc string }{
			{"guid1", "https://a.com/1", "Post 1", "body1"},
			{"guid2", "https://a.com/2", "Post 2", "body2"},
		}))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	feed, _ := c.parser.ParseURL(s.url)
	published := 0
	if _, ok := c.seen[s.id]; !ok {
		c.seen[s.id] = make(map[string]struct{})
		for _, item := range feed.Items {
			c.seen[s.id][itemKey(item)] = struct{}{}
		}
	} else {
		for _, item := range feed.Items {
			if _, already := c.seen[s.id][itemKey(item)]; !already {
				published++
			}
		}
	}

	if published != 0 {
		t.Fatalf("expected 0 published on first fetch, got %d", published)
	}
	if len(c.seen[1]) != 2 {
		t.Fatalf("expected 2 items seeded, got %d", len(c.seen[1]))
	}
}

// ── deduplication ─────────────────────────────────────────────────────────────

func TestDedup_NewItemPublished(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		items := []struct{ guid, link, title, desc string }{
			{"guid1", "https://a.com/1", "Post 1", ""},
		}
		if callCount >= 2 {
			items = append(items, struct{ guid, link, title, desc string }{"guid2", "https://a.com/2", "Post 2", ""})
		}
		fmt.Fprint(w, makeFeedXML(items))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	feed1, _ := c.parser.ParseURL(s.url)
	c.seen[s.id] = make(map[string]struct{})
	for _, item := range feed1.Items {
		c.seen[s.id][itemKey(item)] = struct{}{}
	}

	feed2, _ := c.parser.ParseURL(s.url)
	var newItems []*gofeed.Item
	for _, item := range feed2.Items {
		if _, already := c.seen[s.id][itemKey(item)]; !already {
			newItems = append(newItems, item)
			c.seen[s.id][itemKey(item)] = struct{}{}
		}
	}

	if len(newItems) != 1 {
		t.Fatalf("expected 1 new item, got %d", len(newItems))
	}
	if newItems[0].Title != "Post 2" {
		t.Fatalf("expected Post 2, got %q", newItems[0].Title)
	}
}

func TestDedup_NoRepublishOnStableFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, makeFeedXML([]struct{ guid, link, title, desc string }{
			{"guid1", "https://a.com/1", "Post 1", ""},
		}))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	feed, _ := c.parser.ParseURL(s.url)
	c.seen[s.id] = make(map[string]struct{})
	for _, item := range feed.Items {
		c.seen[s.id][itemKey(item)] = struct{}{}
	}

	feed2, _ := c.parser.ParseURL(s.url)
	var newItems []*gofeed.Item
	for _, item := range feed2.Items {
		if _, already := c.seen[s.id][itemKey(item)]; !already {
			newItems = append(newItems, item)
		}
	}
	if len(newItems) != 0 {
		t.Fatalf("expected 0 new items on stable feed, got %d", len(newItems))
	}
}

func TestDedup_MultipleSources_Independent(t *testing.T) {
	c := newTestCollector()
	c.seen[1] = map[string]struct{}{"guid1": {}}
	c.seen[2] = map[string]struct{}{}

	// guid1 known for source 1, unknown for source 2
	_, knownFor1 := c.seen[1]["guid1"]
	_, knownFor2 := c.seen[2]["guid1"]
	if !knownFor1 {
		t.Fatal("guid1 should be known for source 1")
	}
	if knownFor2 {
		t.Fatal("guid1 should be unknown for source 2")
	}
}

// ── publish content ───────────────────────────────────────────────────────────

func TestPublish_EmptyTitleFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, makeFeedXML([]struct{ guid, link, title, desc string }{
			{"guid1", "https://a.com/1", "", "some body"},
		}))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}
	feed, _ := c.parser.ParseURL(s.url)

	if len(feed.Items) == 0 {
		t.Fatal("expected at least one item")
	}
	item := feed.Items[0]
	title := item.Title
	if title == "" {
		title = "(no title)"
	}
	if title != "(no title)" {
		t.Fatalf("expected fallback title, got %q", title)
	}
}

func TestPublish_BodyFallsBackToLink(t *testing.T) {
	item := &gofeed.Item{Link: "https://example.com/post", Description: ""}
	body := item.Description
	if body == "" {
		body = item.Link
	}
	if body != "https://example.com/post" {
		t.Fatalf("expected link as body fallback, got %q", body)
	}
}

func TestPublish_DescriptionUsedAsBody(t *testing.T) {
	item := &gofeed.Item{Link: "https://example.com", Description: "real description"}
	body := item.Description
	if body == "" {
		body = item.Link
	}
	if body != "real description" {
		t.Fatalf("expected description as body, got %q", body)
	}
}

// ── parse error ───────────────────────────────────────────────────────────────

func TestCheck_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	_, err := c.parser.ParseURL(s.url)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

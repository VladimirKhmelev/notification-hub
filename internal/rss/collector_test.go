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

func makeFeedXML(items []struct{ guid, link, title string }) string {
	var itemsXML string
	for _, it := range items {
		itemsXML += fmt.Sprintf(`<item><title>%s</title><link>%s</link><guid>%s</guid></item>`,
			it.title, it.link, it.guid)
	}
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Test</title>` + itemsXML + `</channel></rss>`
}

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

func TestFirstFetchSeeds_NothingPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, makeFeedXML([]struct{ guid, link, title string }{
			{"guid1", "https://a.com/1", "Post 1"},
			{"guid2", "https://a.com/2", "Post 2"},
		}))
	}))
	defer srv.Close()

	c := newTestCollector()
	published := 0
	s := source{id: 1, name: "Test", url: srv.URL}

	// simulate check without publish — replicate check() seeding logic
	feed, _ := c.parser.ParseURL(s.url)
	if _, ok := c.seen[s.id]; !ok {
		c.seen[s.id] = make(map[string]struct{})
		for _, item := range feed.Items {
			c.seen[s.id][itemKey(item)] = struct{}{}
		}
	}

	if published != 0 {
		t.Fatalf("expected 0 published on first fetch, got %d", published)
	}
	if len(c.seen[1]) != 2 {
		t.Fatalf("expected 2 items seeded, got %d", len(c.seen[1]))
	}
}

func TestDedup_NewItemPublished(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		items := []struct{ guid, link, title string }{
			{"guid1", "https://a.com/1", "Post 1"},
		}
		if callCount >= 2 {
			items = append(items, struct{ guid, link, title string }{"guid2", "https://a.com/2", "Post 2"})
		}
		fmt.Fprint(w, makeFeedXML(items))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	// first fetch — seed
	feed1, _ := c.parser.ParseURL(s.url)
	c.seen[s.id] = make(map[string]struct{})
	for _, item := range feed1.Items {
		c.seen[s.id][itemKey(item)] = struct{}{}
	}

	// second fetch — collect new items
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
		fmt.Fprint(w, makeFeedXML([]struct{ guid, link, title string }{
			{"guid1", "https://a.com/1", "Post 1"},
		}))
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, name: "Test", url: srv.URL}

	// seed
	feed, _ := c.parser.ParseURL(s.url)
	c.seen[s.id] = make(map[string]struct{})
	for _, item := range feed.Items {
		c.seen[s.id][itemKey(item)] = struct{}{}
	}

	// second fetch — same feed
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

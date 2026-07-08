package rss

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/nats-io/nats.go"
	"github.com/vladimir/notification-hub/internal/natsutil"
	"github.com/vladimir/notification-hub/internal/uptime"
)

type source struct {
	id   int64
	name string
	url  string
}

type Collector struct {
	db       *sql.DB
	nc       *nats.Conn
	interval time.Duration
	parser   *gofeed.Parser
	// seen stores GUIDs/links already published per source to avoid duplicates
	seen map[int64]map[string]struct{}
}

func New(db *sql.DB, nc *nats.Conn, interval time.Duration) *Collector {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{Timeout: 10 * time.Second}
	return &Collector{
		db:       db,
		nc:       nc,
		interval: interval,
		parser:   fp,
		seen:     make(map[int64]map[string]struct{}),
	}
}

func (c *Collector) Run() {
	c.tick()
	for range time.Tick(c.interval) {
		c.tick()
	}
}

func (c *Collector) tick() {
	sources, err := c.loadSources()
	if err != nil {
		log.Printf("rss: load sources: %v", err)
		return
	}
	for _, s := range sources {
		c.check(s)
	}
}

func (c *Collector) loadSources() ([]source, error) {
	rows, err := c.db.Query(
		`SELECT id, name, config FROM sources WHERE type = 'rss'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []source
	for rows.Next() {
		var s source
		var configRaw []byte
		if err := rows.Scan(&s.id, &s.name, &configRaw); err != nil {
			return nil, err
		}
		var cfg struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(configRaw, &cfg); err != nil {
			log.Printf("rss: source %d bad config: %v", s.id, err)
			continue
		}
		if cfg.URL == "" {
			log.Printf("rss: source %d has no url, skipping", s.id)
			continue
		}
		s.url = cfg.URL
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func (c *Collector) check(s source) {
	feed, err := c.parser.ParseURL(s.url)
	if err != nil {
		log.Printf("rss: parse %s: %v", s.url, err)
		return
	}

	if _, ok := c.seen[s.id]; !ok {
		// first fetch — seed seen set without publishing
		c.seen[s.id] = make(map[string]struct{})
		for _, item := range feed.Items {
			c.seen[s.id][itemKey(item)] = struct{}{}
		}
		return
	}

	for _, item := range feed.Items {
		key := itemKey(item)
		if _, already := c.seen[s.id][key]; already {
			continue
		}
		c.seen[s.id][key] = struct{}{}
		c.publish(s, item)
	}
}

func (c *Collector) publish(s source, item *gofeed.Item) {
	title := item.Title
	if title == "" {
		title = "(no title)"
	}
	body := item.Description
	if body == "" {
		body = item.Link
	}

	msg := uptime.EventMsg{
		SourceID: s.id,
		Title:    title,
		Body:     body,
		Priority: "normal",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("rss: marshal event: %v", err)
		return
	}
	if err := c.nc.Publish(natsutil.Subject, data); err != nil {
		log.Printf("rss: publish event for source %d: %v", s.id, err)
	}
}

// itemKey returns a stable dedup key for an RSS item.
func itemKey(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}
	return item.Link
}

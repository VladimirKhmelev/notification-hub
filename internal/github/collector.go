package github

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vladimir/notification-hub/internal/natsutil"
	"github.com/vladimir/notification-hub/internal/uptime"
)

type source struct {
	id    int64
	name  string
	repo  string
	token string
}

type Collector struct {
	db       *sql.DB
	nc       *nats.Conn
	interval time.Duration
	client   *http.Client
	// seen stores release IDs already published per source
	seen map[int64]map[int64]struct{}
}

func New(db *sql.DB, nc *nats.Conn, interval time.Duration) *Collector {
	return &Collector{
		db:       db,
		nc:       nc,
		interval: interval,
		client:   &http.Client{Timeout: 10 * time.Second},
		seen:     make(map[int64]map[int64]struct{}),
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
		log.Printf("github: load sources: %v", err)
		return
	}
	for _, s := range sources {
		c.check(s)
	}
}

func (c *Collector) loadSources() ([]source, error) {
	rows, err := c.db.Query(
		`SELECT id, name, config FROM sources WHERE type = 'github'`,
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
			Repo  string `json:"repo"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(configRaw, &cfg); err != nil {
			log.Printf("github: source %d bad config: %v", s.id, err)
			continue
		}
		if cfg.Repo == "" {
			log.Printf("github: source %d has no repo, skipping", s.id)
			continue
		}
		s.repo = cfg.Repo
		s.token = cfg.Token
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

type release struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
	Prerelease bool `json:"prerelease"`
}

func (c *Collector) fetchReleases(s source) ([]release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=10", s.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d for %s", resp.StatusCode, s.repo)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (c *Collector) check(s source) {
	releases, err := c.fetchReleases(s)
	if err != nil {
		log.Printf("github: fetch releases for %s: %v", s.repo, err)
		return
	}

	if _, ok := c.seen[s.id]; !ok {
		// first fetch — seed seen set without publishing
		c.seen[s.id] = make(map[int64]struct{})
		for _, r := range releases {
			c.seen[s.id][r.ID] = struct{}{}
		}
		return
	}

	for _, r := range releases {
		if _, already := c.seen[s.id][r.ID]; already {
			continue
		}
		if r.Draft {
			continue
		}
		c.seen[s.id][r.ID] = struct{}{}
		c.publish(s, r)
	}
}

func (c *Collector) publish(s source, r release) {
	title := fmt.Sprintf("%s: %s", s.repo, r.TagName)
	if r.Name != "" && r.Name != r.TagName {
		title = fmt.Sprintf("%s: %s (%s)", s.repo, r.Name, r.TagName)
	}

	body := r.HTMLURL
	if r.Body != "" {
		// trim release notes to first 200 chars to keep notifications concise
		notes := r.Body
		if len(notes) > 200 {
			notes = notes[:200] + "…"
		}
		body = notes + "\n" + r.HTMLURL
	}

	priority := "normal"
	if r.Prerelease {
		priority = "low"
	}

	msg := uptime.EventMsg{
		SourceID: s.id,
		Title:    title,
		Body:     body,
		Priority: priority,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("github: marshal event: %v", err)
		return
	}
	if err := c.nc.Publish(natsutil.Subject, data); err != nil {
		log.Printf("github: publish event for source %d: %v", s.id, err)
	}
}

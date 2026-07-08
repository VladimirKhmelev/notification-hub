package uptime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/lib/pq"
)

type source struct {
	id     int64
	name   string
	url    string
	expect int // expected HTTP status, default 200
}

type Collector struct {
	db       *sql.DB
	interval time.Duration
	client   http.Client
	// last known state per source: true = up, false = down
	state map[int64]bool
}

func New(db *sql.DB, interval time.Duration) *Collector {
	return &Collector{
		db:       db,
		interval: interval,
		client:   http.Client{Timeout: 5 * time.Second},
		state:    make(map[int64]bool),
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
		log.Printf("uptime: load sources: %v", err)
		return
	}
	for _, s := range sources {
		c.check(s)
	}
}

func (c *Collector) loadSources() ([]source, error) {
	rows, err := c.db.Query(
		`SELECT id, name, config FROM sources WHERE type = 'uptime'`,
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
			URL    string `json:"url"`
			Expect int    `json:"expect"`
		}
		if err := json.Unmarshal(configRaw, &cfg); err != nil {
			log.Printf("uptime: source %d bad config: %v", s.id, err)
			continue
		}
		s.url = cfg.URL
		s.expect = cfg.Expect
		if s.expect == 0 {
			s.expect = 200
		}
		if s.url == "" {
			log.Printf("uptime: source %d has no url, skipping", s.id)
			continue
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func (c *Collector) check(s source) {
	up := c.ping(s)
	prev, seen := c.state[s.id]
	c.state[s.id] = up

	// first check — record initial state but don't emit event
	if !seen {
		return
	}
	if up == prev {
		return
	}

	title, body, priority := stateChange(s.name, s.url, up)
	log.Printf("uptime: state change detected for %s (up=%v)", s.name, up)
	if err := c.writeEvent(s.id, title, body, priority); err != nil {
		log.Printf("uptime: write event for source %d: %v", s.id, err)
	}
}

func (c *Collector) ping(s source) bool {
	resp, err := c.client.Get(s.url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == s.expect
}

func stateChange(name, url string, up bool) (title, body, priority string) {
	if up {
		return fmt.Sprintf("%s is UP", name),
			fmt.Sprintf("%s (%s) recovered", name, url),
			"normal"
	}
	return fmt.Sprintf("%s is DOWN", name),
		fmt.Sprintf("%s (%s) is not responding", name, url),
		"high"
}

func (c *Collector) writeEvent(sourceID int64, title, body, priority string) error {
	_, err := c.db.Exec(
		`INSERT INTO events (source_id, title, body, priority) VALUES ($1, $2, $3, $4)`,
		sourceID, title, body, priority,
	)
	return err
}

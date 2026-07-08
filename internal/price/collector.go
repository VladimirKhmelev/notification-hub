package price

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
	id        int64
	name      string
	coin      string
	currency  string
	threshold float64
	// direction: "above" fires when price crosses threshold from below,
	// "below" fires when price crosses threshold from above
	direction string
}

type Collector struct {
	db       *sql.DB
	nc       *nats.Conn
	interval time.Duration
	client   *http.Client
	// lastPrice stores the last known price per source to detect crossings
	lastPrice map[int64]float64
}

func New(db *sql.DB, nc *nats.Conn, interval time.Duration) *Collector {
	return &Collector{
		db:        db,
		nc:        nc,
		interval:  interval,
		client:    &http.Client{Timeout: 10 * time.Second},
		lastPrice: make(map[int64]float64),
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
		log.Printf("price: load sources: %v", err)
		return
	}
	for _, s := range sources {
		c.check(s)
	}
}

func (c *Collector) loadSources() ([]source, error) {
	rows, err := c.db.Query(
		`SELECT id, name, config FROM sources WHERE type = 'price'`,
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
			Coin      string  `json:"coin"`
			Currency  string  `json:"currency"`
			Threshold float64 `json:"threshold"`
			Direction string  `json:"direction"`
		}
		if err := json.Unmarshal(configRaw, &cfg); err != nil {
			log.Printf("price: source %d bad config: %v", s.id, err)
			continue
		}
		if cfg.Coin == "" || cfg.Threshold == 0 {
			log.Printf("price: source %d missing coin or threshold, skipping", s.id)
			continue
		}
		s.coin = cfg.Coin
		s.currency = cfg.Currency
		if s.currency == "" {
			s.currency = "usd"
		}
		s.threshold = cfg.Threshold
		s.direction = cfg.Direction
		if s.direction == "" {
			s.direction = "above"
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func (c *Collector) fetchPrice(coin, currency string) (float64, error) {
	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=%s",
		coin, currency,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coingecko returned %d", resp.StatusCode)
	}

	// response: {"bitcoin": {"usd": 104000.5}}
	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	prices, ok := result[coin]
	if !ok {
		return 0, fmt.Errorf("coin %q not found in response", coin)
	}
	price, ok := prices[currency]
	if !ok {
		return 0, fmt.Errorf("currency %q not found for coin %q", currency, coin)
	}
	return price, nil
}

func (c *Collector) check(s source) {
	price, err := c.fetchPrice(s.coin, s.currency)
	if err != nil {
		log.Printf("price: fetch %s/%s: %v", s.coin, s.currency, err)
		return
	}

	prev, seen := c.lastPrice[s.id]
	c.lastPrice[s.id] = price

	if !seen {
		log.Printf("price: initial %s price: %.2f %s", s.coin, price, s.currency)
		return
	}

	crossed := false
	if s.direction == "above" && prev < s.threshold && price >= s.threshold {
		crossed = true
	} else if s.direction == "below" && prev > s.threshold && price <= s.threshold {
		crossed = true
	}

	if !crossed {
		return
	}

	title := fmt.Sprintf("%s crossed %.0f %s (%s)", s.coin, s.threshold, s.currency, s.direction)
	body := fmt.Sprintf("Current price: %.2f %s (was %.2f)", price, s.currency, prev)
	log.Printf("price: threshold crossed for source %d: %s", s.id, title)

	msg := uptime.EventMsg{
		SourceID: s.id,
		Title:    title,
		Body:     body,
		Priority: "high",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("price: marshal event: %v", err)
		return
	}
	if err := c.nc.Publish(natsutil.Subject, data); err != nil {
		log.Printf("price: publish event for source %d: %v", s.id, err)
	}
}

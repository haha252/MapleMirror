package statbuffer

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type Counter struct {
	Views   int64
	Auth    int64
	WebAuth int64
	APIAuth int64
	Started int64
	Bytes   int64
}

func (c Counter) empty() bool {
	return c.Views == 0 && c.Auth == 0 && c.WebAuth == 0 &&
		c.APIAuth == 0 && c.Started == 0 && c.Bytes == 0
}

type Buffer struct {
	db       *sql.DB
	interval time.Duration

	mu       sync.Mutex
	public   map[string]Counter
	assets   map[string]Counter
	projects map[string]Counter
	nodes    map[string]int64
	stop     chan struct{}
}

func New(db *sql.DB, interval time.Duration) *Buffer {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Buffer{
		db:       db,
		interval: interval,
		public:   map[string]Counter{},
		assets:   map[string]Counter{},
		projects: map[string]Counter{},
		nodes:    map[string]int64{},
		stop:     make(chan struct{}),
	}
}

func (b *Buffer) Start(ctx context.Context) {
	if b == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(b.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = b.Flush(context.Background())
			case <-ctx.Done():
				_ = b.Flush(context.Background())
				return
			case <-b.stop:
				_ = b.Flush(context.Background())
				return
			}
		}
	}()
}

func (b *Buffer) Stop() {
	if b == nil {
		return
	}
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
}

func (b *Buffer) AddPublic(day string, c Counter) {
	if b == nil || day == "" || c.empty() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.public[day] = addCounter(b.public[day], c)
}

func (b *Buffer) AddAsset(assetID string, c Counter) {
	if b == nil || assetID == "" || c.empty() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.assets[assetID] = addCounter(b.assets[assetID], c)
}

func (b *Buffer) AddProject(projectID string, c Counter) {
	if b == nil || projectID == "" || c.empty() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.projects[projectID] = addCounter(b.projects[projectID], c)
}

func (b *Buffer) AddNode(nodeID string, bytes int64) {
	if b == nil || nodeID == "" || bytes == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodes[nodeID] += bytes
}

func addCounter(a, b Counter) Counter {
	return Counter{
		Views:   a.Views + b.Views,
		Auth:    a.Auth + b.Auth,
		WebAuth: a.WebAuth + b.WebAuth,
		APIAuth: a.APIAuth + b.APIAuth,
		Started: a.Started + b.Started,
		Bytes:   a.Bytes + b.Bytes,
	}
}

func (b *Buffer) Flush(ctx context.Context) error {
	if b == nil || b.db == nil {
		return nil
	}
	public, assets, projects, nodes := b.drain()
	if len(public) == 0 && len(assets) == 0 && len(projects) == 0 && len(nodes) == 0 {
		return nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		b.restore(public, assets, projects, nodes)
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for day, c := range public {
		if err := flushPublic(ctx, tx, day, c, now); err != nil {
			b.restore(public, assets, projects, nodes)
			return err
		}
	}
	for assetID, c := range assets {
		if err := flushAsset(ctx, tx, assetID, c, now); err != nil {
			b.restore(public, assets, projects, nodes)
			return err
		}
	}
	for projectID, c := range projects {
		if err := flushProject(ctx, tx, projectID, c, now); err != nil {
			b.restore(public, assets, projects, nodes)
			return err
		}
	}
	for nodeID, bytes := range nodes {
		if err := flushNode(ctx, tx, nodeID, bytes, now); err != nil {
			b.restore(public, assets, projects, nodes)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		b.restore(public, assets, projects, nodes)
		return err
	}
	return nil
}

func (b *Buffer) drain() (map[string]Counter, map[string]Counter, map[string]Counter, map[string]int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	public, assets, projects, nodes := b.public, b.assets, b.projects, b.nodes
	b.public = map[string]Counter{}
	b.assets = map[string]Counter{}
	b.projects = map[string]Counter{}
	b.nodes = map[string]int64{}
	return public, assets, projects, nodes
}

func (b *Buffer) restore(public, assets, projects map[string]Counter, nodes map[string]int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, value := range public {
		b.public[key] = addCounter(b.public[key], value)
	}
	for key, value := range assets {
		b.assets[key] = addCounter(b.assets[key], value)
	}
	for key, value := range projects {
		b.projects[key] = addCounter(b.projects[key], value)
	}
	for key, value := range nodes {
		b.nodes[key] += value
	}
}

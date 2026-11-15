package sysstats

import (
	"sync"
	"time"
)

// Collector periodically samples CPU and RAM utilization across supported OS platforms.
type Collector struct {
	mu           sync.RWMutex
	lastStats    Stats
	platformImpl platformCollector
	interval     time.Duration
	stopCh       chan struct{}
	running      bool
}

type platformCollector interface {
	collect() Stats
}

// NewCollector allocates a collector configured with a sampling interval.
func NewCollector(interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	c := &Collector{
		interval:     interval,
		platformImpl: newPlatformCollector(),
	}
	// Initial sample
	c.lastStats = c.platformImpl.collect()
	return c
}

// Stats returns the latest snapshot of system utilization metrics.
func (c *Collector) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastStats
}

// Start spawns a background goroutine refreshing metrics at the configured interval.
func (c *Collector) Start(onUpdate func(s Stats)) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				st := c.platformImpl.collect()
				c.mu.Lock()
				c.lastStats = st
				c.mu.Unlock()
				if onUpdate != nil {
					onUpdate(st)
				}
			}
		}
	}()
}

// Stop terminates the background collection loop.
func (c *Collector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	close(c.stopCh)
}

package forgedesk

import "time"

// Clock 是可注入的单调时钟。
type Clock interface {
	Now() time.Time
}

// ManualClock 是测试用的手动推进时钟。
type ManualClock struct {
	now time.Time
}

func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start}
}

func (c *ManualClock) Now() time.Time {
	return c.now
}

func (c *ManualClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

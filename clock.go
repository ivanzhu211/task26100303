package forgedesk

import "time"

// Clock 是可注入的单调时钟。
type Clock interface {
	Now() time.Time
}

// LeaseDuration 是租约的固定时长。
const LeaseDuration = 10 * time.Minute

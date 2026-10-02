package forgedesk

import "time"

// LeaseDuration 是租约的固定时长。
const LeaseDuration = 10 * time.Minute

// Token 是一次租约授权的凭据。
type Token struct {
	componentID string
	holder      string
	expiresAt   time.Time
	epoch       uint64
}

// ComponentID 返回该令牌对应的组件 ID。
func (t Token) ComponentID() string { return t.componentID }

// Holder 返回租约持有者。
func (t Token) Holder() string { return t.holder }

// ExpiresAt 返回租约到期时刻。
func (t Token) ExpiresAt() time.Time { return t.expiresAt }

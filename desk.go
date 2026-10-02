package forgedesk

import (
	"errors"
	"sync"
)

var (
	ErrUnknownComponent = errors.New("forgedesk: unknown component")
	ErrRootLease        = errors.New("forgedesk: cannot lease the root component")
	ErrLeaseHeld        = errors.New("forgedesk: component already has a valid lease")
	ErrInvalidToken     = errors.New("forgedesk: token is invalid or expired")
	ErrCycle            = errors.New("forgedesk: move would create a cycle")
)

// Desk 管理一棵固定的组件树及其租约与移动。
type Desk struct {
	mu     sync.Mutex
	clock  Clock
	rootID string
	parent map[string]string // 组件 ID -> 父组件 ID，根不在其中
	leases map[string]Token  // 组件 ID -> 最近一次签发的令牌
	epoch  uint64            // 令牌代次，保证重新授权可区分
}

// New 用给定的父子关系初始化 Desk。parentOf 中 key 为组件 ID，
// value 为其父组件 ID；根组件不在其中，由 rootID 指定。
func New(rootID string, parentOf map[string]string, clock Clock) (*Desk, error) {
	if _, ok := parentOf[rootID]; ok {
		return nil, errors.New("forgedesk: root must not have a parent")
	}
	parent := make(map[string]string, len(parentOf))
	for id, p := range parentOf {
		parent[id] = p
	}
	// 校验：每个节点沿父链最终到达根（无环、无悬空父引用）。
	for id := range parent {
		seen := map[string]bool{id: true}
		cur := id
		for {
			p, ok := parent[cur]
			if !ok {
				if cur != rootID {
					return nil, errors.New("forgedesk: parent chain does not reach the root")
				}
				break
			}
			if seen[p] {
				return nil, errors.New("forgedesk: parent relation contains a cycle")
			}
			seen[p] = true
			cur = p
		}
	}
	return &Desk{
		clock:  clock,
		rootID: rootID,
		parent: parent,
		leases: make(map[string]Token),
	}, nil
}

// Acquire 为非根组件取得租约，返回令牌。
func (d *Desk) Acquire(componentID, holder string) (Token, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if componentID == d.rootID {
		return Token{}, ErrRootLease
	}
	if _, ok := d.parent[componentID]; !ok {
		return Token{}, ErrUnknownComponent
	}
	now := d.clock.Now()
	if cur, ok := d.leases[componentID]; ok && now.Before(cur.expiresAt) {
		return Token{}, ErrLeaseHeld
	}
	d.epoch++
	tok := Token{
		componentID: componentID,
		holder:      holder,
		expiresAt:   now.Add(LeaseDuration),
		epoch:       d.epoch,
	}
	d.leases[componentID] = tok
	return tok, nil
}

// Move 凭令牌把对应组件及其子树移到 newParentID 下。
func (d *Desk) Move(token Token, newParentID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// 在持锁提交点校验令牌有效性：必须是当前令牌且未到期
	//（当前时间等于到期时间即视为失效）。
	cur, ok := d.leases[token.componentID]
	now := d.clock.Now()
	if !ok || cur.epoch != token.epoch || cur.holder != token.holder ||
		!now.Before(cur.expiresAt) {
		return ErrInvalidToken
	}
	if _, isRoot := d.parent[newParentID]; !isRoot && newParentID != d.rootID {
		return ErrUnknownComponent
	}
	// 环检测：从目标沿父链向上，若遇到被移动组件则成环。
	for cur := newParentID; ; {
		if cur == token.componentID {
			return ErrCycle
		}
		p, ok := d.parent[cur]
		if !ok {
			break
		}
		cur = p
	}
	d.parent[token.componentID] = newParentID
	return nil
}

// Snapshot 返回某个一致时刻的完整父子关系（不含根）。
func (d *Desk) Snapshot() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	snap := make(map[string]string, len(d.parent))
	for id, p := range d.parent {
		snap[id] = p
	}
	return snap
}

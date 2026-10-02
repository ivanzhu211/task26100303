package forgedesk

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Token 是一次租约授权的凭据。
type Token struct {
	componentID string
	holder      string
	serial      uint64
}

var (
	ErrUnknownComponent = errors.New("forgedesk: unknown component")
	ErrRootNotAllowed   = errors.New("forgedesk: operation not allowed on root")
	ErrLeaseHeld        = errors.New("forgedesk: component already leased")
	ErrInvalidToken     = errors.New("forgedesk: invalid or expired token")
	ErrCycle            = errors.New("forgedesk: move would create a cycle")
)

// lease 记录某组件当前生效的租约。
type lease struct {
	holder  string
	serial  uint64
	expires time.Time
}

// Desk 管理一棵固定的组件树及其租约。
type Desk struct {
	mu       sync.Mutex
	clock    Clock
	root     string
	parentOf map[string]string
	leases   map[string]lease
	serial   uint64
}

// New 用一棵合法组件树初始化 Desk。parentOf 给出每个非根组件的父组件。
func New(root string, parentOf map[string]string, clock Clock) (*Desk, error) {
	if clock == nil {
		return nil, errors.New("forgedesk: nil clock")
	}
	if root == "" {
		return nil, errors.New("forgedesk: empty root id")
	}
	if _, ok := parentOf[root]; ok {
		return nil, errors.New("forgedesk: root must not have a parent")
	}
	parents := make(map[string]string, len(parentOf))
	for id, parent := range parentOf {
		if id == "" || parent == "" {
			return nil, errors.New("forgedesk: empty component id")
		}
		parents[id] = parent
	}
	// 校验：每个组件都能沿父指针走到根（保证连通、无环、单父）。
	for id := range parents {
		seen := map[string]bool{}
		cur := id
		for cur != root {
			if seen[cur] {
				return nil, fmt.Errorf("forgedesk: cycle or unreachable component %q", id)
			}
			seen[cur] = true
			next, ok := parents[cur]
			if !ok {
				return nil, fmt.Errorf("forgedesk: component %q does not reach root", id)
			}
			cur = next
		}
	}
	return &Desk{
		clock:    clock,
		root:     root,
		parentOf: parents,
		leases:   make(map[string]lease),
	}, nil
}

// Acquire 为非根组件取得租约，返回令牌。
func (d *Desk) Acquire(componentID, holder string) (Token, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if componentID == d.root {
		return Token{}, ErrRootNotAllowed
	}
	if _, ok := d.parentOf[componentID]; !ok {
		return Token{}, ErrUnknownComponent
	}
	now := d.clock.Now()
	if l, ok := d.leases[componentID]; ok && now.Before(l.expires) {
		return Token{}, ErrLeaseHeld
	}
	// 过期（含 now == expires）即可重新取得；serial 递增保证令牌可区分。
	d.serial++
	d.leases[componentID] = lease{
		holder:  holder,
		serial:  d.serial,
		expires: now.Add(LeaseDuration),
	}
	return Token{componentID: componentID, holder: holder, serial: d.serial}, nil
}

// Move 凭令牌把对应组件及其子树移到 newParentID 下。
func (d *Desk) Move(token Token, newParentID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if newParentID != d.root {
		if _, ok := d.parentOf[newParentID]; !ok {
			return ErrUnknownComponent
		}
	}
	l, ok := d.leases[token.componentID]
	if !ok || l.holder != token.holder || l.serial != token.serial {
		return ErrInvalidToken
	}
	// 租约有效性在提交时判定：now >= expires 即失效。
	if !d.clock.Now().Before(l.expires) {
		return ErrInvalidToken
	}
	// 成环检查：newParentID 不得是被移动组件自身或其后代。
	for cur := newParentID; cur != d.root; {
		if cur == token.componentID {
			return ErrCycle
		}
		cur = d.parentOf[cur]
	}
	// 提交：只改一个父指针，子树整体随动。
	d.parentOf[token.componentID] = newParentID
	return nil
}

// Snapshot 返回某个一致时刻的完整父子关系（不含根）。
func (d *Desk) Snapshot() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()

	snap := make(map[string]string, len(d.parentOf))
	for id, parent := range d.parentOf {
		snap[id] = parent
	}
	return snap
}

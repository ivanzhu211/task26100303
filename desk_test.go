package forgedesk

import (
	"testing"
	"time"
)

// assertTree 校验 snapshot 是以 rootID 为根、包含全部非根组件的树：
// 无环、无丢节点、每个非根组件恰好一个父组件。
func assertTree(t *testing.T, snap map[string]string, rootID string, nonRoot []string) {
	t.Helper()
	if len(snap) != len(nonRoot) {
		t.Fatalf("snapshot has %d nodes, want %d", len(snap), len(nonRoot))
	}
	if _, ok := snap[rootID]; ok {
		t.Fatalf("root %q must not appear in snapshot", rootID)
	}
	for _, id := range nonRoot {
		cur, ok := snap[id]
		if !ok {
			t.Fatalf("node %q missing from snapshot", id)
		}
		seen := map[string]bool{id: true}
		for {
			if cur == rootID {
				break
			}
			if seen[cur] {
				t.Fatalf("cycle detected walking up from %q", id)
			}
			seen[cur] = true
			next, ok := snap[cur]
			if !ok {
				t.Fatalf("parent chain from %q does not reach root", id)
			}
			cur = next
		}
	}
}

func initialTree() map[string]string {
	return map[string]string{
		"A": "root", "B": "root", "C": "root",
		"a": "A", "b": "B", "c": "C",
	}
}

func TestCyclicMovesExactlyOneFails(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0))
	d, err := New("root", initialTree(), clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tokA, err := d.Acquire("A", "h1")
	if err != nil {
		t.Fatalf("Acquire A: %v", err)
	}
	tokB, err := d.Acquire("B", "h2")
	if err != nil {
		t.Fatalf("Acquire B: %v", err)
	}
	tokC, err := d.Acquire("C", "h3")
	if err != nil {
		t.Fatalf("Acquire C: %v", err)
	}

	// 固定时钟下：A->b、B->c 成功，C->a 因成环失败。
	if err := d.Move(tokA, "b"); err != nil {
		t.Fatalf("Move A->b: %v", err)
	}
	if err := d.Move(tokB, "c"); err != nil {
		t.Fatalf("Move B->c: %v", err)
	}
	if err := d.Move(tokC, "a"); err != ErrCycle {
		t.Fatalf("Move C->a = %v, want ErrCycle", err)
	}

	snap := d.Snapshot()
	assertTree(t, snap, "root", []string{"A", "B", "C", "a", "b", "c"})
	want := map[string]string{
		"A": "b", "B": "c", "C": "root",
		"a": "A", "b": "B", "c": "C",
	}
	for id, p := range want {
		if snap[id] != p {
			t.Fatalf("parent of %q = %q, want %q", id, snap[id], p)
		}
	}
}

func TestReacquireAfterExpiryAndOldTokenRejected(t *testing.T) {
	start := time.Unix(0, 0)
	clock := NewManualClock(start)
	d, err := New("root", initialTree(), clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	oldTok, err := d.Acquire("A", "holder")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !oldTok.ExpiresAt().Equal(start.Add(LeaseDuration)) {
		t.Fatalf("ExpiresAt = %v, want %v", oldTok.ExpiresAt(), start.Add(LeaseDuration))
	}

	// 有效租约存在时不能重复取得。
	if _, err := d.Acquire("A", "holder"); err != ErrLeaseHeld {
		t.Fatalf("re-Acquire while valid = %v, want ErrLeaseHeld", err)
	}

	// 推进到恰好到期：当前时间等于到期时间即视为失效。
	clock.Advance(LeaseDuration)
	newTok, err := d.Acquire("A", "holder")
	if err != nil {
		t.Fatalf("re-Acquire at expiry: %v", err)
	}
	if newTok == oldTok {
		t.Fatal("re-acquired token must differ from the expired one")
	}

	// 用新令牌成功移动。
	if err := d.Move(newTok, "b"); err != nil {
		t.Fatalf("Move with new token: %v", err)
	}

	// 旧令牌必须被拒绝，且新位置不被覆盖。
	if err := d.Move(oldTok, "c"); err != ErrInvalidToken {
		t.Fatalf("Move with old token = %v, want ErrInvalidToken", err)
	}
	snap := d.Snapshot()
	assertTree(t, snap, "root", []string{"A", "B", "C", "a", "b", "c"})
	if snap["A"] != "b" {
		t.Fatalf("parent of A = %q, want %q (old token must not overwrite)", snap["A"], "b")
	}
}

func TestMoveToSelfOrDescendantFails(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0))
	d, err := New("root", initialTree(), clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tok, err := d.Acquire("A", "h")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := d.Move(tok, "A"); err != ErrCycle {
		t.Fatalf("Move A->A = %v, want ErrCycle", err)
	}
	if err := d.Move(tok, "a"); err != ErrCycle {
		t.Fatalf("Move A->a = %v, want ErrCycle", err)
	}
	snap := d.Snapshot()
	if snap["A"] != "root" {
		t.Fatalf("failed moves must leave tree unchanged, parent of A = %q", snap["A"])
	}
}

func TestAcquireValidation(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0))
	d, err := New("root", initialTree(), clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := d.Acquire("root", "h"); err != ErrRootLease {
		t.Fatalf("Acquire root = %v, want ErrRootLease", err)
	}
	if _, err := d.Acquire("nope", "h"); err != ErrUnknownComponent {
		t.Fatalf("Acquire unknown = %v, want ErrUnknownComponent", err)
	}
}

func TestMoveWithExpiredTokenFails(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0))
	d, err := New("root", initialTree(), clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tok, err := d.Acquire("A", "h")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	clock.Advance(LeaseDuration) // 恰好到期即失效
	if err := d.Move(tok, "b"); err != ErrInvalidToken {
		t.Fatalf("Move at expiry = %v, want ErrInvalidToken", err)
	}
	if snap := d.Snapshot(); snap["A"] != "root" {
		t.Fatalf("expired move must not change tree, parent of A = %q", snap["A"])
	}
}

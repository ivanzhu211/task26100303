package forgedesk

import (
	"testing"
	"time"
)

// fakeClock 是可手动推进的单调时钟。
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// newTestDesk 构造 root 下有 A、B、C，a/b/c 分别是它们孩子的树。
func newTestDesk(t *testing.T) (*Desk, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	desk, err := New("root", map[string]string{
		"A": "root", "B": "root", "C": "root",
		"a": "A", "b": "B", "c": "C",
	}, clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return desk, clock
}

// assertTree 校验快照是以 root 为根、包含全部组件的无环单父树。
func assertTree(t *testing.T, desk *Desk, wantNodes int) {
	t.Helper()
	snap := desk.Snapshot()
	if len(snap) != wantNodes {
		t.Fatalf("snapshot has %d nodes, want %d", len(snap), wantNodes)
	}
	for id := range snap {
		seen := map[string]bool{}
		cur := id
		for cur != "root" {
			if seen[cur] {
				t.Fatalf("cycle detected at %q", cur)
			}
			seen[cur] = true
			parent, ok := snap[cur]
			if !ok {
				t.Fatalf("component %q unreachable from root", id)
			}
			cur = parent
		}
	}
}

func TestAcquireBasics(t *testing.T) {
	desk, _ := newTestDesk(t)

	if _, err := desk.Acquire("root", "h"); err != ErrRootNotAllowed {
		t.Fatalf("acquire root: got %v", err)
	}
	if _, err := desk.Acquire("nope", "h"); err != ErrUnknownComponent {
		t.Fatalf("acquire unknown: got %v", err)
	}
	if _, err := desk.Acquire("A", "h1"); err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	if _, err := desk.Acquire("A", "h2"); err != ErrLeaseHeld {
		t.Fatalf("re-acquire while held: got %v", err)
	}
}

func TestMoveValidation(t *testing.T) {
	desk, _ := newTestDesk(t)

	if err := desk.Move(Token{}, "root"); err != ErrInvalidToken {
		t.Fatalf("move with zero token: got %v", err)
	}
	tok, err := desk.Acquire("A", "h")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := desk.Move(tok, "nope"); err != ErrUnknownComponent {
		t.Fatalf("move to unknown parent: got %v", err)
	}
	if err := desk.Move(tok, "a"); err != ErrCycle {
		t.Fatalf("move under own child: got %v", err)
	}
	if err := desk.Move(tok, "A"); err != ErrCycle {
		t.Fatalf("move under self: got %v", err)
	}
	// 失败的移动不得改变树。
	snap := desk.Snapshot()
	if snap["A"] != "root" {
		t.Fatalf("failed move changed tree: A's parent = %q", snap["A"])
	}
}

// 场景一：三个有效令牌，A→b、B→c、C→a，恰好两个成功、一个成环失败。
func TestThreeMovesOneCycle(t *testing.T) {
	desk, _ := newTestDesk(t)

	tokA, err := desk.Acquire("A", "h1")
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	tokB, err := desk.Acquire("B", "h2")
	if err != nil {
		t.Fatalf("acquire B: %v", err)
	}
	tokC, err := desk.Acquire("C", "h3")
	if err != nil {
		t.Fatalf("acquire C: %v", err)
	}

	if err := desk.Move(tokA, "b"); err != nil {
		t.Fatalf("move A under b: %v", err)
	}
	if err := desk.Move(tokB, "c"); err != nil {
		t.Fatalf("move B under c: %v", err)
	}
	if err := desk.Move(tokC, "a"); err != ErrCycle {
		t.Fatalf("move C under a: got %v, want ErrCycle", err)
	}

	snap := desk.Snapshot()
	want := map[string]string{
		"A": "b", "a": "A",
		"B": "c", "b": "B",
		"C": "root", "c": "C",
	}
	for id, parent := range want {
		if snap[id] != parent {
			t.Fatalf("snapshot[%q] = %q, want %q", id, snap[id], parent)
		}
	}
	assertTree(t, desk, 6)
}

// 场景二：时钟推进到恰好到期，同一 holder 重新取得并成功移动；旧令牌被拒绝。
func TestExpiryAndReacquire(t *testing.T) {
	desk, clock := newTestDesk(t)

	oldTok, err := desk.Acquire("A", "h")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// 推进到恰好到期：当前时间等于到期时间即失效。
	clock.Advance(LeaseDuration)

	if err := desk.Move(oldTok, "B"); err != ErrInvalidToken {
		t.Fatalf("move at exact expiry: got %v, want ErrInvalidToken", err)
	}

	newTok, err := desk.Acquire("A", "h")
	if err != nil {
		t.Fatalf("re-acquire after expiry: %v", err)
	}
	if newTok == oldTok {
		t.Fatal("re-acquired token must differ from the expired one")
	}

	if err := desk.Move(newTok, "B"); err != nil {
		t.Fatalf("move with new token: %v", err)
	}
	if got := desk.Snapshot()["A"]; got != "B" {
		t.Fatalf("A's parent = %q, want B", got)
	}

	// 旧令牌必须被拒绝，且不得覆盖新位置。
	if err := desk.Move(oldTok, "C"); err != ErrInvalidToken {
		t.Fatalf("move with stale token: got %v, want ErrInvalidToken", err)
	}
	if got := desk.Snapshot()["A"]; got != "B" {
		t.Fatalf("stale token moved A: parent = %q, want B", got)
	}
	assertTree(t, desk, 6)
}

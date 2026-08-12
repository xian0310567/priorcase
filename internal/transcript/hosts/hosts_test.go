package hosts

import (
	"path/filepath"
	"strings"
	"testing"
)

// ★★ **파서를 만들고 배선을 잊으면 조용히 실패한다.**
//
// 그 상태는 알아채기가 특히 어렵다: 파서에는 통과하는 테스트가 잔뜩 있는데 아무도
// 그것을 부르지 않는다. 사용자에게는 "그 도구를 써도 아무것도 안 잡힌다" 로 보이고,
// 그건 "결정이 없었다" 와 구별되지 않는다.
//
// 그래서 목록이 비지 않았는지, 각 항목이 다 채워졌는지 여기서 지킨다.
func TestAllHostsAreFullyWired(t *testing.T) {
	all := All()
	if len(all) < 2 {
		t.Fatalf("호스트가 %d개다 — Claude Code 와 Codex CLI 둘 다 있어야 한다", len(all))
	}
	seen := map[string]bool{}
	required := 0
	for _, h := range all {
		if h.Name == "" {
			t.Error("이름 없는 호스트가 있다 — 진단에 뭐라고 쓸 것인가")
		}
		if seen[h.Name] {
			t.Errorf("이름이 겹친다: %s", h.Name)
		}
		seen[h.Name] = true
		if h.DefaultRoot == nil {
			t.Errorf("%s: DefaultRoot 가 없다 — 기록 자리를 못 찾는다", h.Name)
		}
		if h.List == nil {
			t.Errorf("%s: List 가 없다 — 파일을 못 찾는다", h.Name)
		}
		if h.Parse == nil {
			t.Errorf("%s: Parse 가 없다 — 파일을 찾아도 못 읽는다", h.Name)
		}
		if h.Required {
			required++
		}
	}
	// 필수 호스트가 없으면 override 경로(ClaudeCode)가 nil 을 준다.
	if required != 1 {
		t.Errorf("필수 호스트가 %d개다 — 정확히 하나(Claude Code)여야 한다", required)
	}
}

// ★★ **override 는 "여기도" 가 아니라 "여기만" 이다.**
//
// 격리된 자리를 지정한 사람이 나머지 호스트의 기본 자리까지 훑게 되면, 지정한 곳만
// 볼 줄 알았는데 홈 디렉토리의 남의 대화를 읽는 것이 된다. 실제로 테스트가 진짜
// ~/.codex 의 세션 1,729개를 훑다가 멈추면서 드러났다.
func TestResolveOverrideIsExclusive(t *testing.T) {
	rs, err := Resolve("/tmp/격리된자리")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		var names []string
		for _, r := range rs {
			names = append(names, r.Host.Name+"→"+r.Root)
		}
		t.Fatalf("override 를 줬는데 %d개를 본다 (%s) — 지정한 곳만 봐야 한다",
			len(rs), strings.Join(names, ", "))
	}
	if rs[0].Root != "/tmp/격리된자리" {
		t.Errorf("Root=%q", rs[0].Root)
	}
	if !rs[0].Host.Required {
		t.Error("override 는 필수 호스트(Claude Code)에 걸려야 한다")
	}
}

// override 가 없으면 아는 호스트를 전부 본다.
func TestResolveWithoutOverrideCoversEveryHost(t *testing.T) {
	rs, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != len(All()) {
		t.Errorf("호스트 %d개 중 %d개만 풀렸다", len(All()), len(rs))
	}
	for _, r := range rs {
		if r.Root == "" {
			t.Errorf("%s: 루트가 비었다", r.Host.Name)
		}
	}
}

// ★★ **경로를 못 고르면 nil 이다. 기본값으로 떨어지면 안 된다.**
//
// 엉뚱한 파서로 읽으면 발화가 0개로 나오는데, 그건 "그 세션에 결정이 없었다" 와
// 구별되지 않는다 — 안전망이 도는 것처럼 보이면서 아무것도 안 하는 상태가 된다.
func TestForRefusesUnknownPath(t *testing.T) {
	rs := []Resolved{
		{Host: Host{Name: "A"}, Root: "/home/a"},
		{Host: Host{Name: "B"}, Root: "/home/b"},
	}
	if got := For("/somewhere/else/x.jsonl", rs); got != nil {
		t.Errorf("모르는 경로를 %s 로 골랐다 — 조용히 틀린 파서로 읽힌다", got.Host.Name)
	}
}

// ★ 문자열 접두어로 맞추면 이웃 디렉토리를 삼킨다.
func TestForMatchesOnPathBoundary(t *testing.T) {
	rs := []Resolved{
		{Host: Host{Name: "A"}, Root: "/home/a"},
		{Host: Host{Name: "B"}, Root: "/home/b"},
	}
	// /home/abc 는 /home/a 아래가 아니다.
	if got := For("/home/abc/x.jsonl", rs); got != nil {
		t.Errorf("/home/abc 를 %s(/home/a) 로 골랐다 — 경로 경계를 안 본다", got.Host.Name)
	}
	if got := For(filepath.Join("/home/a", "s", "x.jsonl"), rs); got == nil || got.Host.Name != "A" {
		t.Error("/home/a 아래를 A 로 못 골랐다")
	}
}

// ★ 루트가 겹치면 더 깊은 쪽이 이긴다.
//
// 한 호스트의 자리가 다른 호스트 안에 있을 때, 얕은 쪽이 먼저 잡으면 그 파일을
// 남의 파서로 읽는다.
func TestForPrefersDeeperRoot(t *testing.T) {
	rs := []Resolved{
		{Host: Host{Name: "얕은"}, Root: "/home/x"},
		{Host: Host{Name: "깊은"}, Root: "/home/x/inner"},
	}
	got := For("/home/x/inner/s.jsonl", rs)
	if got == nil || got.Host.Name != "깊은" {
		t.Errorf("겹치는 루트에서 얕은 쪽을 골랐다: %v", got)
	}
}

// ★★ **호출자가 호스트를 알면 경로로 추측하지 않는다.**
//
// 훅은 자기를 부른 호스트를 안다. 목록이 하나면 그것으로 읽어야 하고, 그래야
// 호스트가 기록 자리를 옮겨도 훅이 자기 대화를 계속 읽는다.
func TestForUsesSingletonWithoutRootMatching(t *testing.T) {
	rs := ClaudeCode()
	if len(rs) != 1 {
		t.Fatalf("ClaudeCode()가 %d개를 줬다", len(rs))
	}
	if rs[0].Root != "" {
		t.Errorf("Root=%q — 비어 있어야 경로 매칭에 안 걸린다", rs[0].Root)
	}
	got := For("/전혀/다른/자리/s.jsonl", rs)
	if got == nil {
		t.Fatal("목록이 하나인데 못 골랐다 — 훅이 자기 대화를 못 읽는다")
	}
	if got.Host.Parse == nil {
		t.Error("고른 호스트에 파서가 없다")
	}
}

// ★★ **루트가 지정된 목록은 개수와 무관하게 경로로 맞춘다.**
//
// "목록이 하나면 그것" 만 보고 넘겼더니, 루트를 지정한 한 개짜리 목록이 남의 자리
// 파일까지 삼켰다. 훅의 신호는 "개수가 하나" 가 아니라 **"루트가 비었다"** 이다.
func TestForStillMatchesRootWhenSingletonHasRoot(t *testing.T) {
	rs := []Resolved{{Host: Host{Name: "A"}, Root: "/home/a"}}
	if got := For("/전혀/다른/곳/x.jsonl", rs); got != nil {
		t.Errorf("루트가 있는 한 개짜리 목록이 남의 경로를 삼켰다 (%s)", got.Host.Name)
	}
	if got := For("/home/a/x.jsonl", rs); got == nil {
		t.Error("자기 루트 아래인데 못 골랐다")
	}
}

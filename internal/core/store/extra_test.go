package store

import (
	"strings"
	"testing"
)

// ★★ 사용자가 Obsidian 에서 노트에 필드 하나만 추가하면 그 결정이 **영구히 사라졌다.**
//
// KnownFields(true) 는 "잉여 키를 조용히 버리지 않겠다" 는 뜻이었는데, 버리지 않는
// 대신 **읽기를 포기해서** 결과적으로 더 크게 잃고 있었다. 색인에서 빠지고, 회수에서
// 안 나오고, review 로 고칠 수도 없다 — 사용자는 자기가 뭘 깨뜨렸는지 모른다.
// `aliases` 와 `cssclasses` 는 Obsidian 사용자가 가장 흔히 넣는 두 키다.
func TestUserAddedFieldsSurviveRoundTrip(t *testing.T) {
	raw := []byte(`---
type: decision
date: 2026-08-09
domain: [alpha]
summary: "저장 엔진"
status: active
outcome: pending
supersedes: ""
related: []
tags: [db]
source_session: ""
aliases: [손편집, 별칭]
cssclasses: [wide]
publish: true
---

## 결정

사용자가 회고를 여기 적었다.
`)
	m, body, err := ParseFrontmatter(raw)
	if err != nil {
		t.Fatalf("사용자가 추가한 키 때문에 노트를 못 읽는다: %v", err)
	}
	if m.Summary != "저장 엔진" || m.Tags[0] != "db" {
		t.Fatalf("10키가 깨졌다: %+v", m)
	}
	if !strings.Contains(string(body), "회고") {
		t.Fatal("본문이 안 왔다")
	}
	if len(m.Extra) != 3 {
		t.Fatalf("잉여 키 %d개, want 3: %v", len(m.Extra), m.Extra)
	}

	out := string(EmitFrontmatter(m))
	for _, want := range []string{"aliases: [손편집, 별칭]", "cssclasses: [wide]", "publish: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q 가 되쓰이지 않았다 — 사용자가 쓴 것을 우리가 지운다:\n%s", want, out)
		}
	}
	// 10키가 먼저, 잉여가 뒤. 순서가 섞이면 diff 가 소음이 된다.
	if strings.Index(out, "source_session:") > strings.Index(out, "aliases:") {
		t.Errorf("잉여 키가 10키 앞에 왔다:\n%s", out)
	}
}

// 두 번 저장해도 바이트가 같아야 한다. 맵은 순회 순서가 무작위라 정렬을 안 하면
// 고치지도 않은 노트의 diff 가 매번 달라진다.
func TestExtraEmissionIsDeterministic(t *testing.T) {
	raw := []byte(`---
type: decision
date: 2026-08-09
domain: [alpha]
summary: "x"
status: active
outcome: pending
supersedes: ""
related: []
tags: []
source_session: ""
zebra: 1
alpha: 2
middle: 3
banana: 4
---

x
`)
	m, _, err := ParseFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	first := string(EmitFrontmatter(m))
	for i := 0; i < 20; i++ {
		if got := string(EmitFrontmatter(m)); got != first {
			t.Fatalf("%d번째 방출이 다르다:\n%s\n---\n%s", i, first, got)
		}
	}
	if strings.Index(first, "alpha:") > strings.Index(first, "zebra:") {
		t.Errorf("사전순이 아니다:\n%s", first)
	}
}

// 왕복이 고정점이어야 한다 — 읽고 쓴 것을 다시 읽으면 같아야 한다.
func TestRoundTripIsFixedPoint(t *testing.T) {
	raw := []byte(`---
type: decision
date: 2026-08-09
domain: [alpha, beta]
summary: "따옴표 \"안\" 에 인용"
status: active
outcome: pending
supersedes: ""
related: []
tags: [a, b]
source_session: "S1"
aliases: [별칭]
nested:
    k: v
---

## 결정

본문
`)
	m1, body1, err := ParseFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	once := append(EmitFrontmatter(m1), append([]byte("\n"), body1...)...)
	m2, body2, err := ParseFrontmatter(once)
	if err != nil {
		t.Fatalf("우리가 쓴 것을 우리가 못 읽는다: %v", err)
	}
	twice := append(EmitFrontmatter(m2), append([]byte("\n"), body2...)...)
	if string(once) != string(twice) {
		t.Errorf("왕복이 고정점이 아니다:\n%s\n=====\n%s", once, twice)
	}
	if len(m2.Extra) != 2 {
		t.Errorf("2회차에 잉여 키가 %d개: %v", len(m2.Extra), m2.Extra)
	}
}

// 잉여 키가 없으면 아무것도 덧붙이지 않는다 — 기존 62건의 바이트가 안 바뀌어야 한다.
func TestNoExtraEmitsNothingAdditional(t *testing.T) {
	m := Meta{Type: "decision", Date: "2026-08-09", Domain: []string{"alpha"},
		Summary: "x", Status: "active", Outcome: "pending", Related: []string{}, Tags: []string{}}
	out := string(EmitFrontmatter(m))
	if strings.Count(out, "---") != 2 {
		t.Errorf("펜스가 2개가 아니다:\n%s", out)
	}
	if !strings.HasSuffix(out, "source_session: \"\"\n---\n") {
		t.Errorf("10키 뒤에 뭔가 붙었다:\n%s", out)
	}
}

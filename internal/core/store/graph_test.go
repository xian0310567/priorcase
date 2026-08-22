package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAllStemsSeesWholeVaultNotJustDeclaredDomains(t *testing.T) {
	l := fixtureLayout(t)

	// 실볼트 실측: frontmatter 링크 214개 중 49개(23%)가 결정이 **아닌** 문서를
	// 가리킨다 ([[00-omni-프로젝트-개요]], [[_meta/00-볼트-네이밍-규약]]).
	// 결정 폴더만 보면 그 49건이 전부 오탐이 된다.
	writeFile(t, l, "_meta/00-볼트-네이밍-규약.md", "# 규약\n")
	writeFile(t, l, "alpha/00-alpha-프로젝트-개요.md", "# 개요\n")
	// 설정에 없는 도메인 폴더. DecisionStems 계열은 화이트리스트를 타서 이걸 못 본다 —
	// 검사기가 색인의 사각지대를 상속하면 안 된다 (실볼트에서 bard 14건이 그랬다).
	writeFile(t, l, "gamma/decisions/gamma-결정-미선언-2026-08-05.md", "---\ntype: decision\n---\n")
	// 마크다운이 아닌 것과 옵시디언 내부 파일은 위키링크 대상이 아니다.
	writeFile(t, l, "alpha/그림.png", "not markdown")
	writeFile(t, l, ".obsidian/workspace.md", "# 내부\n")

	stems, err := l.AllStems()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"alpha-결정-저장엔진-2026-08-01",   // 선언된 도메인의 결정
		"common-결정-로케일함정-2026-08-04", //
		"00-볼트-네이밍-규약",               // 결정이 아닌 문서
		"00-alpha-프로젝트-개요",           //
		"gamma-결정-미선언-2026-08-05",    // 미선언 도메인
	} {
		if !stems[want] {
			t.Errorf("AllStems 에 %q 가 없다", want)
		}
	}
	for _, notWant := range []string{"그림", "workspace"} {
		if stems[notWant] {
			t.Errorf("AllStems 에 %q 가 있으면 안 된다", notWant)
		}
	}
}

// writeFile 은 볼트 안에 파일 하나를 만든다.
func writeFile(t *testing.T, l *Layout, rel, body string) {
	t.Helper()
	p := filepath.Join(l.vault, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLinkTargetsSplitsRelatedAndSupersedes(t *testing.T) {
	m := Meta{
		Supersedes: []string{"[[alpha-결정-옛것-2026-08-01]]"},
		Related: []string{
			"[[beta-결정-딴것-2026-08-03]]",
			"common-결정-맨stem-2026-08-04", // [[ ]] 없이 저장된 실볼트 사례
			"",                           // 빈 칸은 링크가 아니다
			"   ",
		},
	}
	want := []Link{
		{Target: "alpha-결정-옛것-2026-08-01", Kind: KindSupersedes, Field: "supersedes"},
		{Target: "beta-결정-딴것-2026-08-03", Kind: KindCites, Field: "related"},
		{Target: "common-결정-맨stem-2026-08-04", Kind: KindCites, Field: "related", Unwrapped: true},
	}
	if got := LinkTargets(m); !reflect.DeepEqual(got, want) {
		t.Errorf("LinkTargets()\n got %+v\nwant %+v", got, want)
	}
}

func TestLinkTargetsOnEmptyMetaGivesNothing(t *testing.T) {
	// supersedes 는 EmitFrontmatter 가 항상 쓰므로 디스크의 전건이 `supersedes: ""`
	// 를 갖는다. 그 빈 값이 링크로 세지면 볼트 전건이 dangling 이 된다.
	if got := LinkTargets(Meta{Supersedes: []string{""}, Related: nil}); len(got) != 0 {
		t.Errorf("LinkTargets(빈 Meta) = %+v, 아무것도 없어야 한다", got)
	}
}

func TestNormalizeLinkWrapsBareStem(t *testing.T) {
	// 실볼트에 맨 stem 이 실제로 있었다 (bard-결정-godot-headless-import-failure-signal
	// 의 related). 옵시디언은 그걸 링크로 안 읽으므로 죽은 문자열이 된다.
	cases := []struct{ in, want string }{
		{"alpha-결정-저장엔진-2026-08-01", "[[alpha-결정-저장엔진-2026-08-01]]"},
		{"[[alpha-결정-저장엔진-2026-08-01]]", "[[alpha-결정-저장엔진-2026-08-01]]"},
		{"  [[alpha-결정-저장엔진-2026-08-01]]  ", "[[alpha-결정-저장엔진-2026-08-01]]"},
		{"[[ alpha-결정-저장엔진-2026-08-01 ]]", "[[alpha-결정-저장엔진-2026-08-01]]"},
	}
	for _, c := range cases {
		got, err := NormalizeLink(c.in)
		if err != nil {
			t.Errorf("NormalizeLink(%q) 에러: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeLink(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeLinkRejectsPathEscapes(t *testing.T) {
	// supersede.go 주석이 실측으로 기록한 사고: "../../CLAUDE" 가 frontmatter 에
	// 그대로 안착했다. ResolveStem 이 막던 경로 순회를 --related 가 우회했다.
	// 조용히 통과시키는 것이 곧 조용히 틀리는 것이다.
	bad := []string{
		"",
		"   ",
		"[[]]",
		"[[   ]]",
		"../CLAUDE",
		"../../CLAUDE",
		"[[../CLAUDE]]",
		"alpha/decisions/x",
		`alpha\decisions\x`,
		"[[a/b]]",
	}
	for _, in := range bad {
		got, err := NormalizeLink(in)
		if err == nil {
			t.Errorf("NormalizeLink(%q) = %q, 에러를 기대했다", in, got)
		}
	}
}

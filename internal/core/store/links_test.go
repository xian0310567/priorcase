package store

import "testing"

// **표 안의 `\|` 를 안 끊으면 멀쩡한 링크가 전부 깨진 것으로 보인다.**
//
// 실측 중에 실제로 이 실수를 했다 — `[[대상\|별칭]]` 이 `대상\` 로 잡혀 후보가
// 68개로 부풀었고, 고치니 28개가 됐다. 오탐 93%로 본문 검사를 포기했던 2026-08-15
// 의 판단도 일부는 이런 순진한 파싱 탓이었을 수 있다.
func TestLinkTargetHandlesEscapedAliasPipe(t *testing.T) {
	for raw, want := range map[string]string{
		`대상`:     "대상",
		`대상|별칭`:  "대상",
		`대상\|별칭`: "대상", // 표 안에서 이스케이프된 파이프
		`대상#헤딩`:  "대상",
		`00-도구-선택-가이드\|01 · 가이드`: "00-도구-선택-가이드",
		`  여백  `: "여백",
	} {
		if got := LinkTarget(raw); got != want {
			t.Errorf("LinkTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

// ★★ **코드 안의 링크는 링크가 아니다.**
//
// 스키마·규약 문서가 자리표시자를 코드로 적는다. 그걸 세면 그 문서가 영원히
// 짖고, 늘 뜨는 경고는 무시를 가르쳐 진짜 깨진 링크까지 묻는다.
//
// 이 판정이 두 벌이었을 때 같은 볼트를 두고 doctor 는 "링크가 전부 걸린다",
// 통계는 "끊긴 링크 36건" 이라고 했다.
func TestBodyLinksSkipsCode(t *testing.T) {
	body := "본문 [[진짜-결정-2026-01-01]] 이다\n" +
		"인라인 `[[코드-속-2026-01-01]]` 는 아니다\n" +
		"```\n[[펜스-속-2026-01-01]]\n```\n" +
		"~~~\n[[물결펜스-속-2026-01-01]]\n~~~\n" +
		"닫힌 뒤 [[또하나-2026-01-01]]\n"
	got := BodyLinks(body)
	want := []string{"진짜-결정-2026-01-01", "또하나-2026-01-01"}
	if len(got) != len(want) {
		t.Fatalf("%v — %v 여야 한다", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] %q, want %q", i, got[i], want[i])
		}
	}
}

// 한 줄에 링크가 둘이면 **탐욕 매칭이 둘을 하나로 삼킨다.**
func TestBodyLinksTakesBothOnOneLine(t *testing.T) {
	got := BodyLinks("[[가-2026-01-01]] 와 [[나-2026-01-02]] 를 견줬다")
	if len(got) != 2 || got[0] != "가-2026-01-01" || got[1] != "나-2026-01-02" {
		t.Errorf("%v — 둘 다 나와야 한다", got)
	}
}

// Links 는 frontmatter 와 본문을 함께 준다 — 그래프는 둘 다여야 성립한다.
func TestLinksMergesFrontmatterAndBody(t *testing.T) {
	n := Note{
		Meta: Meta{
			Related:    []string{"[[관련-2026-01-01]]", "  "},
			Supersedes: LinkList{"뒤집힌-2026-01-02"},
		},
		Body: []byte("본문 [[본문링크-2026-01-03]]"),
	}
	got := n.Links()
	want := map[string]bool{"관련-2026-01-01": true, "뒤집힌-2026-01-02": true, "본문링크-2026-01-03": true}
	if len(got) != len(want) {
		t.Fatalf("%v — %d개여야 한다", got, len(want))
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("뜻밖의 링크 %q", v)
		}
	}
}

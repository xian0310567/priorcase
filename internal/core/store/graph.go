package store

import (
	"fmt"
	"strings"
)

// LinkKind 는 링크가 무엇을 뜻하는지다.
//
// **저장하지 않는다.** frontmatter 에 종류를 적는 필드를 새로 만들면 그것이
// 사람 손과 어긋날 새 자리가 된다. 종류는 어느 필드에서 나왔는지로 정해진다.
type LinkKind int

const (
	KindCites      LinkKind = iota // related — "이걸 근거로 삼았다"
	KindSupersedes                 // supersedes — "이 결정이 저것을 뒤집는다"
)

// Link 는 노트 하나가 내보내는 링크 한 줄이다.
type Link struct {
	Target string   // `[[ ]]` 를 벗긴 stem
	Kind   LinkKind //
	Field  string   // 어느 frontmatter 키에서 나왔나 — 경고 문구가 사람에게 자리를 알려 준다
	// Unwrapped 는 원본에 `[[ ]]` 가 없었다는 뜻이다. 옵시디언은 그런 값을 링크로
	// 읽지 않으므로 백링크 패널에 안 뜬다 — 우리 쪽에서만 보이는 반쪽 링크다.
	Unwrapped bool
}

// LinkTargets 는 frontmatter 의 supersedes·related 에서 링크를 뽑는다.
//
// **본문은 보지 않는다.** 실볼트 실측(2026-08-15)으로 본문 위키링크 291개 중
// 대상이 없는 것이 15개인데 진짜는 1개뿐이었다 — 오탐률 93%. 나머지는 ```toml
// 펜스 안의 `[[domain]]`·`[[vault]]` (TOML array-of-tables 문법), `[[옛이름]]`
// 자리표시자, `[[벧전 5:7]]` 성경 인용이다. health.go 가 이 프로젝트의 죄목으로
// 드는 "늘 뜨는 경고는 무시하는 법을 가르친다" 에 정면으로 걸린다.
//
// frontmatter 는 우리 방출기가 쓰는 자리라 기준선이 깨끗하다(실측 dangling 0).
//
// 빈 값은 링크가 아니다 — EmitFrontmatter 가 supersedes 를 omitempty 없이 항상
// 쓰므로 디스크의 전건이 `supersedes: ""` 를 갖는다. 그것을 세면 볼트 전건이
// dangling 이 된다.
func LinkTargets(m Meta) []Link {
	var out []Link
	for _, s := range m.Supersedes {
		if l, ok := parseLink(s, KindSupersedes, "supersedes"); ok {
			out = append(out, l)
		}
	}
	for _, s := range m.Related {
		if l, ok := parseLink(s, KindCites, "related"); ok {
			out = append(out, l)
		}
	}
	return out
}

// parseLink 는 값 하나를 Link 로 만든다. 링크가 아니면 ok=false.
//
// **모양이 나빠도 버리지 않는다.** 경로 조각이 든 값은 NormalizeLink 가 쓰기에서
// 막지만, 이미 디스크에 있는 것은 읽어서 알려야 한다 — 어차피 AllStems 에 없으므로
// 끊어진 링크로 보고된다. 조용히 건너뛰면 그 노트만 검사에서 사라진다.
func parseLink(raw string, k LinkKind, field string) (Link, bool) {
	s := strings.TrimSpace(NFC(raw))
	wrapped := strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]")
	if wrapped {
		s = strings.TrimSpace(s[2 : len(s)-2])
	}
	if s == "" {
		return Link{}, false
	}
	return Link{Target: s, Kind: k, Field: field, Unwrapped: !wrapped}, true
}

// NormalizeLink 는 링크 값 하나를 정본 `[[stem]]` 형태로 만든다.
//
// **`[[ ]]` 를 씌우고 벗기는 규칙이 사는 유일한 자리다.** 예전에는 그 규칙이
// capture/supersede.go 의 문자열 리터럴 한 곳에만 있었고, 읽는 쪽(회수·색인)은
// 아무도 벗기지 않았다. 검사기를 붙이면서 같은 리터럴이 복제되면 스펙 §4.1 이
// 없애려던 "같은 일을 하는 코드 두 벌" 이 읽기 쪽에서 다시 생긴다.
//
// **맨 stem 도 받아 감싼다.** MCP 도구 설명이 "위키링크 **또는** stem" 이라고
// 두 형식을 다 권한 탓에 실볼트에 `[[ ]]` 없는 값이 실제로 남았다 — 옵시디언은
// 그것을 링크로 읽지 않으므로 백링크 패널에 안 뜨는 죽은 문자열이 된다.
func NormalizeLink(s string) (string, error) {
	stem := strings.TrimSpace(NFC(s))
	stem = strings.TrimSuffix(strings.TrimPrefix(stem, "[["), "]]")
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return "", fmt.Errorf("빈 링크")
	}
	// ResolveStem(paths.go:111) 과 같은 모양 검사다. 거기는 쓰기 경로의 보안
	// 검증이고 여기는 링크 값의 검증인데, **막아야 할 문자열이 같다** — 위키링크는
	// basename 으로 해석되므로 경로 조각이 들어간 링크는 어차피 아무것도 안 가리킨다.
	if strings.ContainsAny(stem, `/\`) || strings.Contains(stem, "..") {
		return "", fmt.Errorf("허용되지 않는 링크: %q (경로 조각이 들어 있다)", stem)
	}
	return "[[" + stem + "]]", nil
}

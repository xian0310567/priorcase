// Package toolsum 은 도구 호출 하나를 발췌에 실을 한 줄로 줄인다.
//
// **호스트마다 파서는 다르지만 이 요약은 같다.** 도구 이름과 대상(파일 경로·명령
// 첫 줄)만 남기고 결과는 안 남긴다는 규칙, 준비 동작(cd·export)을 건너뛴다는 규칙,
// 그리고 **자격증명을 가린다**는 규칙은 호스트와 무관하게 옳다.
//
// 특히 가림(redact)은 호스트별로 복제하면 안 된다. 한쪽만 고치면 다른 쪽이 조용히
// 토큰을 상태 파일과 판별기로 흘린다 — 복제된 보안 규칙은 반드시 어긋난다.
package toolsum

import (
	"encoding/json"
	"regexp"
	"strings"
)

// **무엇을 했는지만 남기고 결과는 안 남긴다.** tool_result 본문은 이 세션만 840KB 라
// 담으면 발췌가 터진다. 도구 이름과 대상(파일 경로·명령 첫 줄)만으로도 "무슨 일이
// 있었나" 는 충분히 전해지고, 판별기가 필요로 하는 것도 그것이다.
// Line 은 도구 호출 하나를 한 줄로 줄인다. 없으면 빈 문자열.
func Line(name string, input json.RawMessage) string {
	if name == "" {
		return ""
	}
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return name
	}

	// 도구마다 "무엇에" 했는지가 다른 키에 있다. 흔한 것부터 본다.
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if s, ok := in[k].(string); ok && s != "" {
			return name + " " + s
		}
	}
	if s, ok := in["command"].(string); ok && s != "" {
		return name + " " + Redact(Command(s))
	}
	for _, k := range []string{"description", "prompt", "query", "pattern", "url"} {
		if s, ok := in[k].(string); ok && s != "" {
			return name + " " + Redact(FirstLine(s))
		}
	}
	return name
}

// maxToolLine 은 한 줄의 상한이다. 명령이 길면 앞만 남긴다 — 무엇을 했는지는 보통
// 앞에 있다.
const maxToolLine = 120

// prelude 는 명령 앞에 붙는 준비 동작이다. 신호가 없다.
//
// 실측으로 드러났다 — 첫 줄만 담았더니 발췌 12줄이 전부 `cd /Users/…` 였다.
// 에이전트는 거의 모든 Bash 를 `cd` 나 `export` 로 시작하므로, 첫 줄을 담는 것은
// **아무것도 안 담는 것**과 같다. 실제로 한 일은 그다음에 있다.
var prelude = regexp.MustCompile(`^\s*(cd|export|set|source|\.|unset|umask|alias)\b`)

// meaningfulCommand 는 준비 동작을 건너뛰고 실제로 한 일을 준다.
//
// 줄바꿈과 `&&`·`;` 로 쪼갠다. 전부 준비 동작이면 첫 조각을 준다 — 그래도
// 아무것도 안 담는 것보다는 낫다.
// Command 는 준비 동작을 건너뛰고 실제로 한 일을 준다.
func Command(s string) string {
	parts := splitCommand(s)
	for _, c := range parts {
		c = strings.TrimSpace(c)
		if c == "" || prelude.MatchString(c) {
			continue
		}
		return clip(c)
	}
	if len(parts) > 0 {
		return clip(strings.TrimSpace(parts[0]))
	}
	return clip(s)
}

// splitCommand 는 셸 명령을 조각으로 나눈다. 정확한 파싱이 아니라 어림이다 —
// 따옴표 안의 구분자도 나뉘지만, 우리는 "무엇을 했나" 의 첫 조각만 필요하다.
func splitCommand(s string) []string {
	f := func(r rune) bool { return r == '\n' || r == '\r' || r == ';' }
	var out []string
	for _, line := range strings.FieldsFunc(s, f) {
		out = append(out, strings.Split(line, "&&")...)
	}
	return out
}

// FirstLine 은 첫 줄만 상한까지 잘라 준다.
func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	return clip(s)
}

// clip 은 한 줄을 상한까지 자른다.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > maxToolLine {
		return string(r[:maxToolLine]) + "…"
	}
	return s
}

// secretRe 는 명령줄에 섞인 자격증명 모양이다.
//
// **발화보다 명령이 위험하다.** 사람이 산문으로 토큰을 적는 일은 드물지만, 셸 명령에는
// `export TOKEN=…` `-H "Authorization: …"` 같은 것이 자연스럽게 들어간다. 이 줄은
// 상태 파일(state.json)에 남고 판별기에게도 넘어가므로, 우리가 새로 만드는 노출이다.
//
// 완전한 방어는 아니다 — 완전한 자격증명 탐지는 불가능하다. 흔한 모양을 지우고,
// 남는 위험은 문서에 적는다.
var secretRe = regexp.MustCompile(
	`(?i)(authorization:\s*\S+|bearer\s+\S+|` +
		`(api[_-]?key|secret|token|passwd|password|credential)\s*[=:]\s*\S+|` +
		`(gh[pousr]_|sk-|xox[baprs]-|AKIA)[A-Za-z0-9_\-]{8,})`)

// Redact 는 흔한 자격증명 모양을 지운다.
func Redact(s string) string { return secretRe.ReplaceAllString(s, "…(가림)") }

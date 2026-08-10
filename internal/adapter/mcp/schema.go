package mcp

import "github.com/xian0310567/priorcase/internal/core/i18n"

// 도구 입력 스키마를 **손으로 짓는다.**
//
// `jsonschema:"..."` 구조체 태그는 컴파일 타임 상수라 언어를 따라갈 수 없다. 그런데
// 이 설명들은 **모델이 인자를 만들 때 읽는 글**이라, 대화 언어와 어긋나면 도구 선택과
// 인자 구성이 같이 나빠진다. 실측으로 확인됐다 — initialize 응답이 통째로 한국어라
// 영어 사용자의 에이전트가 한국어 지시문을 받는다.
//
// SDK 는 `Tool.InputSchema` 가 이미 채워져 있으면 리플렉션을 건너뛴다
// (go-sdk server.go 의 setSchema: `if *sfield == nil`). map 으로 주면 내부에서
// 다시 마셜링해 스키마로 만든다 — 그래서 jsonschema 패키지를 직접 의존하지 않아도 된다.
//
// **구조체와 어긋나면 도구 호출이 통째로 실패한다.** 필드 이름·타입·필수 여부가
// json 태그와 정확히 같아야 한다. schema_test.go 가 실제 핸드셰이크로 그걸 검사한다.

// prop 은 스키마 속성 하나다.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// arrayProp 은 문자열 배열 속성이다.
func arrayProp(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": desc,
	}
}

// object 는 도구 입력 스키마 한 벌이다. required 가 비면 키를 아예 넣지 않는다 —
// 빈 배열을 넣으면 일부 클라이언트가 "필수 인자가 있다" 로 읽는다.
func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func recallSchema(l i18n.Lang) map[string]any {
	return object(map[string]any{
		"query": prop("string", l.T(
			"찾을 주제나 키워드",
			"Topic or keywords to search for")),
		"limit": prop("integer", l.T(
			"최대 결과 수 (기본 3)",
			"Maximum number of results (default 3)")),
		"cross_project": prop("boolean", l.T(
			"현재 프로젝트 밖의 결정도 찾는다 (기본 true)",
			"Also search decisions outside the current project (default true)")),
	}, "query")
}

func captureSchema(l i18n.Lang) map[string]any {
	return object(map[string]any{
		"domain": prop("string", l.T(
			"결정이 속한 프로젝트 도메인 접두어",
			"Domain prefix of the project this decision belongs to")),
		"slug": prop("string", l.T(
			"파일명에 들어갈 짧은 주제어",
			"Short topic slug used in the filename")),
		"summary": prop("string", l.T(
			"한 줄 요약 — 회수 때 이것만 주입되므로 그 자체로 읽혀야 한다",
			"One-line summary. Recall injects only this line, so it must stand on its own")),
		"body": prop("string", l.T(
			"본문 마크다운 (## 결정 / ## 근거 / ## 고려한 대안 / ## 예상 리스크 / ## 회고)",
			"Body markdown (## Decision / ## Rationale / ## Alternatives considered / ## Risks / ## Retrospective)")),
		"tags": arrayProp(l.T(
			"프로젝트를 넘어 쓰일 교훈이면 lesson 을 넣는다",
			"Add `lesson` if this is a lesson that applies beyond this project")),
		"related": arrayProp(l.T(
			"관련 문서의 위키링크 또는 stem",
			"Wiki links or stems of related documents")),
		"supersedes": prop("string", l.T(
			"이 결정이 뒤집는 기존 결정의 stem",
			"Stem of the existing decision this one overturns")),
		"date": prop("string", l.T(
			"YYYY-MM-DD (기본: 오늘)",
			"YYYY-MM-DD (default: today)")),
		"session_id": prop("string", l.T(
			"이 결정이 나온 대화의 세션 id. 세션 진입 컨텍스트에 적혀 있으면 그대로 넘긴다",
			"Session id of the conversation this decision came from. Pass it through if the session-start context states one")),
	}, "domain", "slug", "summary")
}

func reviewSchema(l i18n.Lang) map[string]any {
	return object(map[string]any{
		"stem": prop("string", l.T(
			"갱신할 결정의 파일명 (확장자 제외)",
			"Filename of the decision to update (without extension)")),
		"outcome": prop("string", l.T(
			"pending | good | bad",
			"pending | good | bad")),
		"status": prop("string", l.T(
			"active | superseded | regretted",
			"active | superseded | regretted")),
		"retrospective": prop("string", l.T(
			"## 회고 에 붙일 내용",
			"Text to append under ## Retrospective")),
		"supersedes": prop("string", l.T(
			"이 결정이 뒤집는 결정의 stem",
			"Stem of the decision this one overturns")),
	}, "stem")
}

func pendingSchema(l i18n.Lang) map[string]any {
	return object(map[string]any{
		"resolve": prop("string", l.T(
			"지울 구간의 id. 비우면 목록만 본다",
			"Id of the segment to clear. Leave empty to only list them")),
	})
}

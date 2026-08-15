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
		// **"또는 stem" 을 지웠다.** 두 형식을 다 권한 탓에 실볼트에 `[[ ]]` 없는
		// 값이 남았고, 옵시디언은 그것을 링크로 안 읽어 백링크가 안 걸렸다.
		// (capture 가 이제 맨 stem 도 감싸 주지만, 권하는 형식은 하나여야 한다.)
		//
		// 무엇을 넣을지도 적는다 — 다른 프로젝트의 결정을 근거로 삼는 것이
		// 이 필드의 존재 이유인데, 그 말이 없으면 아무도 안 쓴다.
		"related": arrayProp(l.T(
			"실제로 근거로 삼은 문서의 위키링크 [[stem]]. **다른 프로젝트의 결정도 넣는다** — 그게 프로젝트를 잇는 유일한 수단이다",
			"Wiki links [[stem]] of documents you actually relied on. **Include decisions from other projects** — that is the only thing connecting them")),
		// **배열이다.** 한 결정이 여럿을 뒤집는 일이 실제로 있었는데(2026-08-13
		// 방향전환이 전제 6개를 폐기), 필드가 한 칸뿐이라 나머지가 본문 산문으로
		// 밀려나고 두 노트가 "뒤집혔는데 뒤집은 쪽이 없는" 상태로 남았다.
		"supersedes": arrayProp(l.T(
			"이 결정이 뒤집는 기존 결정들의 stem. **여러 전제를 한꺼번에 걷어냈으면 전부 적는다** — 빠뜨린 것은 낡은 채로 계속 회수된다",
			"Stems of existing decisions this one overturns. **List every one** — anything omitted keeps being recalled as if still current")),
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
		// retracted 는 **애초에 결정이 아니었다** 는 뜻이다 — 판별기 오기록이나
		// 사람의 착오. regretted("했는데 나빴다")와 다르다: 후회는 같은 실수를
		// 되풀이하지 않으려고 계속 떠야 하지만, 철회는 근거가 아니므로 회수에서
		// 통째로 빠진다. 파일은 남는다.
		"status": prop("string", l.T(
			"active | superseded | regretted | retracted — retracted 는 '애초에 결정이 아니었다' 로 회수에서 빠진다(retrospective 필수). regretted 는 '했는데 나빴다' 라 계속 회수된다",
			"active | superseded | regretted | retracted — retracted means 'this was never a decision'; it drops out of recall entirely (retrospective required). regretted means 'we did it and it went badly' and keeps surfacing")),
		"retrospective": prop("string", l.T(
			"## 회고 에 붙일 내용",
			"Text to append under ## Retrospective")),
		"supersedes": arrayProp(l.T(
			"이 결정이 뒤집는 결정들의 stem (여럿 가능)",
			"Stems of the decisions this one overturns (multiple allowed)")),
	}, "stem")
}

func pendingSchema(l i18n.Lang) map[string]any {
	return object(map[string]any{
		"resolve": prop("string", l.T(
			"지울 구간의 id. 비우면 목록만 본다",
			"Id of the segment to clear. Leave empty to only list them")),
	})
}

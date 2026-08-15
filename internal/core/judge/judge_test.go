package judge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAcceptsPlainJSON(t *testing.T) {
	v, err := parse(`{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nx"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Record || v.Slug != "저장엔진" {
		t.Errorf("%+v", v)
	}
}

// 모델이 앞뒤에 말을 붙여도 견뎌야 한다 — 지시해도 종종 붙인다.
func TestParseSurvivesChatter(t *testing.T) {
	v, err := parse("네, 판정하겠습니다.\n\n```json\n{\"record\":false,\"reason\":\"진행 보고다\"}\n```\n도움이 되었길.")
	if err != nil {
		t.Fatal(err)
	}
	if v.Record {
		t.Error("record 가 true 로 읽혔다")
	}
	if v.Reason == "" {
		t.Error("이유를 못 읽었다")
	}
}

// ★ **기록하라면서 무엇을 기록할지 안 주면 안 만든다.**
// 조용히 빈 노트를 만드는 것보다 안 만드는 것이 낫다.
func TestParseRejectsIncompleteRecord(t *testing.T) {
	for _, s := range []string{
		`{"record":true,"summary":"요약만 있다"}`,
		`{"record":true,"slug":"슬러그만-있다"}`,
	} {
		v, err := parse(s)
		if err != nil {
			t.Fatal(err)
		}
		if v.Record {
			t.Errorf("불완전한데 기록하려 한다: %s", s)
		}
		if v.Reason == "" {
			t.Errorf("왜 안 만드는지 안 알려 준다: %s", s)
		}
	}
}

func TestParseFailsLoudlyOnGarbage(t *testing.T) {
	if _, err := parse("판정할 수 없습니다."); err == nil {
		t.Error("JSON 이 아닌데 조용히 넘어갔다")
	}
}

// 판별기가 없는 것은 **고장이 아니라 설정이다.** 에러로 만들면 판별기 없는
// 사용자에게 매번 경고가 뜬다.
func TestFindReturnsNilWhenAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if j := Find("", ""); j != nil {
		t.Errorf("없는데 찾았다고 한다: %+v", j)
	}
}

func TestFindUsesExplicitPath(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := Find(bin, "")
	if j == nil || j.Path != bin {
		t.Fatalf("명시 경로를 안 쓴다: %+v", j)
	}
	if j.Model != DefaultModel {
		t.Errorf("Model = %q", j.Model)
	}
}

// ★ **명시 경로가 잘못돼도 조용히 꺼진다.**
//
// 검증하지 않으면 오타 하나로 매 세션 끝에 "판별기 실행 실패" 가 뜬다. 훅은 대화
// 흐름에 있어서 반복 경고를 낼 자리가 아니다 — "설정했는데 안 된다" 는 prior doctor 가 알린다.
func TestFindRejectsBrokenExplicitPath(t *testing.T) {
	if j := Find(filepath.Join(t.TempDir(), "없는것"), ""); j != nil {
		t.Errorf("없는 경로를 받아들였다: %+v", j)
	}
	// 실행 권한이 없는 파일도 마찬가지다.
	noexec := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(noexec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if j := Find(noexec, ""); j != nil {
		t.Errorf("실행 권한 없는 파일을 받아들였다: %+v", j)
	}
}

// 설정했는지와 쓸 수 있는지를 나눠 알려 준다 — prior doctor 가 둘을 구별해 말해야 한다.
func TestConfiguredDistinguishesSetFromUsable(t *testing.T) {
	if set, _ := Configured(""); set {
		t.Error("안 적었는데 적었다고 한다")
	}
	broken := filepath.Join(t.TempDir(), "없는것")
	set, ok := Configured(broken)
	if !set || ok {
		t.Errorf("Configured(%q) = (%v, %v), (true, false) 여야 한다", broken, set, ok)
	}
}

// 지시문이 보수적으로 판정하게 만드는지 — 이게 이 패키지의 안전장치 전부다.
func TestPromptDemandsConservatism(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "뭔가", Existing: []ExistingDecision{{Stem: "alpha-결정-옛것-2026-08-01", Summary: "이미 있는 결정"}}})
	for _, want := range []string{"애매하면 record=false", "사람이 쓴 결정 노트와 섞이므로", "이미 기록된 결정", "alpha"} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다", want)
		}
	}
}

// ★ **회수 구조를 지시문이 알려 줘야 한다.**
//
// 회수는 파일명·summary·tags 만 본다 (search.scoreAll 의 headHits==0 → continue).
// 본문에만 있는 낱말로는 영원히 못 찾는다. 실측: tags 를 회수 어휘로 채우니
// 같은 노트가 0/1 → 3/3 으로 걸렸다.
//
// 지시문이 이걸 말해 주지 않으면 판별기는 tags 를 주제 분류로 쓴다.
func TestPromptTeachesRetrievalStructure(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "x"})
	for _, want := range []string{
		"파일명·summary·tags 뿐", // 무엇이 검색되는지
		"영원히 찾을 수 없다",        // 안 그러면 무슨 일이 나는지
		"회수 키워드",             // tags 의 정체
		"동의어와 상위어",           // 어떻게 채우는지
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다 — 판별기가 tags 를 주제 분류로 쓴다", want)
		}
	}
}

// ★ claude CLI 는 실패를 **stdout 에** 쓴다 ("Not logged in · Please run /login").
// stderr 만 보면 `exit status 1` 뒤가 비어서 사용자가 무엇을 해야 할지 알 수 없다.
// 새 사용자 시뮬레이션에서 실제로 그 상태를 만났다.
func TestFailureMessageIncludesStdout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "judge")
	script := "#!/bin/sh\ncat >/dev/null\necho 'Not logged in · Please run /login'\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &CLI{Path: bin, Model: "m", Timeout: 10 * time.Second}
	_, err := c.Decide(context.Background(), Request{Excerpt: "x", Domain: "alpha"})
	if err == nil {
		t.Fatal("실패했어야 한다")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("stdout 의 실패 이유가 안 보인다 — 사용자가 뭘 해야 할지 모른다: %v", err)
	}
}

// 아무 출력도 없이 실패하면 그것도 말해 준다 — 빈 메시지는 진단이 아니다.
func TestFailureMessageNeverEmpty(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "judge")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &CLI{Path: bin, Model: "m", Timeout: 10 * time.Second}
	_, err := c.Decide(context.Background(), Request{Excerpt: "x"})
	if err == nil || !strings.Contains(err.Error(), "출력 없음") {
		t.Errorf("빈 실패를 설명하지 않는다: %v", err)
	}
}

// ★ **출력은 발췌의 언어로.** 지시문이 한국어라고 영어 대화에 한국어 노트를 만들면
// 파일명이 두 언어로 갈리고(app-decision-sqlite-단일서버-선택), 영어 사용자가
// 영어로 물었을 때 summary 가 안 걸린다. 실측으로 그 상태를 확인했다.
func TestPromptDemandsMatchingLanguage(t *testing.T) {
	p := prompt(Request{Domain: "app", Excerpt: "We decided on SQLite."})
	for _, want := range []string{
		"발췌와 같은 언어",   // 무엇을
		"섞어 쓰지 마라",    // 왜 중요한지
		"## Decision", // 영어 예시까지 준다
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다", want)
		}
	}
}

// Check 는 "있기만 한 것" 과 "쓸 수 있는 것" 을 가른다. 로그인이 안 됐으면 세션이
// 끝나는 순간에야 알게 되는데, 그때는 이미 그 구간을 놓친 것이다.
func TestCheckDistinguishesUsableFromPresent(t *testing.T) {
	ok := filepath.Join(t.TempDir(), "judge")
	if err := os.WriteFile(ok, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"ok\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&CLI{Path: ok, Model: "m", Timeout: 10 * time.Second}).Check(context.Background()); err != nil {
		t.Errorf("답하는 판별기를 실패로 봤다: %v", err)
	}

	bad := filepath.Join(t.TempDir(), "judge")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\ncat >/dev/null\necho 'Not logged in'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := (&CLI{Path: bad, Model: "m", Timeout: 10 * time.Second}).Check(context.Background())
	if err == nil {
		t.Fatal("답하지 않는 판별기를 통과시켰다")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("이유가 안 보인다: %v", err)
	}
}

// 판별기 지시문에서 **도구 활동 설명이 사라지면** 판별기가 "· Edit foo.go" 를
// 에이전트가 한 말로 읽는다. 지시문은 긴 raw 문자열이라 편집 중 통째로 날아가기 쉽다.
func TestPromptExplainsToolActivity(t *testing.T) {
	got := prompt(Request{Excerpt: "x", Domain: "alpha", Date: "2026-08-09"})
	for _, want := range []string{"한 일", "가운뎃점", "일상적 작업이면 결정이 아니다"} {
		if !strings.Contains(got, want) {
			t.Errorf("지시문에 %q 가 없다 — 판별기가 도구 활동을 말로 읽는다", want)
		}
	}
	// 회수 구조 설명도 같이 지킨다. 이게 빠지면 tags 가 검색어가 아니라 분류가 된다.
	for _, want := range []string{"검색어", "record=false"} {
		if !strings.Contains(got, want) {
			t.Errorf("지시문에 %q 가 없다", want)
		}
	}
}

// 지시문에서 **날조 금지가 사라지면** 판별기가 근거 절을 그럴듯하게 채운다.
//
// 실제로 그랬다. 2026-08-10 에 ③ 이 만든 첫 노트가 "canonlog 는 판례집의 라틴어
// 표현" 이라고 적었는데, 대화 어디에도 없는 말이었다. 지시문이 record 여부와 회수
// 구조는 길게 다루면서 "발췌에 있는 것만 써라" 를 빠뜨린 탓이다.
//
// **이게 이 제품에서 가장 나쁜 실패다.** 노트가 없으면 사람이 찾아보지만, 틀린
// 노트는 아무도 의심하지 않는다 — 권위 있는 형식으로 남기 때문이다.
func TestPromptForbidsFabrication(t *testing.T) {
	got := prompt(Request{Excerpt: "x", Domain: "alpha", Date: "2026-08-09"})
	for _, want := range []string{
		"지어내지 마라",        // 총칙
		"근거 절이 위험하다",     // 어느 절이 특히 위험한지
		"근거가 대화에 남지 않았다", // 비어 있을 때 쓸 말을 준다
	} {
		if !strings.Contains(got, want) {
			t.Errorf("지시문에 %q 가 없다 — 판별기가 근거를 지어낸다", want)
		}
	}
	// 출력 형식의 body 설명 자체에도 제약이 붙어 있어야 한다. 위 산문만 남고
	// 여기가 "절 셋" 으로 되돌아가면, 형식 예시가 산문을 이긴다.
	if !strings.Contains(got, `"body": "발췌의 언어로. 절 셋: 결정 / 근거 / 고려한 대안. 발췌에 있는 것만"`) {
		t.Error("출력 형식의 body 설명에 발췌 제약이 없다")
	}
}

// ★★ **답이 나왔으면 프로세스가 어떻게 끝났든 쓴다.**
//
// claude CLI 는 답을 찍은 뒤에도 정리(텔레메트리·MCP 종료)에 시간을 쓴다. 그 사이에
// 상한이 걸리면 프로세스는 killed 인데 **stdout 에는 완전한 JSON 이 이미 있다.**
// 그걸 실패로 버리면 판별기를 부른 값을 통째로 날리고, 그 구간은 다음에도 같은 일을
// 반복한다.
//
// 실측으로 확인했다: 원장의 killed 기록 하나가 완전한 verdict 를 담고 있었고
// (record·slug·summary·body), 그 발췌는 그 뒤로도 계속 큐에 남아 있었다.
func TestDecideUsesOutputEvenWhenProcessFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "judge")
	// 답을 찍고 0 아닌 코드로 죽는다 — killed 를 흉내낸다.
	//
	// **echo 를 쓰지 않는다.** sh 의 echo 는 \n 을 해석해서 JSON 문자열 안에 진짜
	// 줄바꿈을 넣어 버린다 — 그러면 파싱이 깨지고, 테스트가 구현을 탓하게 된다.
	// 실제로 그렇게 한 번 헛짚었다.
	script := "#!/bin/sh\ncat >/dev/null\ncat <<'J'\n" +
		`{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정"}` +
		"\nJ\nexit 137\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &CLI{Path: bin, Model: "m", Timeout: 10 * time.Second}
	v, err := c.Decide(context.Background(), Request{Excerpt: "x", Domain: "alpha"})
	if err != nil {
		t.Fatalf("답이 있는데 실패로 버렸다: %v", err)
	}
	if !v.Record || v.Slug != "저장엔진" {
		t.Errorf("verdict 가 안 읽혔다: %+v", v)
	}
}

// ★ **그렇다고 아무 출력이나 통과시키면 안 된다.** 잘린 JSON·로그·빈 출력은
// 여전히 실패다 — 안 그러면 실패가 조용히 "기록 안 함" 으로 둔갑한다.
func TestDecideStillFailsWhenOutputIsNotAVerdict(t *testing.T) {
	for _, c := range []struct{ name, out string }{
		{"빈 출력", ""},
		{"잘린 JSON", `{"record":true,"slug":"저장엔`},
		{"로그만", "Not logged in · Please run /login"},
		{"불완전한 record", `{"record":true}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "judge")
			script := "#!/bin/sh\ncat >/dev/null\ncat <<'J'\n" + c.out + "\nJ\nexit 137\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			j := &CLI{Path: bin, Model: "m", Timeout: 10 * time.Second}
			_, err := j.Decide(context.Background(), Request{Excerpt: "x", Domain: "alpha"})
			if err == nil {
				t.Error("실패했어야 한다 — 답이 아닌 출력을 verdict 로 받으면 " +
					"판별기 고장이 '기록 안 함' 으로 둔갑한다")
			}
		})
	}
}

// ★ 실제로 발생했다. `~/.local/state/priorcase/promotions.jsonl` 2026-08-14T17:14:59Z,
// domain=nova — record:true 에 slug·summary·body 까지 다 찬 verdict 가 통째로 버려졌다.
//
// 발췌 한 구간에 기록할 결정이 둘 이상이면 판별기가 단일 객체 대신 배열을 낸다.
// parse 는 첫 `{` 부터 마지막 `}` 까지를 잘라내므로 잘린 문자열이 `{...},\n{...}`
// 가 되고 json.Unmarshal 이 "invalid character ',' after top-level value" 로 죽는다.
//
// 배열이면 **첫 원소를 쓴다.** 나머지 결정은 잃지만, 그 구간은 pending 에 남아
// 다음에 다시 판정된다 — 통째로 버리는 것보다 낫다.
func TestParseAcceptsJSONArray(t *testing.T) {
	raw := "```json\n[\n  {\"record\": true, \"slug\": \"첫째\", \"summary\": \"첫 결정\"},\n" +
		"  {\"record\": true, \"slug\": \"둘째\", \"summary\": \"둘째 결정\"}\n]\n```"
	v, err := parse(raw)
	if err != nil {
		t.Fatalf("배열 응답을 못 읽는다: %v", err)
	}
	if !v.Record || v.Slug != "첫째" {
		t.Errorf("첫 원소를 안 썼다: %+v", v)
	}
}

// 배열 대응을 넣어도 단일 객체는 그대로 읽혀야 한다.
func TestParseStillReadsSingleObject(t *testing.T) {
	v, err := parse("어쩌고 {\"record\": true, \"slug\": \"하나\", \"summary\": \"요약\"} 저쩌고")
	if err != nil {
		t.Fatalf("단일 객체를 못 읽는다: %v", err)
	}
	if v.Slug != "하나" {
		t.Errorf("slug = %q, want 하나", v.Slug)
	}
}

// ★ 지시문이 **반복과 뒤집기를 가르게** 만드는지.
//
// 이게 없으면 자동 경로는 뒤집기를 중복으로 판정해 조용히 지운다 — 기존 결정을
// 뒤집는 대화는 그 결정과 주제·어휘가 거의 같아서 중복으로 판정되기 가장 좋다.
// 그 결과 기록이 각 주제의 첫 결정 쪽으로 계통적으로 편향된다.
func TestPromptOpensSupersedeChannel(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "뭔가", Existing: []ExistingDecision{
		{Stem: "alpha-결정-옛것-2026-08-01", Summary: "옛 결론"},
	}})
	for _, want := range []string{
		"뒤집는 것은 새 결정이다",   // 반복과 뒤집기를 가르라는 지시
		"supersedes",         // 어디에 적는지
		"지어내지 마라",          // 목록 밖 stem 금지
		"alpha-결정-옛것-2026-08-01", // ★ stem 이 실제로 보여야 지목할 수 있다
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다", want)
		}
	}
}

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

// ★ **불완전한 판정에서 무엇을 버리고 무엇을 살릴지가 갈렸다.**
//
// 옛 판은 slug 든 summary 든 하나만 없어도 통째로 버렸다 — "조용히 빈 노트를
// 만드는 것보다 안 만드는 것이 낫다". 그 원칙 자체는 여전히 옳지만, **빈 노트가
// 되는 경우와 파일명만 못 짓는 경우를 구별하지 않은 것**이 틀렸다.
//
// 실측으로 물렸다: 이 저장소의 실제 세션(발화 359개)을 도중 판정에 넣으니
// 판별기가 slug 없는 decision 을 줬고, 본문에 진단·근거·미결이 다 들어 있는데도
// 옛 판이 그걸 none 으로 접었다. 등급이 둘이 된 지금은 담을 자리가 있다.
//
//   - summary 가 없다 → 정말로 쓸 것이 없다. 버린다.
//   - slug 만 없다 → 파일명을 못 지을 뿐이다. 작업 로그로 내린다.
func TestParseSalvagesWhatItCan(t *testing.T) {
	// slug 가 없으면 결정 노트는 못 만들지만 내용은 산다.
	v, err := parse(`{"record":true,"summary":"요약만 있다","body":"근거가 여기 있다"}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Tier != TierWorklog {
		t.Errorf("tier = %q, want worklog — 내용이 있는데 버렸다", v.Tier)
	}
	if v.Body != "근거가 여기 있다" {
		t.Errorf("본문이 사라졌다: %q", v.Body)
	}
	if v.Reason == "" {
		t.Error("왜 등급을 내렸는지 안 알려 준다")
	}

	// summary 가 없으면 남길 것이 없다. 이건 여전히 버린다.
	v, err = parse(`{"record":true,"slug":"슬러그만-있다"}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Recorded() {
		t.Error("summary 도 없는데 기록하려 한다 — 빈 노트가 생긴다")
	}
	if v.Reason == "" {
		t.Error("왜 안 만드는지 안 알려 준다")
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

// ★ **애매한 것은 버리지 말고 작업 로그로 보낸다.**
//
// 옛 지시문은 "애매하면 record=false" 였고, 그것이 이 패키지의 안전장치 전부라고
// 스스로 적어 뒀다. 그리고 그 목적을 정확히 달성했다 — 실측 7일 판정 23건에
// **자동 기록 0건.** 기각 사유 23건이 지시문 문장을 직역해서 되돌아왔다.
//
// 뒤집은 근거: 판별기가 11건을 "아직 최종 결정이 아니다" 로 버린 세션에서,
// 볼트에는 같은 source_session 의 결정 노트 **8건이 사람 손으로** 기록돼 있었다.
// 판단이 틀린 게 아니라 선택지가 둘뿐이었던 것이 틀렸다.
//
// 이 테스트는 옛 문구가 **되돌아오지 않는 것**까지 본다. 지시문은 긴 raw 문자열이라
// 편집 중에 옛 판이 통째로 복원되기 쉽다.
func TestPromptRoutesAmbiguousToWorklog(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "뭔가", Existing: []string{"이미 있는 결정"}})
	for _, want := range []string{
		`애매하면 "worklog" 다`, // 새 기본값
		"버리지 마라",           // 왜 바뀌었는지
		"이미 기록된 결정",        // 중복 자료는 여전히 준다
		"alpha",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다", want)
		}
	}
	// 옛 정책이 되살아나면 자동 기록이 다시 0건이 된다.
	for _, gone := range []string{"애매하면 record=false", "안 기록하는 쪽이 언제나 안전하다"} {
		if strings.Contains(p, gone) {
			t.Errorf("옛 보수 정책 %q 가 되돌아왔다 — 자동 기록이 다시 0건이 된다", gone)
		}
	}
}

// ★ **"아직 안 정했다" 는 버릴 이유가 아니다.**
//
// 원장 23건 중 11건이 이 사유로 기각됐다 — "최종 결정이 대화에 없다",
// "어느 안을 채택할지 아직 미결정", "선택이 내려지지 않은 상태".
// 그중 하나는 대안 A·B·C·D 를 비교한 구간이었는데, 사용자가 볼트에
// "기각한 대안이야말로 재사용 가치가 높다" 고 적어 둔 바로 그 자산이다.
func TestPromptSaysUndecidedIsNotNone(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "x"})
	for _, want := range []string{
		`"아직 안 정했다" 는 none 의 이유가 아니다`,
		"확정 여부는 상관없다",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다 — 판별기가 미결정을 다시 버린다", want)
		}
	}
}

// ★ **도중 판정에는 decision 등급을 아예 보여 주지 않는다.**
//
// 실측: min_turns=6 에 닿는 순간 체크포인트가 전진해 watch.log 전진 26건 중
// 25건이 정확히 "발화 6" 이다. 구간 @3467548 은 마지막 발화 01:18:49 → 판정
// 01:19:06 → **대화가 7초 뒤 01:19:13 에 이어졌다.**
//
// 진행 중인 창을 보면서 "이게 최종인가" 를 물으면 답은 당연히 "아니다" 다.
// 등급이 보이면 모델은 쓴다. 그래서 안 보여 준다.
func TestPromptScopeGatesDecisionTier(t *testing.T) {
	mid := prompt(Request{Domain: "alpha", Excerpt: "x", Scope: ScopeMid})
	if strings.Contains(mid, `"tier": "decision"`) {
		t.Error("도중 판정인데 decision 출력 형식을 보여 준다 — 아크가 안 끝난 창에서 결정이 나온다")
	}
	if !strings.Contains(mid, `"tier": "worklog"`) {
		t.Error("도중 판정에 worklog 출력 형식이 없다")
	}
	if !strings.Contains(mid, "이 대화는 아마 아직 안 끝났다") {
		t.Error("도중이라는 것을 판별기에게 안 알려 준다")
	}
	// **형식을 안 보여 주는 것만으로는 부족했다.** 실측: 실제 세션을 도중 판정에
	// 넣으니 판별기가 decision 을 줬다(그리고 slug 가 없어 옛 판은 버렸다).
	// 등급이 유효하지 않다는 것을 말로도 못 박아야 한다.
	if !strings.Contains(mid, `유효한 tier 는 "worklog" 와 "none" 둘뿐이다`) {
		t.Error("도중 판정에서 decision 이 무효라는 것을 말로 안 한다")
	}

	end := prompt(Request{Domain: "alpha", Excerpt: "x", Scope: ScopeEnd})
	for _, want := range []string{`"tier": "decision"`, `"tier": "worklog"`, "아크 전체"} {
		if !strings.Contains(end, want) {
			t.Errorf("세션 끝 판정에 %q 가 없다", want)
		}
	}

	// Scope 를 안 주면 도중으로 본다 — 옛 호출부는 전부 도중 판정이었다.
	if prompt(Request{Domain: "alpha", Excerpt: "x"}) != mid {
		t.Error("Scope 가 비었을 때 ScopeMid 와 다르게 군다")
	}
}

// ★ **발췌에 새로 생긴 표지 셋을 프롬프트가 설명해야 한다.**
//
// 발췌 생성(daemon.buildExcerpt)과 파서(transcript/claudecode)가 바뀌면서 판별기가
// 보는 글의 모양이 달라졌다. 설명이 없으면 판별기는 `… (193 발화 생략) …` 를
// 대화 내용으로 읽고, 도구가 돌려준 것을 에이전트가 한 말로 읽는다.
//
// **AskUserQuestion 결과가 특히 중요하다.** 원장 11행의 기각 사유가 정확히
// "AskUserQuestion 답변이 발췌에 없어서 실제로 무엇을 정했는지 불명확" 이었다.
// 이제 그 답이 발췌에 들어오는데, 그게 사용자의 실제 선택이라는 것을 알려 주지
// 않으면 같은 기각이 반복된다.
func TestPromptExplainsNewExcerptMarkers(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "x"})
	for _, want := range []string{
		"발화 생략",     // daemon/scan.go 의 "… (%d 발화 생략) …"
		"중략",        // 긴 발화 하나를 줄인 자리
		"없는 인과",     // 왜 이어 읽으면 안 되는지
		"도구가 돌려준 것", // 화살표 뒤의 정체
		"AskUserQuestion",
		"가장 강한 근거",
		"실패 →", // 무엇이 선택을 뒤집었는지의 자리
	} {
		if !strings.Contains(p, want) {
			t.Errorf("지시문에 %q 가 없다 — 판별기가 발췌 표지를 오독한다", want)
		}
	}
}

// ★ 생략된 발췌에서 "근거가 대화에 남지 않았다" 는 **거짓말이 된다.**
//
// 근거가 대화에는 있는데 발췌 밖에 있는 것이라, 그렇게 적으면 프롬프트 자신의
// 경고("틀린 근거는 아무도 의심하지 않는다")에 정면으로 걸린다.
func TestPromptDistinguishesMissingFromOutOfRange(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "x"})
	if !strings.Contains(p, "근거가 이 발췌 범위 밖이다") {
		t.Error("생략된 발췌에서 쓸 말을 안 준다 — 판별기가 '근거가 없다' 고 거짓을 남긴다")
	}
}

// 세션 끝 판정은 그 세션에서 이미 쌓인 작업 로그를 근거로 받는다.
// 발췌 상한에 잘려 나간 앞부분이 거기 남아 있다.
func TestPromptCarriesWorklogIntoEndScope(t *testing.T) {
	p := prompt(Request{Domain: "alpha", Excerpt: "x", Scope: ScopeEnd,
		Worklog: []string{"옵션 A·B·C·D 비교", "B 기각 — 심사 경로"}})
	for _, want := range []string{"이미 작업 로그에 쌓인 것", "옵션 A·B·C·D 비교", "B 기각 — 심사 경로"} {
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
	for _, want := range []string{"검색어", `"tier": "none"`} {
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
	// 형식 예시에서 빠지면, 예시가 산문을 이긴다.
	//
	// **문턱을 낮춘 뒤로 이게 더 중요해졌다.** 예전에는 애매한 것을 버렸으니
	// 지어낼 기회 자체가 적었다. 이제 애매한 것이 전부 worklog 로 들어오므로,
	// 근거가 없는 발췌를 받는 빈도가 올라간다 — 그때 채우게 된다.
	for _, scope := range []Scope{ScopeMid, ScopeEnd} {
		p := prompt(Request{Excerpt: "x", Domain: "alpha", Scope: scope})
		for _, block := range strings.Split(p, `"body": `)[1:] {
			line := strings.SplitN(block, "\n", 2)[0]
			if !strings.Contains(line, "발췌에 있는 것만") {
				t.Errorf("scope=%s 의 출력 형식 body 설명에 발췌 제약이 없다: %s", scope, line)
			}
		}
	}
}

// ★ parse 는 등급을 정본으로 읽고, 옛 record 형식도 접는다.
//
// 모델 출력은 우리가 통제하지 못한다 — 지시문을 바꿔도 record 를 줄 수 있고,
// 옛 원장도 그 키로 쓰여 있다.
func TestParseFoldsTiersAndLegacyRecord(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want Tier
		// slug/summary 가 모자라 none 으로 접히는지 본다
		wantReason string
	}{
		{"결정", `{"tier":"decision","slug":"s","summary":"m"}`, TierDecision, ""},
		{"작업로그", `{"tier":"worklog","summary":"m"}`, TierWorklog, ""},
		{"작업로그는 slug 가 필요 없다", `{"tier":"worklog","summary":"m"}`, TierWorklog, ""},
		{"없음", `{"tier":"none","reason":"조회뿐"}`, TierNone, "조회뿐"},
		{"옛 record=true", `{"record":true,"slug":"s","summary":"m"}`, TierDecision, ""},
		{"옛 record=false", `{"record":false,"reason":"x"}`, TierNone, "x"},
		// ★ **slug 가 없다고 버리지 않는다.** 실측으로 물렸다: 이 저장소의 실제
		// 세션(발화 359개)을 도중 판정에 넣으니 판별기가 slug 없는 decision 을 줬고,
		// 본문에 진단·근거가 다 들어 있는데 옛 판은 그걸 통째로 none 으로 접었다.
		// slug 는 파일명을 짓기 위한 것이지 내용의 값어치와 무관하다.
		{"결정인데 slug 없음 → 작업 로그로 내린다", `{"tier":"decision","summary":"m","body":"근거"}`,
			TierWorklog, "결정 등급인데 slug 가 없어 작업 로그로 내렸다"},
		{"결정인데 summary 없음", `{"tier":"decision","slug":"s"}`, TierNone,
			"판별기가 결정 등급을 줬는데 summary 가 없다"},
		{"작업로그인데 summary 없음", `{"tier":"worklog","body":"b"}`, TierNone,
			"판별기가 작업 로그 등급을 줬는데 summary 가 없다"},
		{"모르는 등급", `{"tier":"뭔가"}`, TierNone, `판별기가 모르는 등급을 줬다: "뭔가"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parse(tc.in)
			if err != nil {
				t.Fatalf("parse(%s) = %v", tc.in, err)
			}
			if v.Tier != tc.want {
				t.Errorf("tier = %q, want %q (reason=%q)", v.Tier, tc.want, v.Reason)
			}
			if tc.wantReason != "" && v.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", v.Reason, tc.wantReason)
			}
			// Record 는 옛 소비자·원장 호환용이라 Tier 와 어긋나면 안 된다.
			if v.Record != v.Recorded() {
				t.Errorf("Record(%v) 와 Recorded()(%v) 가 어긋난다", v.Record, v.Recorded())
			}
		})
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

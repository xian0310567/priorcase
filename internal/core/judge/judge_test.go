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
// 흐름에 있어서 반복 경고를 낼 자리가 아니다 — "설정했는데 안 된다" 는 cb doctor 가 알린다.
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

// 설정했는지와 쓸 수 있는지를 나눠 알려 준다 — cb doctor 가 둘을 구별해 말해야 한다.
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
	p := prompt(Request{Domain: "alpha", Excerpt: "뭔가", Existing: []string{"이미 있는 결정"}})
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

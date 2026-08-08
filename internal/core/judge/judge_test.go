package judge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

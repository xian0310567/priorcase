package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/health"
)

// ★★★ **진단이 거짓말하면 사람은 조사하지 않는다.**
//
// `자동 기록` 검사는 "판별기가 도는가" 를 답한다. 여기가 OK 라고 말하면 사람은
// 세션 끝에 자동 승격이 일어난다고 믿고 넘어간다 — 실제로 안 돌고 있어도.
//
// 이 검사에는 갈래가 다섯인데 절반만 덮여 있었다(44.4%). 실측 커버리지의
// 나머지 낮은 자리들은 cobra 클로저였고, **본체에서 진짜 구멍은 여기 하나였다.**
//
// 갈래마다 사람이 할 일이 다르다:
//   - judge_path 를 적었는데 실행이 안 된다 → 경로를 고친다
//   - 판별기도 없고 시그널도 없다        → 안전망이 통째로 죽었다
//   - 판별기는 없고 시그널은 있다         → 시그널로만 돈다 (언어 의존)
//   - 판별기를 찾았는데 답하지 않는다     → 로그인을 본다
//   - 정상                                → 아무것도 안 한다
//
// 뭉치면 사람이 무엇을 고쳐야 할지 모른다.

func judgeReport(t *testing.T, judgePath string, signals []string) health.Check {
	t.Helper()
	r := wiringReport(t, DoctorOptions{
		Config: &config.Config{
			Capture: config.Capture{JudgePath: judgePath, Signals: signals},
		},
		// 훅 배선·데몬은 이 시험의 관심이 아니다. 없는 자리를 줘서 다른 검사가
		// 조용히 실패해도 이 검사의 판정에는 영향이 없게 한다.
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		StateDir:     t.TempDir(),
	})
	return check(t, r, "자동 기록")
}

// hideJudge 는 판별기를 못 찾게 만든다.
//
// **PATH 만 비우면 안 된다.** judge.Find 는 PATH 보다 `~/.local/bin/claude` 를
// 먼저 보므로(PATH 에 없는 설치가 흔해서 일부러 그렇게 뒀다) HOME 도 같이 옮겨야
// 한다. 처음에 PATH 만 비웠더니 진짜 claude 를 찾아 놓고, 빈 PATH 때문에 그것이
// "Not logged in" 으로 실패해 **엉뚱한 갈래를 시험하고 있었다.**
func hideJudge(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
}

// fakeJudge 는 흉내 판별기를 만든다. exit 0 이면 Check 를 통과한다.
func fakeJudge(t *testing.T, name, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// ★★★ judge_path 를 적었는데 실행이 안 되면 **fail 이다.**
//
// 이 판이 조용하면 사람은 자동 승격이 도는 줄 안다. 실제로는 경로 오타 하나로
// 세션마다 아무것도 기록되지 않는다.
func TestJudgeCheckFailsOnUnusableJudgePath(t *testing.T) {
	// 실행 권한이 없는 파일
	p := fakeJudge(t, "nope.sh", "#!/bin/sh\nexit 0\n", 0o644)
	c := judgeReport(t, p, []string{"결정"})
	if c.Level != health.Fail {
		t.Errorf("등급 %v — fail 이어야 한다 (%s)", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "실행할 수 없다") {
		t.Errorf("무엇이 문제인지 안 말한다: %s", c.Detail)
	}
	if c.Fix == "" {
		t.Error("고칠 방법을 안 준다")
	}
}

// ★★★ 판별기도 없고 시그널도 없으면 **안전망이 통째로 죽는다.** 이건 warn 이
// 아니라 fail 이다 — 아무것도 표시되지 않으므로 사람이 알아챌 다른 통로가 없다.
func TestJudgeCheckFailsWhenNoJudgeAndNoSignals(t *testing.T) {
	hideJudge(t)
	c := judgeReport(t, "", nil)
	if c.Level != health.Fail {
		t.Errorf("등급 %v — fail 이어야 한다 (%s)", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "표시조차") {
		t.Errorf("시그널까지 비었다는 사실을 안 말한다: %s", c.Detail)
	}
}

// ★★ 판별기는 없지만 시그널이 있으면 **warn 이다.** 안전망이 절반은 돈다 —
// fail 로 올리면 "고장" 과 "덜 좋은 상태" 가 같아 보인다.
func TestJudgeCheckWarnsWhenSignalsOnly(t *testing.T) {
	hideJudge(t)
	c := judgeReport(t, "", []string{"결정", "선택"})
	if c.Level != health.Warn {
		t.Errorf("등급 %v — warn 이어야 한다 (%s)", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "signals") {
		t.Errorf("무엇으로 도는지 안 말한다: %s", c.Detail)
	}
}

// ★★★ **판별기를 찾았는데 답하지 않으면 fail 이다.**
//
// 가장 위험한 판이다 — 실행 파일은 있으므로 "찾았다" 로 끝내면 OK 가 된다.
// 실제로는 로그인이 풀렸거나 모델이 없어 매 세션 실패한다.
func TestJudgeCheckFailsWhenJudgeDoesNotAnswer(t *testing.T) {
	p := fakeJudge(t, "dead.sh", "#!/bin/sh\necho '로그인이 필요합니다' >&2\nexit 1\n", 0o755)
	c := judgeReport(t, p, []string{"결정"})
	if c.Level != health.Fail {
		t.Errorf("등급 %v — fail 이어야 한다 (%s)", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "답하지 않는다") {
		t.Errorf("찾았지만 안 답한다는 것을 안 말한다: %s", c.Detail)
	}
	// stderr 를 담아야 한다 — 사람이 고칠 유일한 단서다.
	if !strings.Contains(c.Detail, "로그인이 필요합니다") {
		t.Errorf("판별기의 stderr 를 버렸다: %s", c.Detail)
	}
}

// ★★★ **판별기가 답하면 OK 다.** 그리고 그때만 OK 다.
func TestJudgeCheckOKWhenJudgeAnswers(t *testing.T) {
	p := fakeJudge(t, "ok.sh", "#!/bin/sh\ncat >/dev/null\necho '{\"ok\":true}'\n", 0o755)
	c := judgeReport(t, p, []string{"결정"})
	if c.Level != health.OK {
		t.Errorf("등급 %v — ok 여야 한다 (%s)", c.Level, c.Detail)
	}
	// 판별기가 있으면 시그널은 안 쓰인다 — 그 사실을 말해야 사람이 시그널을
	// 다듬느라 시간을 안 쓴다.
	if !strings.Contains(c.Detail, "signals") {
		t.Errorf("시그널이 안 쓰인다는 것을 안 말한다: %s", c.Detail)
	}
}

// ★★ 설정이 없으면 이 검사는 아무 줄도 안 낸다 — 판정할 근거가 없다.
// 여기서 억지로 OK 를 내면 그것이 거짓말이다.
func TestJudgeCheckSilentWithoutConfig(t *testing.T) {
	r := wiringReport(t, DoctorOptions{
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		StateDir:     t.TempDir(),
	})
	for _, c := range r.Checks {
		if c.Name == "자동 기록" {
			t.Errorf("설정이 없는데 판정했다: %v %s", c.Level, c.Detail)
		}
	}
}

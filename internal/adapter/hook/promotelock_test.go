package hook

import (
	"encoding/json"
	"github.com/xian0310567/casebook/internal/core/judge"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/xian0310567/casebook/internal/daemon"
)

// ★★ `cb watch` 를 켜는 것이 자동 기록을 끄는 행위가 되면 안 된다.
//
// 승격이 소유권 게이트 뒤에 있으면, 데몬이 락을 쥔 순간 훅은 promote 이전에
// 반환한다. 그런데 데몬의 drain 은 판별기를 부르지 않고, 데몬은 세션이 끝난 것도
// 모른다 — 아무도 승격하지 않는 상태가 무기한 이어진다. 사용자에게 "데몬을 띄우면
// 더 촘촘해진다" 고 말하는 순간 정반대가 된다.
func TestPromoteRunsEvenWhenDaemonOwnsLock(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = stubJudge(t,
		`{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nx\n"}`)

	// 먼저 데몬 없이 한 번 훑어 표시를 남긴다.
	seed := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if seed.e != nil {
		t.Fatal(seed.e)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 1 {
		t.Fatalf("표시가 %d건 — 준비가 안 됐다", len(items))
	}

	// 이제 cb watch 가 돌고 있는 상태를 만든다.
	lk := flock.New(filepath.Join(sd, "watch.lock"))
	got, err := lk.TryLock()
	if err != nil || !got {
		t.Fatalf("락을 잡을 수 없다: %v", err)
	}
	defer func() { _ = lk.Unlock() }()

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.err, "자동 기록") {
		t.Errorf("데몬이 락을 쥐었다고 승격을 건너뛰었다 — cb watch 가 자동 기록을 끈다:\n%s", r.err)
	}
	m, _ := filepath.Glob(filepath.Join(c.Vault, "alpha", "decisions", "*저장엔진*"))
	if len(m) == 0 {
		t.Error("노트가 안 만들어졌다")
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 0 {
		t.Errorf("승격했는데 표시가 %d건 남았다", len(items))
	}
}

// 데몬이 락을 쥐고 있으면 훑기는 하지 않는다 — 주인이 하나여야 중복 처리가 없다.
func TestDaemonOwnedLockStillYieldsScanning(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = "" // 판별기 없음 — 승격은 애초에 안 일어난다

	lk := flock.New(filepath.Join(sd, "watch.lock"))
	if got, err := lk.TryLock(); err != nil || !got {
		t.Fatalf("락을 잡을 수 없다: %v", err)
	}
	defer func() { _ = lk.Unlock() }()

	r := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 0 {
		t.Errorf("데몬이 주인인데 훅이 훑어서 표시를 남겼다 (%d건) — 중복 처리다", len(items))
	}
}

// 승격의 세 갈래가 전부 원장에 남아야 한다. stderr 는 그 순간 안 보면 사라지고
// 표시는 곧 해소돼 지워진다 — 원장이 유일하게 남는 증거다.
func TestPromotionIsRecordedInLedger(t *testing.T) {
	for _, tc := range []struct {
		name     string
		judge    string
		recorded bool
		want     func(daemon.Promotion) bool
	}{
		{"기록함", `{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nx\n"}`,
			true, func(p daemon.Promotion) bool { return p.Recorded && p.Path != "" }},
		{"기록안함", `{"record":false,"reason":"기록할 결정이 아니다"}`,
			false, func(p daemon.Promotion) bool { return !p.Recorded && p.Reason != "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd := t.TempDir()
			tp := writeTranscript(t, t.TempDir(), 8)
			c := cfg(t)
			c.Capture.JudgePath = stubJudge(t, tc.judge)

			r := runHook(t, c, sd, EventSessionEnd, Input{
				Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
			if r.e != nil {
				t.Fatal(r.e)
			}
			got, err := daemon.ReadPromotions(sd, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("원장 %d건, want 1 — 판별기가 돌았는지 알 수 없다:\n%s", len(got), r.err)
			}
			if !tc.want(got[0]) {
				t.Errorf("갈래가 구분되지 않는다: %+v", got[0])
			}
			if got[0].At.IsZero() || got[0].Domain == "" || got[0].ID == "" {
				t.Errorf("언제·어디·무엇이 빠졌다: %+v", got[0])
			}
		})
	}
}

// 함수만 테스트하면 호출부를 떼어내도 안 잡힌다 — 실제로 승격 순서가 그렇게 나오는지 본다.
func TestPromoteHandlesOwnSessionFirst(t *testing.T) {
	sd := t.TempDir()
	dir := t.TempDir()
	mine := writeTranscript(t, dir, 8)
	other := filepath.Join(dir, "other.jsonl")
	c := cfg(t)
	c.Capture.JudgePath = stubJudge(t, `{"record":false,"reason":"기록할 결정이 아니다"}`)

	// 남의 구간을 **먼저** 넣는다. 우선순위가 없으면 이게 먼저 처리된다.
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []daemon.Pending{
		{Path: other, From: 0, Domain: "alpha", Days: []string{"2026-08-07"},
			Excerpt: "남의 구간", At: time.Now().UTC().Add(-time.Hour)},
		{Path: mine, From: 0, Domain: "alpha", Days: []string{"2026-08-07"},
			Excerpt: "내 구간", At: time.Now().UTC()},
	} {
		if err := s.AddPending(p); err != nil {
			t.Fatal(err)
		}
	}

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: mine})
	if r.e != nil {
		t.Fatal(r.e)
	}
	proms, err := daemon.ReadPromotions(sd, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(proms) < 1 {
		t.Fatalf("승격 기록이 없다:\n%s", r.err)
	}
	if !strings.HasPrefix(proms[0].ID, mine) {
		t.Errorf("남의 구간을 먼저 처리했다 (%s) — 세션이 끝나는 것은 *이* 대화의 마지막 기회다",
			proms[0].ID)
	}
}

// ★★ 시간 상한 셋의 순서가 어긋나면 자동 기록이 조용히 0건이 된다.
//
//	judge.DefaultTimeout < promoteBudget < promoteHookTimeout
//
// 실제로 어긋나 있었다 — 예산 75초에 판별기 상한 90초. 한 건이 예산을 통째로
// 먹으면 두 번째 구간이 영영 안 돈다. 그리고 훅에 timeout 이 아예 안 적혀 있어서
// 호스트 기본값(우리가 모르는 값)이 승격을 중간에 죽일 수 있었다.
func TestTimeoutOrderingIsSafe(t *testing.T) {
	if judge.DefaultTimeout >= promoteBudget {
		t.Errorf("판별기 상한(%v) ≥ 승격 예산(%v) — 한 건이 예산을 다 먹는다",
			judge.DefaultTimeout, promoteBudget)
	}
	if promoteBudget >= promoteHookTimeout {
		t.Errorf("승격 예산(%v) ≥ 훅 상한(%v) — 예산을 쓰기 전에 호스트가 훅을 죽인다",
			promoteBudget, promoteHookTimeout)
	}
	// 예산 안에 판별기가 최소 두 번은 들어가야 한다. 한 번뿐이면 미확인 구간이
	// 둘 이상일 때 영원히 하나씩만 처리된다.
	if promoteBudget < 2*judge.DefaultTimeout {
		t.Errorf("예산(%v)에 판별기(%v)가 두 번 안 들어간다", promoteBudget, judge.DefaultTimeout)
	}
}

// 승격하는 훅에만 timeout 이 적혀야 한다. 나머지는 밀리초 단위라 적을 이유가 없다.
func TestOnlyPromotingHooksCarryTimeout(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(sp, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(InitOptions{SettingsPath: sp, Binary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ReadSettings(sp)); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"SessionEnd": int(promoteHookTimeout / time.Second),
		"PreCompact": int(promoteHookTimeout / time.Second),
	}
	seen := 0
	for ev, groups := range root.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if !strings.Contains(h.Command, hookMarker) {
					continue
				}
				seen++
				if got := h.Timeout; got != want[ev] {
					t.Errorf("%s: timeout=%d, want %d", ev, got, want[ev])
				}
			}
		}
	}
	if seen != len(Events) {
		t.Errorf("훅 %d개, want %d", seen, len(Events))
	}
}

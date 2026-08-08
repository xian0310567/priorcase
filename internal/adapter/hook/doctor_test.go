package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/casebook/internal/core/health"
	"github.com/xian0310567/casebook/internal/daemon"
)

func wiringReport(t *testing.T, o DoctorOptions) *health.Report {
	t.Helper()
	r := &health.Report{}
	Wiring(r, o)
	return r
}

func check(t *testing.T, r *health.Report, name string) health.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	var got []string
	for _, c := range r.Checks {
		got = append(got, c.Name)
	}
	t.Fatalf("검사 %q 가 없다. 있는 것: %v", name, got)
	return health.Check{}
}

// 배선된 설정을 만든다 (cb init 이 만드는 것과 같은 모양).
func wiredSettings(t *testing.T, binary string) string {
	t.Helper()
	sp := writeSettings(t, realisticSettings)
	p, err := BuildPlan(InitOptions{SettingsPath: sp, Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ReadSettings(sp)); err != nil {
		t.Fatal(err)
	}
	return sp
}

func TestDoctorSeesCompleteWiring(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: t.TempDir(), Binary: self})

	if got := check(t, r, "훅 배선"); got.Level != health.OK {
		t.Errorf("[%s] %s", got.Level.Mark(), got.Detail)
	}
	if got := check(t, r, "훅 바이너리"); got.Level != health.OK {
		t.Errorf("[%s] %s", got.Level.Mark(), got.Detail)
	}
}

// 배선이 빠졌으면 **몇 개가 빠졌는지, 남의 훅은 몇 개인지** 같이 알려야 한다.
func TestDoctorDetectsMissingHooks(t *testing.T) {
	sp := writeSettings(t, realisticSettings) // casebook 훅이 없는 상태
	got := check(t, wiringReport(t, DoctorOptions{SettingsPath: sp, StateDir: t.TempDir()}), "훅 배선")
	if got.Level != health.Fail {
		t.Errorf("Level = %v, Fail 이어야 한다", got.Level)
	}
	if got.Fix != "cb init --apply" {
		t.Errorf("Fix = %q", got.Fix)
	}
}

// ★ 컷오버로 생긴 취약점을 덮는 검사다.
//
// 훅에는 cb 의 **절대 경로가 박혀** 있고 훅은 언제나 exit 0 이다. 그 파일이 사라지면
// 아무 일도 안 하면서 정상으로 보인다.
func TestDoctorDetectsVanishedBinary(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "cb")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sp := wiredSettings(t, fake)
	if err := os.Remove(fake); err != nil { // 사라졌다
		t.Fatal(err)
	}

	got := check(t, wiringReport(t, DoctorOptions{SettingsPath: sp, StateDir: t.TempDir()}), "훅 바이너리")
	if got.Level != health.Fail {
		t.Fatalf("Level = %v, Fail 이어야 한다 — 훅이 조용히 죽는다", got.Level)
	}
	if !strings.Contains(got.Detail, "exit 0") {
		t.Errorf("왜 조용한지 설명하지 않는다: %s", got.Detail)
	}
}

// 훅이 부르는 것과 지금 도는 것이 다르면 고친 게 반영되지 않는다.
func TestDoctorDetectsStaleBinaryPath(t *testing.T) {
	other := filepath.Join(t.TempDir(), "cb")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, other), StateDir: t.TempDir(), Binary: self}), "훅 바이너리")

	if got.Level != health.Warn {
		t.Errorf("Level = %v, Warn 이어야 한다", got.Level)
	}
	if !strings.Contains(got.Detail, "반영되지 않는다") {
		t.Errorf("무슨 문제인지 설명하지 않는다: %s", got.Detail)
	}
}

// 데몬이 안 돌아도 정상이다 — 훅이 대신 훑는다.
func TestDoctorTreatsNoDaemonAsNormal(t *testing.T) {
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: t.TempDir()}), "안전망")
	if got.Level != health.OK {
		t.Errorf("Level = %v, OK 여야 한다: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "훅이 턴 경계") {
		t.Errorf("어느 경로로 도는지 안 알려 준다: %s", got.Detail)
	}
}

// ★ 컷오버 회고의 판정 기준. pending 이 오래 쌓인다는 것은 에이전트가
// cb capture 를 안 부르고 있다는 뜻이다.
func TestDoctorFlagsStalePending(t *testing.T) {
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	if err := s.AddPending(daemon.Pending{
		Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, At: now.Add(-30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: stateDir, Now: now}), "안전망")

	if got.Level != health.Warn {
		t.Errorf("Level = %v, Warn 이어야 한다", got.Level)
	}
	if !strings.Contains(got.Detail, "방치") {
		t.Errorf("방치된 구간을 안 알린다: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "방치") {
		t.Errorf("방치된 구간을 안 알린다: %s", got.Detail)
	}
}

// 상태 파일이 깨졌으면 "미확인 0건" 이 아니라 "확인할 수 없다" 여야 한다.
func TestDoctorDistinguishesBrokenStateFromZero(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{깨짐"), 0o600); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: stateDir}), "안전망")
	if got.Level != health.Fail {
		t.Errorf("Level = %v, Fail 이어야 한다 — 안전망이 죽은 것을 할 일 없는 것으로 읽으면 안 된다", got.Level)
	}
}

// 한글 이름이 섞여도 표가 어긋나지 않아야 한다 (%-Ns 는 바이트를 센다).
func TestDisplayWidthCountsHangulAsTwo(t *testing.T) {
	for s, want := range map[string]int{"볼트": 4, "미선언 도메인": 13, "cb": 2, "훅 배선": 7} {
		if got := displayWidth(s); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

// ★ **PATH 검사가 없으면 진단 자체가 무용지물이다.**
//
// cb doctor 가 내는 모든 → 는 `cb 무엇무엇` 을 치라는 것인데, PATH 에 없으면 사용자는
// 그중 하나도 실행할 수 없다. 훅은 절대 경로로 배선되므로 **시스템은 멀쩡히 도는데
// 사람만 손을 못 대는** 상태가 된다.
//
// 실제로 그랬다 — 개발 내내 절대 경로로 불러서 못 봤고, 사용자가 `cb doctor` 를 쳤을 때
// "command not found" 로 처음 드러났다.
func TestDoctorDetectsCbNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // cb 가 없는 PATH
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: writeSettings(t, realisticSettings), StateDir: t.TempDir()}), "PATH")

	if got.Level != health.Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다", got.Level)
	}
	if !strings.Contains(got.Detail, "사람이 명령을 칠 수 없다") {
		t.Errorf("무엇이 문제인지 설명하지 않는다: %s", got.Detail)
	}
	if !strings.Contains(got.Fix, "ln -s") {
		t.Errorf("고치는 법이 실행 가능한 명령이 아니다: %s", got.Fix)
	}
}

// PATH 에 있으면 정상이다.
func TestDoctorAcceptsCbOnPath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, "cb")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: writeSettings(t, realisticSettings), StateDir: t.TempDir()}), "PATH")
	if got.Level != health.OK {
		t.Errorf("Level = %v, OK 여야 한다: %s", got.Level, got.Detail)
	}
}

// PATH 의 cb 가 지금 도는 것과 다르면 옛 사본을 부르고 있다는 뜻이다.
func TestDoctorDetectsDifferentCbOnPath(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "cb")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: writeSettings(t, realisticSettings), StateDir: t.TempDir()}), "PATH")
	if got.Level != health.Warn {
		t.Errorf("Level = %v, Warn 이어야 한다: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "다르다") {
		t.Errorf("무엇이 다른지 설명하지 않는다: %s", got.Detail)
	}
}

// ★ **회고 판정이 이 한 줄이다.** 표시는 쌓이는데 기록이 0이면 에이전트가
// cb capture 를 안 부르고 있다는 뜻이고, 그러면 이 설계가 실패한 것이다.
func TestDoctorFailsWhenPendingGrowsButNothingRecorded(t *testing.T) {
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	if err := s.AddPending(daemon.Pending{Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, At: now}); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()

	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: stateDir, Now: now,
		RecentDecisions: ptr(0)}), "안전망")

	if got.Level != health.Fail {
		t.Fatalf("Level = %v, Fail 이어야 한다 — 기록 0 + 표시 누적은 설계 실패 신호다", got.Level)
	}
	if !strings.Contains(got.Detail, "기록이 0") {
		t.Errorf("무슨 뜻인지 안 알려 준다: %s", got.Detail)
	}
}

// 기록이 있으면 표시가 있어도 경고에 그친다 — 정상 운영 중이다.
func TestDoctorWarnsOnlyWhenRecordingHappens(t *testing.T) {
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	if err := s.AddPending(daemon.Pending{Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, At: now}); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: stateDir, Now: now,
		RecentDecisions: ptr(5)}), "안전망")
	if got.Level != health.Warn {
		t.Errorf("Level = %v, Warn 이어야 한다: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "기록 5건") {
		t.Errorf("기록 활동을 안 보여 준다: %s", got.Detail)
	}
}

func ptr(n int) *int { return &n }

// ★ 안전망 줄은 **설정이 아니라 실제로 한 일**을 말해야 한다.
//
// 컷오버 1일차에 "미확인 구간 없음 · 이상 없다" 가 나왔는데, 실은 안전망이 구조적으로
// 억제돼 한 번도 판별기를 못 부른 상태였다. 같은 문장이 두 상태를 덮었다.
func TestDoctorReportsActualActivity(t *testing.T) {
	sd := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteScan("/t.jsonl", now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// 판별기가 12번 봤고 그중 3번 기록했다.
	for i := 0; i < 12; i++ {
		p := daemon.Promotion{At: now.Add(-time.Hour), ID: "/t@0", Domain: "alpha"}
		if i < 3 {
			p.Recorded = true
		} else {
			p.Reason = "기록할 결정이 아니다"
		}
		if err := daemon.AppendPromotion(sd, p); err != nil {
			t.Fatal(err)
		}
	}

	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: sd, Now: now}), "안전망")
	for _, want := range []string{"마지막 훑기", "30분 전", "자동 기록 3건", "판정 12건"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("%q 가 없다 — 실제 활동이 안 보인다: %s", want, got.Detail)
		}
	}
}

// 판별기가 돌았지만 기록할 게 없었던 것과 아예 안 돈 것은 다르게 말해야 한다.
func TestDoctorDistinguishesNoPromotionFromNoRecord(t *testing.T) {
	self, _ := os.Executable()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	sd := t.TempDir()
	s := daemon.NewStore(sd)
	_ = s.Load()
	if err := s.NoteScan("/t.jsonl", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	silent := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: sd, Now: now}), "안전망")
	if !strings.Contains(silent.Detail, "자동 기록 없음") {
		t.Errorf("승격이 한 번도 없었다는 사실이 안 보인다: %s", silent.Detail)
	}
}

// 훑은 흔적이 오래됐으면 그것이 안전망이 멎었다는 유일한 양성 증거다.
func TestDoctorWarnsWhenScanningWentSilent(t *testing.T) {
	sd := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	s := daemon.NewStore(sd)
	_ = s.Load()
	if err := s.NoteScan("/t.jsonl", now.AddDate(0, 0, -20)); err != nil {
		t.Fatal(err)
	}

	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: sd, Now: now}), "안전망")
	if got.Level != health.Warn {
		t.Errorf("Level = %v, Warn 이어야 한다: %s", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "훑은 흔적이 없다") {
		t.Errorf("멎었다는 말이 없다: %s", got.Detail)
	}
	if got.Fix == "" {
		t.Error("고치는 법이 없다")
	}
}

// 갓 깐 설치는 경보 대상이 아니다 — 흔적이 없는 것과 멎은 것은 다르다.
func TestDoctorDoesNotAlarmOnFreshInstall(t *testing.T) {
	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: t.TempDir(),
		Now: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)}), "안전망")
	if got.Level != health.OK {
		t.Errorf("갓 깐 설치에 경보가 울렸다 (%v): %s", got.Level, got.Detail)
	}
}

// 억제 횟수가 안 보이면 "볼 게 없어서 조용하다" 와 "N번 눈감았다" 가 여전히 같다.
func TestDoctorShowsSuppressionCount(t *testing.T) {
	sd := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteScan("/t.jsonl", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := s.Credit("/t.jsonl", i, nil); err != nil {
			t.Fatal(err)
		}
	}

	self, _ := os.Executable()
	got := check(t, wiringReport(t, DoctorOptions{
		SettingsPath: wiredSettings(t, self), StateDir: sd, Now: now}), "안전망")
	if !strings.Contains(got.Detail, "면제 3회") {
		t.Errorf("면제 횟수가 안 보인다 — 조용한 이유를 알 수 없다: %s", got.Detail)
	}
}

// 진단을 읽는 사이에 다른 세션의 훅이 돌면 저장된 시각이 몇 밀리초 앞설 수 있다.
// 그때 절대 시각을 뱉으면 방금 일어난 일이 엉뚱한 시각으로 보인다 — 실기기 첫
// 실행에서 실제로 "마지막 훑기 2026-08-08 15:04" 가 나왔고, 그건 25초 전이었다.
func TestHumanAgoHandlesSlightlyFutureTimestamps(t *testing.T) {
	now := time.Date(2026, 8, 9, 0, 5, 16, 0, time.UTC)
	if got := humanAgo(now, now.Add(75*time.Millisecond)); got != "방금" {
		t.Errorf("몇 밀리초 앞선 시각을 %q 로 냈다 — 방금 이어야 한다", got)
	}
	if got := humanAgo(now, now.Add(-30*time.Second)); got != "방금" {
		t.Errorf("30초 전을 %q 로 냈다", got)
	}
	// 정말로 크게 어긋났으면 상대 시간을 말하면 안 된다.
	got := humanAgo(now, now.Add(5*time.Hour))
	if !strings.Contains(got, "시계 어긋남") {
		t.Errorf("5시간 미래를 %q 로 냈다 — 시계가 어긋났다고 말해야 한다", got)
	}
}

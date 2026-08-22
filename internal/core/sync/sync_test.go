package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// git 은 진짜를 쓴다. 이 패키지가 하는 일이 곧 git 을 부르는 것이라, 흉내를 내면
// 검사하는 것이 우리 흉내뿐이 된다.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// pair 는 베어 리모트 하나와 그것을 가리키는 작업 사본 둘을 만든다.
// 두 머신(집·회사)을 흉내 내는 가장 작은 판이다.
func pair(t *testing.T) (a, b string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	git(t, root, "init", "--bare", "-b", "main", bare)

	clone := func(name string) string {
		p := filepath.Join(root, name)
		git(t, root, "clone", bare, p)
		git(t, p, "config", "user.email", "t@example.com")
		git(t, p, "config", "user.name", "t")
		return p
	}
	a = clone("a")
	// 빈 저장소는 브랜치가 없어 pull 이 실패한다. 첫 커밋을 만들어 판을 세운다.
	write(t, a, "seed.md", "seed\n")
	git(t, a, "add", "-A")
	git(t, a, "commit", "-m", "seed")
	git(t, a, "push", "-u", "origin", "main")
	b = clone("b")
	return a, b
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★ 집에서 쓴 결정이 회사에서 보여야 한다. 이 패키지의 존재 이유 전부다.
func TestPushThenPullMovesNotesBetweenMachines(t *testing.T) {
	a, b := pair(t)
	write(t, a, "alpha/decisions/alpha-결정-집에서-2026-08-20.md", "---\ntype: decision\n---\n")

	if r := Push(a, "테스트"); r.Err != nil {
		t.Fatalf("Push: %v", r.Err)
	} else if !r.Pushed || r.Files != 1 {
		t.Fatalf("Push 결과가 이상하다: %+v", r)
	}

	if r := Pull(b); r.Err != nil {
		t.Fatalf("Pull: %v", r.Err)
	}
	if _, err := os.Stat(filepath.Join(b, "alpha/decisions/alpha-결정-집에서-2026-08-20.md")); err != nil {
		t.Errorf("회사 사본에 안 왔다: %v", err)
	}
}

// 커밋할 것이 없으면 빈 커밋을 만들지 않는다 — 원장이 소음으로 찬다.
func TestPushWithNothingToCommitIsClean(t *testing.T) {
	a, _ := pair(t)
	r := Push(a, "테스트")
	if r.Err != nil {
		t.Fatalf("Push: %v", r.Err)
	}
	if r.Files != 0 {
		t.Errorf("커밋할 것이 없는데 %d개를 커밋했다", r.Files)
	}
}

// ★ **리모트가 없는 것은 고장이 아니라 설정이다.**
//
// 볼트를 혼자 한 머신에서만 쓰는 사람이 훨씬 많다. 그때 훅이 매번 에러를 내면
// 그 경고는 무시하는 법을 가르치고, 진짜 실패까지 같이 묻힌다.
func TestNoRemoteIsSkippedNotError(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	for _, r := range []Result{Pull(dir), Push(dir, "x")} {
		if r.Err != nil {
			t.Errorf("리모트 없음을 에러로 냈다: %v", r.Err)
		}
		if r.Skipped == "" {
			t.Errorf("건너뛴 이유를 안 준다: %+v", r)
		}
	}
}

// git 저장소가 아닌 볼트도 정상이다. 이 도구를 안 쓰는 사람의 볼트가 그렇다.
func TestNonRepoIsSkippedNotError(t *testing.T) {
	if r := Push(t.TempDir(), "x"); r.Err != nil || r.Skipped == "" {
		t.Errorf("git 저장소가 아닌 것을 에러로 냈다: %+v", r)
	}
}

// ★ 네트워크가 죽으면 git 이 매달린다. 훅이 세션 경계에서 부르므로 그때 사람이
// 같이 멎는다 — judge 가 상한을 두는 것과 같은 이유로 여기도 상한이 있어야 한다.
//
// PATH 앞에 멈추는 가짜 git 을 놓아 결정적으로 잰다.
func TestSlowGitIsCutOff(t *testing.T) {
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "git"),
		[]byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := timeout
	timeout = 300 * time.Millisecond
	defer func() { timeout = old }()

	start := time.Now()
	r := Push(t.TempDir(), "x")
	el := time.Since(start)
	if el > 3*time.Second {
		t.Fatalf("상한이 안 걸렸다 — %v 를 기다렸다", el)
	}
	// 상한에 걸린 것은 "git 저장소가 아니다" 로 접힌다(precheck 이 먼저 돈다).
	// 중요한 것은 **매달리지 않는 것**이다.
	if r.Err == nil && r.Skipped == "" {
		t.Errorf("상한에 걸렸는데 성공으로 보고했다: %+v", r)
	}
}

// ★ doctor 가 볼 것은 **지금 밀리지 않은 것이 있는가** 다.
//
// "마지막 동기화가 N일 전" 같은 시각 기준보다 낫다 — 며칠 안 썼으면 안 민 것이
// 정상이지만, 쓴 것이 안 밀렸으면 그건 언제 그랬든 손해다. 회사 머신에서 그
// 결정이 안 보인다.
func TestStatusSeesUnpushedWork(t *testing.T) {
	a, _ := pair(t)
	if s := Status(a); !s.HasRemote || s.Ahead != 0 || s.Dirty != 0 {
		t.Fatalf("깨끗한 사본인데: %+v", s)
	}

	write(t, a, "alpha/decisions/alpha-결정-안민것-2026-08-20.md", "x\n")
	if s := Status(a); s.Dirty != 1 {
		t.Errorf("커밋 안 된 파일을 못 본다: %+v", s)
	}

	git(t, a, "add", "-A")
	git(t, a, "commit", "-m", "local")
	s := Status(a)
	if s.Ahead != 1 {
		t.Errorf("밀리지 않은 커밋을 못 본다: %+v", s)
	}
	if s.Dirty != 0 {
		t.Errorf("커밋했는데 dirty 로 센다: %+v", s)
	}
}

// 리모트가 없으면 "동기화를 안 쓰는 볼트" 다. 경고가 아니다.
func TestStatusWithoutRemote(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	if s := Status(dir); s.HasRemote {
		t.Errorf("리모트가 없는데 있다고 한다: %+v", s)
	}
}

// 마지막 시도의 결과를 남긴다 — 조용히 실패한 것을 doctor 가 읽을 유일한 근거다.
func TestStampRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadStamp(dir); ok {
		t.Error("아무것도 안 썼는데 읽혔다")
	}
	want := Stamp{At: time.Now().Truncate(time.Second), OK: false, Detail: "push 실패: 인증"}
	if err := WriteStamp(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadStamp(dir)
	if !ok || got.OK != want.OK || got.Detail != want.Detail || !got.At.Equal(want.At) {
		t.Errorf("왕복이 깨졌다: %+v → %+v", want, got)
	}
}

// ★ 볼트가 여럿일 수 있고, **하나가 실패해도 나머지는 계속해야 한다.**
// 한 리모트의 인증이 만료됐다고 다른 볼트까지 안 밀 이유가 없다.
//
// 순회를 core 가 갖는 이유: CLI 와 훅이 둘 다 이걸 부르는데, 어댑터끼리는
// 서로를 import 할 수 없다(§4.1). 공유할 것이 생기면 core 로 내린다.
func TestAllSweepsEveryVaultAndIsolatesFailure(t *testing.T) {
	good, _ := pair(t)
	bad := t.TempDir() // git 저장소가 아니다 → 건너뜀
	c := &config.Config{Vaults: []config.Vault{
		{Name: "good", Path: good}, {Name: "bad", Path: bad},
	}}
	write(t, good, "alpha/decisions/alpha-결정-여럿-2026-08-20.md", "x\n")

	got := All(c, Options{}, false, true, "테스트")
	if len(got) != 2 {
		t.Fatalf("볼트 %d개, want 2: %+v", len(got), got)
	}
	byName := map[string]VaultResult{}
	for _, v := range got {
		byName[v.Name] = v
	}
	if g := byName["good"]; !g.OK() || g.Files() != 1 {
		t.Errorf("정상 볼트가 안 밀렸다: %+v", g)
	}
	if b := byName["bad"]; b.OK() {
		t.Errorf("git 이 아닌 볼트를 성공으로 봤다: %+v", b)
	}
	if byName["bad"].Failed() {
		t.Errorf("건너뜀을 실패로 봤다 — 리모트 없는 볼트는 고장이 아니다: %+v", byName["bad"])
	}
}

// ★ **세션 진입에 거는 예산은 짧아야 한다.**
//
// SessionStart 훅이 pull 을 부르면 그건 에이전트가 컨텍스트를 받기 전에 사람이
// 기다리는 시간이다. 회사 VPN·캡티브 포털에서 git 이 매달리면 매 세션이 그만큼
// 느려진다 — 20초는 세션당 20초다.
//
// 못 가져오면 그 세션은 어제 것으로 돈다. 그건 손해지만 **매 세션 20초보다 작다.**
func TestAllRespectsBudget(t *testing.T) {
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := &config.Config{Vaults: []config.Vault{{Name: "v", Path: t.TempDir()}}}
	start := time.Now()
	All(c, Options{Timeout: 200 * time.Millisecond}, true, true, "x")
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("예산을 안 지켰다 — %v 를 기다렸다", el)
	}
}

// ★ **판이 갈렸다는 사실을 깨지기 전에 알아야 한다.**
//
// 2026-08-21 사고는 "다른 머신이 더 새 판을 쓴다" 를 아무도 몰랐기 때문에 났다.
// 노트가 안 읽히고 나서야 드러났고, 그때는 이미 사람이 손댈 준비가 된 뒤였다.
//
// **아무도 기억할 필요가 없는 신호를 쓴다.** Go 가 vcs.revision·vcs.time 을 자동으로
// 박으므로, 각 머신이 자기 것을 볼트에 남기면 판 갈림이 저절로 보인다.
// `schema.Current` 에 기대지 않는 이유: 그건 사람이 올려야 하고, 이번 사고가
// 정확히 **안 올려서** 났다.
func TestBuildStampMakesDriftVisible(t *testing.T) {
	v := t.TempDir()
	older := Build{Host: "home", Revision: "aaa", Committed: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)}
	newer := Build{Host: "work", Revision: "bbb", Committed: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)}

	for _, b := range []Build{older, newer} {
		if err := RecordBuild(v, b); err != nil {
			t.Fatal(err)
		}
	}

	// 집(옛 판)에서 보면 회사가 더 새 판이다.
	got := NewerBuilds(v, older)
	if len(got) != 1 || got[0].Host != "work" {
		t.Fatalf("더 새 판을 못 본다: %+v", got)
	}
	// 회사(새 판)에서 보면 앞선 것이 없다.
	if got := NewerBuilds(v, newer); len(got) != 0 {
		t.Errorf("내가 최신인데 남을 더 새 판이라 한다: %+v", got)
	}
}

// **머신마다 자기 파일을 쓴다.** 한 파일을 공유하면 두 머신이 같은 줄을 고쳐
// 동기화할 때마다 충돌한다 — 색인을 추적하지 않기로 한 것과 같은 이유다.
func TestBuildStampIsPerMachine(t *testing.T) {
	v := t.TempDir()
	for _, h := range []string{"home", "work"} {
		if err := RecordBuild(v, Build{Host: h, Revision: h, Committed: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	_ = filepath.Walk(v, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n++
		}
		return nil
	})
	if n != 2 {
		t.Errorf("파일 %d개 — 머신마다 하나여야 한다", n)
	}
}

// ★ **바뀐 게 없으면 쓰지 않는다.**
//
// 매번 쓰면 동기화할 때마다 이 파일 하나 때문에 커밋이 생긴다 — "보낼 것 없음" 이
// 영영 안 나오고, 볼트 원장이 판 도장으로 도배된다.
func TestBuildStampDoesNotRewriteWhenUnchanged(t *testing.T) {
	v := t.TempDir()
	b := Build{Host: "home", Revision: "aaa", Committed: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)}
	if err := RecordBuild(v, b); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(v, "_meta", ".priorcase", "home.json")
	first, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordBuild(v, b); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(p)
	if string(first) != string(again) {
		t.Errorf("같은 판인데 바이트가 바뀐다:\n%s\n%s", first, again)
	}
}

// ★ 도장은 **밀 때 남긴다.** 그래야 같은 커밋에 실려 저쪽이 본다.
//
// pull 만 할 때는 안 남긴다 — 남기면 커밋 안 된 파일이 생겨 doctor 가 "안 밀렸다" 를
// 띄우고, 사람은 아무것도 안 했는데 경고를 본다.
func TestAllRecordsBuildWhenPushing(t *testing.T) {
	a, b := pair(t)
	c := &config.Config{Vaults: []config.Vault{{Name: "v", Path: a}}}
	self := Build{Host: "테스트머신", Revision: "aaa", Committed: time.Now().UTC()}

	All(c, Options{Stamp: self}, false, true, "테스트")

	if _, err := os.Stat(filepath.Join(a, "_meta", ".priorcase", "테스트머신.json")); err != nil {
		t.Fatalf("밀었는데 도장이 없다: %v", err)
	}
	// 같은 커밋에 실려 저쪽으로 건너가야 한다.
	if r := Pull(b); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, err := os.Stat(filepath.Join(b, "_meta", ".priorcase", "테스트머신.json")); err != nil {
		t.Errorf("도장이 저쪽에 안 갔다: %v", err)
	}
}

func TestAllDoesNotRecordOnPullOnly(t *testing.T) {
	a, _ := pair(t)
	c := &config.Config{Vaults: []config.Vault{{Name: "v", Path: a}}}
	All(c, Options{Stamp: Build{Host: "테스트머신", Revision: "aaa", Committed: time.Now()}}, true, false, "x")
	if _, err := os.Stat(filepath.Join(a, "_meta", ".priorcase", "테스트머신.json")); err == nil {
		t.Error("pull 만 했는데 도장을 남겼다 — 커밋 안 된 파일이 생긴다")
	}
}

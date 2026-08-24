package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/health"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// DoctorOptions 는 진단에 필요한 것이다.
type DoctorOptions struct {
	SettingsPath string
	// CodexSettingsPath 는 Codex 훅 파일이다. 비면 ~/.codex/hooks.json.
	//
	// **따로 두는 이유**: 두 호스트를 한 머신에서 같이 쓸 수 있고(집은 Codex, 회사는
	// Claude Code 라도 두 앱이 다 깔려 있다), 그때 한쪽만 보면 다른 쪽이 조용히
	// 죽어도 진단이 초록불을 낸다.
	CodexSettingsPath string
	StateDir          string
	// Binary 는 훅에 배선돼 있어야 할 prior 경로다. 비면 지금 실행 중인 것.
	Binary string
	// Config 는 자동 승격 설정을 보는 데 쓴다. nil 이면 그 검사를 건너뛴다.
	Config *config.Config
	// Now 는 pending 나이를 재는 기준이다. 비면 현재 시각.
	Now time.Time
	// RecentDecisions 는 최근 7일 결정 노트 수다. **nil 이면 모른다는 뜻이다.**
	//
	// 포인터인 이유가 있다. int 로 두면 안 채운 호출자가 0을 주게 되는데, 0은
	// "기록이 하나도 없다" 는 가장 심각한 신호라 **거짓 경보가 울린다.**
	// 제로값이 최악의 판정을 뜻하면 안 된다.
	//
	// 미확인 구간 수와 나란히 놓아야 회고를 판정할 수 있다 — 기록이 0인데 표시만
	// 쌓이면 에이전트가 prior capture 를 안 부르고 있다는 뜻이다.
	RecentDecisions *int
}

// Wiring 은 훅 배선과 데몬 상태를 검사해 core 검사 뒤에 붙인다.
//
// **이 검사들이 컷오버로 생긴 취약점을 덮는다.** 훅에는 prior 의 **절대 경로가 박혀** 있고,
// 훅은 규약상 언제나 exit 0 이다. 그래서 그 파일이 사라지거나 다른 버전으로 바뀌면
// 아무 일도 안 하면서 정상으로 보인다 — 이 프로젝트가 죄목으로 드는 조용한 무동작을
// 우리가 만든 것이다. 여기가 그걸 보는 유일한 자리다.
func Wiring(r *health.Report, o DoctorOptions) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	checkPath(r)
	checkHooks(r, o)
	checkCodexHooks(r, o)
	checkJudge(r, o)
	checkDaemon(r, o)
}

// checkPath 는 사용자가 `prior` 를 그냥 칠 수 있는지 본다.
//
// **이게 없으면 진단 자체가 무용지물이다.** prior doctor 가 내는 모든 → 는 `prior 무엇무엇`
// 을 치라는 것인데, PATH 에 없으면 사용자는 그중 하나도 실행할 수 없다. 훅은 절대
// 경로로 배선되므로 **시스템은 멀쩡히 도는데 사람만 손을 못 대는** 상태가 된다.
//
// 실제로 그랬다 — 개발 내내 절대 경로로 불러서 이 구멍을 못 봤고, 사용자가
// `prior doctor` 를 쳤을 때 "command not found" 로 처음 드러났다.
func checkPath(r *health.Report) {
	exe, err := os.Executable()
	if err != nil {
		return // 자기 경로를 모르면 비교할 것이 없다
	}
	found, lerr := exec.LookPath("prior")
	if lerr != nil {
		add(r, "PATH", health.Warn,
			fmt.Sprintf("`prior` 를 PATH 에서 찾을 수 없다 (지금 것: %s) — "+
				"훅은 절대 경로로 돌지만 사람이 명령을 칠 수 없다", exe),
			"PATH 에 있는 디렉토리로 링크하라 (예: ln -s "+exe+" ~/.local/bin/prior)")
		return
	}
	if !sameFile(found, exe) {
		add(r, "PATH", health.Warn,
			fmt.Sprintf("PATH 의 prior(%s)가 지금 도는 것(%s)과 다르다", found, exe),
			"오래된 사본을 지우거나 링크를 다시 걸어라")
		return
	}
	add(r, "PATH", health.OK, found, "")
}

func checkHooks(r *health.Report, o DoctorOptions) {
	raw, err := os.ReadFile(o.SettingsPath)
	if os.IsNotExist(err) {
		add(r, "훅 배선", health.Fail, o.SettingsPath+" 가 없다", "prior init --apply")
		return
	}
	if err != nil {
		add(r, "훅 배선", health.Fail, "설정을 읽을 수 없다: "+err.Error(), "")
		return
	}
	var root struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		add(r, "훅 배선", health.Fail, "설정이 JSON 이 아니다: "+err.Error(), "")
		return
	}

	wired := map[string]string{} // 이벤트 → 명령
	others := 0
	for ev, groups := range root.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, hookMarker) {
					wired[ev] = h.Command
				} else {
					others++
				}
			}
		}
	}

	var missing []string
	for _, ev := range Events {
		if _, ok := wired[ev.claudeCodeName()]; !ok {
			missing = append(missing, ev.claudeCodeName())
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		add(r, "훅 배선", health.Fail,
			fmt.Sprintf("%d/%d 개만 배선됐다. 빠진 것: %v (남의 훅 %d개는 그대로)",
				len(wired), len(Events), missing, others),
			"prior init --apply")
		return
	}
	add(r, "훅 배선", health.OK,
		fmt.Sprintf("%d개 전부 (남의 훅 %d개는 손대지 않음)", len(wired), others), "")

	checkBinary(r, o, wired)
}

// checkCodexHooks 는 Codex 쪽 배선을 본다.
//
// **배선을 안 했으면 한 줄도 안 낸다.** Codex 를 안 쓰는 사람(대부분)에게 매번
// "Codex 가 배선 안 됐다" 가 뜨면 그 줄은 곧 배경이 되고, 배경이 되면 진짜 실패도
// 같이 안 읽힌다. 그래서 **이미 배선한 것만** 검사한다.
//
// 잡아야 할 진짜 고장은 `--host codex` 가 빠진 배선이다. 그러면 훅은 멀쩡히 돌고
// 안전망도 도는데 **컨텍스트 주입만 사라진다** — Codex 는 평문 stdout 을 안 읽기
// 때문이다. 그리고 훅은 규약상 언제나 exit 0 이라 아무 표시도 안 난다. 옛 바이너리로
// 배선했거나 손으로 고쳤을 때 이 모양이 된다.
func checkCodexHooks(r *health.Report, o DoctorOptions) {
	path := o.CodexSettingsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		path = defaultSettingsPath(home, HostCodex)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return // 파일이 없다 = Codex 를 안 쓴다. 고장이 아니다.
	}
	if !bytes.Contains(raw, []byte(hookMarker)) {
		return // 남의 훅만 있다 = 아직 배선을 안 했다. 그것도 고장이 아니다.
	}

	var root struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		add(r, "Codex 훅", health.Fail, path+" 이 JSON 이 아니다: "+err.Error(), "")
		return
	}
	wired := map[string]string{}
	for ev, groups := range root.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, hookMarker) {
					wired[ev] = h.Command
				}
			}
		}
	}

	want := EventsFor(HostCodex)
	var missing, noFlag []string
	for _, ev := range want {
		name := ev.NameFor(HostCodex)
		cmd, ok := wired[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !strings.Contains(cmd, "--host "+string(HostCodex)) {
			noFlag = append(noFlag, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(noFlag)

	switch {
	case len(missing) > 0:
		add(r, "Codex 훅", health.Fail,
			fmt.Sprintf("%d/%d 개만 배선됐다. 빠진 것: %v", len(wired), len(want), missing),
			"prior init --host codex --apply")
	case len(noFlag) > 0:
		add(r, "Codex 훅", health.Fail,
			fmt.Sprintf("--host codex 가 빠진 훅이 있다 %v — 훅은 돌지만 회수 주입이 "+
				"통째로 사라진다 (Codex 는 평문 stdout 을 안 읽는다)", noFlag),
			"prior init --host codex --apply")
	default:
		add(r, "Codex 훅", health.OK, fmt.Sprintf("%d개 전부 (%s)", len(wired), path), "")
	}

	// **Codex 에서는 데몬이 선택이 아니다.**
	//
	// Claude Code 는 SessionEnd 훅이 세션 끝에 아크를 판정한다(scan.go). Codex 에는
	// 그 이벤트가 아예 없으므로 그 자리를 데몬의 arcStale(20분 침묵, daemon.go)이
	// 대신해야 한다 — 안 띄우면 **자동 기록이 압축될 때만 돈다.**
	//
	// 일반 "안전망" 줄로는 이게 안 보인다. 거기는 "데몬이 없어도 훅이 대신 훑는다" 고
	// 말하는데, 그 말이 Claude Code 에서는 맞고 Codex 에서는 틀리다 — 훑기는 대신
	// 하지만 **승격은 못 한다.** 그래서 별도 줄로 낸다. 이 줄은 Codex 를 배선한
	// 사람에게만 뜨므로 늘 뜨는 경고가 되지 않는다.
	if daemon.IsRunning(o.StateDir) {
		add(r, "Codex 자동기록", health.OK,
			"데몬이 세션 끝 자리를 대신한다 (20분 침묵 뒤 아크 판정)", "")
		return
	}
	add(r, "Codex 자동기록", health.Warn,
		"데몬이 없다 — Codex 에는 SessionEnd 가 없어 자동 기록이 압축될 때만 돈다",
		"prior watch 를 띄워라 (Claude Code 와 달리 Codex 에서는 선택이 아니다)")
}

// checkBinary 는 훅에 박힌 경로가 지금도 실행 가능한지, 그리고 **지금 이 프로세스와
// 같은 것인지** 본다.
//
// 다르면 훅은 옛 바이너리를 부른다. `go install` 로 새로 깔아도 훅이 다른 경로를
// 가리키고 있으면 고친 게 반영되지 않는데, 겉으로는 아무 차이가 없다.
func checkBinary(r *health.Report, o DoctorOptions, wired map[string]string) {
	want := o.Binary
	if want == "" {
		if exe, err := os.Executable(); err == nil {
			want = exe
		}
	}

	paths := map[string]bool{}
	for _, cmd := range wired {
		if p := binaryFromCommand(cmd); p != "" {
			paths[p] = true
		}
	}
	if len(paths) != 1 {
		var list []string
		for p := range paths {
			list = append(list, p)
		}
		sort.Strings(list)
		add(r, "훅 바이너리", health.Warn,
			fmt.Sprintf("훅마다 다른 경로를 부른다 %v", list), "prior init --apply 로 통일하라")
		return
	}
	var got string
	for p := range paths {
		got = p
	}

	if _, err := os.Stat(got); err != nil {
		add(r, "훅 바이너리", health.Fail,
			fmt.Sprintf("%s 를 실행할 수 없다 (%v) — 훅은 언제나 exit 0 이라 조용히 아무 일도 안 한다", got, err),
			"prior init --apply 로 지금 경로에 다시 배선하라")
		return
	}
	if want != "" && !sameFile(got, want) {
		add(r, "훅 바이너리", health.Warn,
			fmt.Sprintf("훅은 %s 를 부르는데 지금 도는 것은 %s 다 — 고친 것이 반영되지 않는다", got, want),
			"prior init --apply")
		return
	}
	add(r, "훅 바이너리", health.OK, got, "")
}

// binaryFromCommand 는 `PRIORCASE_HOOK=1 "<경로>" hook <event>` 에서 경로를 꺼낸다.
func binaryFromCommand(cmd string) string {
	i := strings.Index(cmd, `"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(cmd[i+1:], `"`)
	if j < 0 {
		return ""
	}
	return cmd[i+1 : i+1+j]
}

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// checkJudge 는 자동 승격이 켜져 있는지 본다.
//
// **사용자가 가장 알고 싶어 할 한 줄이다** — 켜져 있으면 에이전트가 안 불러도 결정이
// 기록되고, 꺼져 있으면 에이전트가 부를 때만 남는다. 두 상태의 차이가 크므로
// 어느 쪽인지 분명히 말해야 한다.
//
// **어느 CLI 가 어느 대화를 판정하는지도 말한다.** 2026-08-24 에 사용자가 "Codex 를
// 쓰는데 왜 claude CLI 를 부르나" 를 물었고, 그때 이 줄은 `claude` 경로만 내고 있어서
// 답이 되지 않았다. 판별기가 호스트별로 갈린 뒤에는 그 배치가 보여야 한다 —
// 안 보이면 사용자는 같은 질문을 다시 한다.
func checkJudge(r *health.Report, o DoctorOptions) {
	if o.Config == nil {
		return
	}
	set, ok := judge.Configured(o.Config.Capture.JudgePath)
	if set && !ok {
		add(r, "자동 기록", health.Fail,
			fmt.Sprintf("설정의 judge_path (%s) 를 실행할 수 없다 — 자동 승격이 꺼져 있다",
				o.Config.Capture.JudgePath),
			"경로를 고치거나 judge_path 를 비워 자동 탐색에 맡겨라")
		return
	}
	// **명시 경로는 그것 하나만 쓴다** (judge.FindFor 와 같은 규칙).
	if set {
		one := judge.FindFlavorAt(o.Config.Capture.JudgePath, o.Config.Capture.JudgeModel)
		reportJudges(r, []*judge.CLI{one}, true, len(o.Config.Capture.Signals) == 0)
		return
	}

	// 종류별로 따로 찾는다. 하나만 있으면 그것이 두 호스트의 대화를 다 판정한다
	// (사슬의 폴백). 둘 다 있으면 대화를 만든 호스트의 것이 앞에 선다.
	var found []*judge.CLI
	for _, f := range []judge.Flavor{judge.FlavorClaude, judge.FlavorCodex} {
		m := ""
		if f == judge.FlavorClaude {
			m = o.Config.Capture.JudgeModel
		}
		if c := judge.FindFlavor(f, m); c != nil {
			found = append(found, c)
		}
	}
	reportJudges(r, found, false, len(o.Config.Capture.Signals) == 0)
}

// reportJudges 는 찾은 판별기들을 한 줄로 낸다.
func reportJudges(r *health.Report, found []*judge.CLI, explicit, noSignals bool) {
	var live []*judge.CLI
	var dead []string
	for _, c := range found {
		if c == nil {
			continue
		}
		if err := c.Check(context.Background()); err != nil {
			dead = append(dead, fmt.Sprintf("%s: %v", c.Path, err))
			continue
		}
		live = append(live, c)
	}

	if len(live) == 0 {
		if len(dead) > 0 {
			add(r, "자동 기록", health.Fail,
				"판별기를 찾았지만 답하지 않는다 — "+strings.Join(dead, " · "),
				"로그인했는지 확인하라 (claude 라면 `claude` 를 띄워 /login, codex 라면 `codex login`)")
			return
		}
		detail := "판별기를 찾지 못했다 — 에이전트가 prior capture 를 부를 때만 기록된다"
		// **판별기도 없고 시그널도 없으면 안전망이 통째로 죽는다.** 그때는 경고가
		// 아니라 실패다 — 표시조차 안 되므로 사람이 알 방법이 없다.
		if noSignals {
			add(r, "자동 기록", health.Fail,
				detail+" · [capture] signals 도 비어 있어 표시조차 되지 않는다",
				"claude 나 codex CLI 를 PATH 에 두거나 signals 를 채워라")
			return
		}
		add(r, "자동 기록", health.Warn, detail+
			" · 안전망은 [capture] signals 로만 돈다 (대화 언어와 맞아야 한다)",
			"claude 나 codex CLI 를 PATH 에 두거나 [capture] judge_path 를 적어라")
		return
	}

	var parts []string
	for _, c := range live {
		parts = append(parts, fmt.Sprintf("%s(%s)", c.Flavor, c.Model))
	}
	detail := strings.Join(parts, " · ")
	switch {
	case explicit:
		detail += " — judge_path 로 지정한 것만 쓴다"
	case len(live) == 1:
		// **한쪽만 있으면 그것이 두 호스트의 대화를 다 판정한다.** 이 사실을 말해야
		// "Codex 로 일하는데 왜 claude 가 도나" 를 사용자가 여기서 알 수 있다.
		detail += fmt.Sprintf(" — 두 호스트의 대화를 모두 %s 로 판정한다", live[0].Flavor)
	default:
		detail += " — 대화를 만든 호스트의 CLI 가 먼저 서고, 실패하면 다른 쪽이 받는다"
	}
	if len(dead) > 0 {
		detail += " · 응답 없음: " + strings.Join(dead, " · ")
	}
	add(r, "자동 기록", health.OK,
		detail+" · 에이전트가 안 불러도 세션 끝에 판별기가 기록한다 · "+
			"판별기가 있으므로 [capture] signals 는 쓰이지 않는다", "")
}

// pendingStale 은 이보다 오래된 미확인 구간을 "쌓이고 있다" 로 본다.
//
// 컷오버 회고의 판정 기준이 여기다 — pending 이 계속 쌓인다는 것은 에이전트가
// prior capture 를 안 부르고 있다는 뜻이고, 그러면 이 설계가 실패한 것이다.
const pendingStale = 7 * 24 * time.Hour

func checkDaemon(r *health.Report, o DoctorOptions) {
	if o.StateDir == "" {
		add(r, "안전망", health.Warn, "상태 디렉토리를 정하지 못했다", "")
		return
	}
	items, err := daemon.ReadPending(o.StateDir)
	if err != nil {
		add(r, "안전망", health.Fail,
			"미확인 구간을 읽을 수 없다: "+err.Error(),
			"상태 파일을 지우면 처음부터 시작한다 (이미 기록된 결정은 그대로다)")
		return
	}

	// 데몬이 도는지는 락으로 안다. 안 돌아도 훅이 대신 훑으므로 정상이다.
	running := daemon.IsRunning(o.StateDir)
	mode := "훅이 턴 경계마다 훑는다 (데몬 없음)"
	if running {
		mode = "데몬(prior watch)이 돌고 있다"
	}

	act := ""
	if o.RecentDecisions != nil {
		act = fmt.Sprintf(" · 최근 7일 기록 %d건", *o.RecentDecisions)
	}
	act += activity(o)

	// **훑은 흔적이 오래됐으면 그것이 안전망이 멎었다는 양성 증거다.**
	//
	// "미확인 구간 없음" 은 훑어서 비운 것과 한 번도 안 훑은 것을 구분하지 못한다 —
	// 컷오버 1일차에 실제로 후자를 전자로 보고했다. 다만 **흔적이 없는 것**만으로는
	// 경보를 울릴 수 없다. 갓 깐 설치도, 흔적 필드가 없던 옛 판에서 올라온 설치도
	// 똑같이 비어 있기 때문이다. 그래서 "오래됐다" 는 양성 증거일 때만 운다.
	//
	// **여기서 반환하지 않는다.** 조기 반환하면 아래의 회고 판정(기록 0인데 표시만
	// 쌓인다 = Fail)을 가려 버린다. 가장 심각한 조합이 Warn 한 줄로 강등되는데,
	// 그 조합이야말로 이 진단이 존재하는 이유다.
	silent := ""
	silentFix := ""
	if last := lastScan(o); !last.IsZero() && o.Now.Sub(last) > pendingStale {
		silent = fmt.Sprintf(" — **%s부터 훑은 흔적이 없다**", humanAgo(o.Now, last))
		silentFix = "훅이 아직 붙어 있는지 확인해라 (prior doctor 의 훅 배선 줄, 또는 prior init --apply)"
	}

	if len(items) == 0 {
		if silent != "" {
			add(r, "안전망", health.Warn, mode+act+silent, silentFix)
			return
		}
		add(r, "안전망", health.OK, mode+act+" · 미확인 구간 없음", "")
		return
	}
	stale, orphan := 0, 0
	for _, p := range items {
		if !p.At.IsZero() && o.Now.Sub(p.At) > pendingStale {
			stale++
		}
		// **설정에 없는 도메인을 단 구간은 영원히 승격되지 않는다.**
		//
		// 승격이 판별기 앞에서 걸러 내므로 낭비는 없지만, 그 구간은 큐에 계속 남아
		// 매 세션 다시 뜬다. 그리고 그 사실이 아무 데도 안 보인다 — 사람은 "왜 이건
		// 계속 안 없어지지" 만 겪는다.
		//
		// 실제로 그 상태였다: 개명(casebook → priorcase) 때 state.json 의 pending
		// 도메인을 안 옮겨 7건이 남았다.
		// Config 가 없으면 판정하지 않는다. "모른다" 를 "없다" 로 읽으면
		// 멀쩡한 구간을 고아라고 말하게 된다.
		if o.Config != nil && p.Domain != "" {
			if _, ok := o.Config.FolderFor(p.Domain); !ok {
				orphan++
			}
		}
	}
	detail := fmt.Sprintf("%s%s · 미확인 구간 %d건%s", mode, act, len(items), silent)
	lv := health.Warn
	fix := "확인하고 실제 결정이면 prior capture 로 남겨라"
	if silentFix != "" {
		fix = silentFix
	}

	// **이 조합이 회고의 판정이다.** 표시는 쌓이는데 기록이 0이면 에이전트가
	// prior capture 를 안 부르고 있다는 뜻이고, 그러면 이 설계가 실패한 것이다.
	if o.RecentDecisions != nil && *o.RecentDecisions == 0 {
		lv = health.Fail
		detail += " — **기록이 0인데 표시만 쌓인다.** 결정을 내리고도 안 남기고 있다"
		fix = "결정 시점에 prior capture 를 불러라. 미확인 구간부터 확인하라"
	} else if stale > 0 {
		detail += fmt.Sprintf(" (그중 %d건은 7일 넘게 방치)", stale)
	}
	if orphan > 0 {
		detail += fmt.Sprintf(" · **%d건은 설정에 없는 도메인이라 영영 승격되지 않는다**", orphan)
		fix = "도메인 이름을 바꿨다면 그 구간을 prior pending --resolve 로 지우거나 설정에 도메인을 추가하라"
	}
	add(r, "안전망", lv, detail, fix)
}

// lastScan 은 안전망이 마지막으로 훑은 시각이다. 한 번도 안 훑었으면 제로값이다.
func lastScan(o DoctorOptions) time.Time {
	s := daemon.NewStore(o.StateDir)
	if s.Load() != nil {
		return time.Time{}
	}
	return s.LastScan()
}

// activity 는 안전망이 **실제로 한 일**을 한 줄로 만든다.
//
// 설정이 어떻게 돼 있는지가 아니라 무엇이 일어났는지를 말한다. 앞의 "최근 7일
// 기록 N건" 은 볼트 전체를 세므로 컷오버 전 이관까지 합산되는데(1일차 회고에서
// 62건 = 볼트 총량), 이 줄은 priorcase 가 스스로 한 일만 센다.
func activity(o DoctorOptions) string {
	last := lastScan(o)
	if last.IsZero() {
		// 흔적이 없다. 갓 깐 설치이거나, 흔적 필드가 없던 옛 판에서 올라왔거나,
		// 정말 한 번도 안 돈 것이다 — 셋을 여기서 가릴 수 없으므로 단정하지 않는다.
		return ""
	}
	out := " · 마지막 훑기 " + humanAgo(o.Now, last)

	proms, err := daemon.ReadPromotions(o.StateDir, o.Now.AddDate(0, 0, -7))
	if err != nil {
		return out
	}
	// **조용히 넘긴 표시를 센다.** 이게 없으면 "볼 게 없어서 조용하다" 와 "N번
	// 눈감았다" 가 여전히 같은 문장이다. 컷오버 1일차의 오진이 정확히 그 구분의 부재였다.
	//
	// 문구를 두 가지 이유로 고쳤다.
	//
	//  1. **"면제 N회" 는 이제 거짓말이다.** 그 말은 "기록을 N번 눈감았다" 로 읽히는데,
	//     그게 방금 없앤 동작이다. 면제는 pending 을 지우던 것에서 pending 에 Quiet
	//     표를 다는 것으로 바뀌었다(daemon.Store.Credit) — 기록은 그대로 가고, 묻지
	//     않았는데 들이미는 자리에서만 빠진다.
	//  2. **창이 다르다.** 이 수는 Checkpoint.Suppressed 의 **설치 이후 누적**인데,
	//     같은 줄의 자동 기록·판정은 최근 7일이다. 기간이 다른 수를 나란히 놓아
	//     "면제 6회 / 최근 7일 자동 기록 0건" 이라는 가짜 인과를 읽게 만들었다.
	//     실제 원인은 횟수가 아니라 면제가 pending 을 지운다는 사실 자체였고, 그건
	//     이 줄로는 영영 안 보인다. 누적임을 문구에 박아 그 대조를 막는다.
	if n := suppressed(o); n > 0 {
		out += fmt.Sprintf(" · 표시를 조용히 넘김 %d회(설치 이후 누적)", n)
	}
	if len(proms) == 0 {
		return out + " · 자동 기록 없음"
	}
	// **등급을 나눠 센다.** 기록 계층이 둘로 갈리면서(결정 노트 / 작업 로그) 합계
	// 하나로는 성패를 못 읽는다 — 작업 로그만 잔뜩 쌓이고 결정이 0인 상태와, 결정이
	// 꾸준히 나오는 상태가 똑같이 "자동 기록 N건" 이 된다. 그 둘을 가르는 것이
	// 이번 변경의 성패이고, 사람이 그걸 확인하는 계기판이 이 줄뿐이다.
	//
	// tier 가 없는 줄은 결정으로 센다 — 옛 원장 23줄에 이 키가 없고, 그때는 등급이
	// 하나뿐이라 전부 결정 노트 시도였다(daemon.Promotion.Tier).
	dec, wlog := 0, 0
	for _, p := range proms {
		switch {
		case !p.Recorded:
		case p.Tier == string(judge.TierWorklog):
			wlog++
		default:
			dec++
		}
	}
	// 판정 건수를 같이 낸다 — 0/12 는 "판별기가 안 돈다" 가 아니라 "12번 봤는데
	// 기록할 게 없었다" 이고, 그 둘은 전혀 다른 진단이다.
	return out + fmt.Sprintf(" · 최근 7일 자동 기록 %d건(결정 %d건 · 작업 로그 %d건)/판정 %d건",
		dec+wlog, dec, wlog, len(proms))
}

// suppressed 는 안전망이 조용히 넘긴 표시의 **설치 이후 누적** 횟수다.
//
// 기록을 건너뛴 수가 아니다 — Checkpoint.Suppressed 주석 참고. 창을 좁힐 수 없는
// 이유는 그 값이 체크포인트에 누적 정수 하나로만 있기 때문이다. 기간을 맞추려면
// 상태 파일에 시각을 남겨야 하는데, 그 파일은 매 스캔마다 통째로 다시 쓰인다.
// 그래서 수를 고치는 대신 **문구에 창을 박는다.**
func suppressed(o DoctorOptions) int {
	s := daemon.NewStore(o.StateDir)
	if s.Load() != nil {
		return 0
	}
	return s.Suppressed()
}

// humanAgo 는 경과 시간을 짧게 만든다.
func humanAgo(now, then time.Time) string {
	d := now.Sub(then)
	switch {
	case d < -time.Hour:
		// 시계가 크게 어긋났다. 상대 시간을 말하면 거짓말이 되므로 그대로 보여 준다.
		// **로컬 시간으로** 준다 — 저장은 UTC 라, 그대로 찍으면 사용자가 자기
		// 시계와 몇 시간 어긋난 숫자를 보고 더 헷갈린다.
		return then.Local().Format("2006-01-02 15:04") + " (시계 어긋남)"
	case d < time.Minute:
		// 음수도 여기로 온다. 진단을 읽는 사이에 다른 세션의 훅이 돌면 저장된 시각이
		// 몇 밀리초 앞설 수 있는데, 그때 절대 시각을 뱉으면 방금 일어난 일이
		// 엉뚱한 시각으로 보인다 — 실기기 첫 실행에서 실제로 그랬다.
		return "방금"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	}
}

func add(r *health.Report, name string, lv health.Level, detail, fix string) {
	r.Checks = append(r.Checks, health.Check{Name: name, Level: lv, Detail: detail, Fix: fix})
}

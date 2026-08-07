package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/health"
	"github.com/xian0310567/casebook/internal/daemon"
)

// DoctorOptions 는 진단에 필요한 것이다.
type DoctorOptions struct {
	SettingsPath string
	StateDir     string
	// Binary 는 훅에 배선돼 있어야 할 cb 경로다. 비면 지금 실행 중인 것.
	Binary string
	// Now 는 pending 나이를 재는 기준이다. 비면 현재 시각.
	Now time.Time
}

// Wiring 은 훅 배선과 데몬 상태를 검사해 core 검사 뒤에 붙인다.
//
// **이 검사들이 컷오버로 생긴 취약점을 덮는다.** 훅에는 cb 의 **절대 경로가 박혀** 있고,
// 훅은 규약상 언제나 exit 0 이다. 그래서 그 파일이 사라지거나 다른 버전으로 바뀌면
// 아무 일도 안 하면서 정상으로 보인다 — 이 프로젝트가 죄목으로 드는 조용한 무동작을
// 우리가 만든 것이다. 여기가 그걸 보는 유일한 자리다.
func Wiring(r *health.Report, o DoctorOptions) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	checkPath(r)
	checkHooks(r, o)
	checkDaemon(r, o)
}

// checkPath 는 사용자가 `cb` 를 그냥 칠 수 있는지 본다.
//
// **이게 없으면 진단 자체가 무용지물이다.** cb doctor 가 내는 모든 → 는 `cb 무엇무엇`
// 을 치라는 것인데, PATH 에 없으면 사용자는 그중 하나도 실행할 수 없다. 훅은 절대
// 경로로 배선되므로 **시스템은 멀쩡히 도는데 사람만 손을 못 대는** 상태가 된다.
//
// 실제로 그랬다 — 개발 내내 절대 경로로 불러서 이 구멍을 못 봤고, 사용자가
// `cb doctor` 를 쳤을 때 "command not found" 로 처음 드러났다.
func checkPath(r *health.Report) {
	exe, err := os.Executable()
	if err != nil {
		return // 자기 경로를 모르면 비교할 것이 없다
	}
	found, lerr := exec.LookPath("cb")
	if lerr != nil {
		add(r, "PATH", health.Warn,
			fmt.Sprintf("`cb` 를 PATH 에서 찾을 수 없다 (지금 것: %s) — "+
				"훅은 절대 경로로 돌지만 사람이 명령을 칠 수 없다", exe),
			"PATH 에 있는 디렉토리로 링크하라 (예: ln -s "+exe+" ~/.local/bin/cb)")
		return
	}
	if !sameFile(found, exe) {
		add(r, "PATH", health.Warn,
			fmt.Sprintf("PATH 의 cb(%s)가 지금 도는 것(%s)과 다르다", found, exe),
			"오래된 사본을 지우거나 링크를 다시 걸어라")
		return
	}
	add(r, "PATH", health.OK, found, "")
}

func checkHooks(r *health.Report, o DoctorOptions) {
	raw, err := os.ReadFile(o.SettingsPath)
	if os.IsNotExist(err) {
		add(r, "훅 배선", health.Fail, o.SettingsPath+" 가 없다", "cb init --apply")
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
			"cb init --apply")
		return
	}
	add(r, "훅 배선", health.OK,
		fmt.Sprintf("%d개 전부 (남의 훅 %d개는 손대지 않음)", len(wired), others), "")

	checkBinary(r, o, wired)
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
			fmt.Sprintf("훅마다 다른 경로를 부른다 %v", list), "cb init --apply 로 통일하라")
		return
	}
	var got string
	for p := range paths {
		got = p
	}

	if _, err := os.Stat(got); err != nil {
		add(r, "훅 바이너리", health.Fail,
			fmt.Sprintf("%s 를 실행할 수 없다 (%v) — 훅은 언제나 exit 0 이라 조용히 아무 일도 안 한다", got, err),
			"cb init --apply 로 지금 경로에 다시 배선하라")
		return
	}
	if want != "" && !sameFile(got, want) {
		add(r, "훅 바이너리", health.Warn,
			fmt.Sprintf("훅은 %s 를 부르는데 지금 도는 것은 %s 다 — 고친 것이 반영되지 않는다", got, want),
			"cb init --apply")
		return
	}
	add(r, "훅 바이너리", health.OK, got, "")
}

// binaryFromCommand 는 `CASEBOOK_HOOK=1 "<경로>" hook <event>` 에서 경로를 꺼낸다.
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

// pendingStale 은 이보다 오래된 미확인 구간을 "쌓이고 있다" 로 본다.
//
// 컷오버 회고의 판정 기준이 여기다 — pending 이 계속 쌓인다는 것은 에이전트가
// cb capture 를 안 부르고 있다는 뜻이고, 그러면 이 설계가 실패한 것이다.
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
		mode = "데몬(cb watch)이 돌고 있다"
	}

	if len(items) == 0 {
		add(r, "안전망", health.OK, mode+" · 미확인 구간 없음", "")
		return
	}
	stale := 0
	for _, p := range items {
		if !p.At.IsZero() && o.Now.Sub(p.At) > pendingStale {
			stale++
		}
	}
	lv, fix := health.Warn, "확인하고 실제 결정이면 cb capture 로 남겨라"
	detail := fmt.Sprintf("%s · 미확인 구간 %d건", mode, len(items))
	if stale > 0 {
		detail += fmt.Sprintf(" (그중 %d건은 7일 넘게 방치 — 기록이 안 되고 있다는 신호다)", stale)
	}
	add(r, "안전망", lv, detail, fix)
}

func add(r *health.Report, name string, lv health.Level, detail, fix string) {
	r.Checks = append(r.Checks, health.Check{Name: name, Level: lv, Detail: detail, Fix: fix})
}

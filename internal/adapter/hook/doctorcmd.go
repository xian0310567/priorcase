package hook

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/health"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
)

// NewDoctorCommand 는 `cb doctor` 를 만든다.
//
// 이 명령이 hook 패키지에 있는 이유는 검사 대상의 절반이 Claude Code 배선이기
// 때문이다 — settings.json 형식과 CASEBOOK_HOOK 표식은 이 어댑터의 지식이다.
// cli 에 두면 cli 가 hook 을 import 해야 하고 §4.1 이 깨진다.
func NewDoctorCommand() *cobra.Command {
	var settingsPath, stateDir string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "지금 casebook 이 제대로 돌고 있는지 검사한다",
		Long: "설정 · 볼트 · 색인 · 훅 배선 · 안전망을 한 번에 본다.\n\n" +
			"**이 명령이 있는 이유는 조용한 무동작이다.** 이 시스템의 부품은 전부 실패해도 " +
			"대화를 막지 않도록 만들어졌다 — 훅은 무슨 일이 있어도 exit 0 이고, 회수는 못 " +
			"찾으면 아무것도 안 내고, 데몬은 백그라운드다. 그 대가로 고장이 정상과 구별되지 " +
			"않는다. 여기가 그걸 구별하는 자리다.\n\n" +
			"문제가 있으면 종료 코드가 0이 아니다 (경고 1 · 오류 2).",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			r := &health.Report{}

			cfgPath, _ := cmd.Flags().GetString("config")
			c, err := config.Load(cfgPath)
			if err != nil {
				// 설정이 없으면 나머지를 볼 수 없다. 그래도 조용히 죽지는 않는다.
				r.Checks = append(r.Checks, health.Check{
					Name: "설정", Level: health.Fail, Detail: err.Error(),
					Fix: "cb init --apply 가 기본 설정을 만든다"})
				return report(out, r)
			}
			resolved, _ := config.ResolvePath(cfgPath)
			r.Checks = append(r.Checks, health.Check{Name: "설정", Level: health.OK, Detail: resolved})

			l := store.NewLayout(c)
			r.Checks = append(r.Checks, health.Vault(c, l).Checks...)

			if settingsPath == "" {
				if home, err := os.UserHomeDir(); err == nil {
					settingsPath = filepath.Join(home, ".claude", "settings.json")
				}
			}
			if stateDir == "" {
				if d, err := daemon.DefaultDir(); err == nil {
					stateDir = d
				}
			}
			Wiring(r, DoctorOptions{
				SettingsPath:    settingsPath,
				StateDir:        stateDir,
				RecentDecisions: recentPtr(health.RecentDecisions(l, time.Now(), 7)),
			})
			return report(out, r)
		},
	}
	f := cmd.Flags()
	f.StringVar(&settingsPath, "settings", "", "Claude Code 설정 경로 (기본: ~/.claude/settings.json)")
	f.StringVar(&stateDir, "state-dir", "", "데몬 상태 디렉토리 (기본: $XDG_STATE_HOME/casebook)")
	return cmd
}

// report 는 검사 결과를 찍고 종료 코드를 정한다.
func report(out io.Writer, r *health.Report) error {
	width := 0
	for _, ck := range r.Checks {
		if w := displayWidth(ck.Name); w > width {
			width = w
		}
	}
	for _, ck := range r.Checks {
		pad := strings.Repeat(" ", width-displayWidth(ck.Name)+2)
		fmt.Fprintf(out, "%s %s%s%s\n", ck.Level.Mark(), ck.Name, pad, ck.Detail)
		if ck.Level != health.OK && ck.Fix != "" {
			fmt.Fprintf(out, "   → %s\n", ck.Fix)
		}
	}
	switch r.Worst() {
	case health.OK:
		fmt.Fprintln(out, "\n이상 없다.")
		return nil
	case health.Warn:
		fmt.Fprintln(out, "\n동작하지만 손해가 있다. 위의 → 를 따라라.")
	default:
		fmt.Fprintln(out, "\n동작하지 않는 것이 있다. 위의 → 를 따라라.")
	}
	return diagnostic{r.Worst()}
}

// displayWidth 는 터미널에서 차지하는 칸 수다. 한글은 두 칸이다 —
// %-12s 는 바이트를 세므로 한글 이름이 섞이면 표가 어긋난다.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x1100 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// diagnostic 은 종료 코드를 나르는 에러다. **메시지가 비어 있다** — 진단 결과는 이미
// stdout 에 찍었고, 여기서 또 내면 같은 말이 두 번 나온다.
type diagnostic struct{ lv health.Level }

func (d diagnostic) Error() string { return "" }

// DiagnosticExit 은 진단 결과의 종료 코드를 준다. 두 번째 값이 true 면 **메시지를
// 더 찍지 마라** — 이미 찍었다.
//
// 자동화가 기계적으로 읽을 수 있어야 해서 경고(1)와 오류(2)를 나눈다.
func DiagnosticExit(err error) (code int, silent bool) {
	var d diagnostic
	if !errors.As(err, &d) {
		return 0, false
	}
	if d.lv == health.Warn {
		return 1, true
	}
	return 2, true
}

// recentPtr 는 최근 기록 수를 포인터로 감싼다. 음수(집계 실패)는 nil 로 — 모르는 것과
// 0건은 다른 사실이고, 후자만 경보 대상이다.
func recentPtr(n int) *int {
	if n < 0 {
		return nil
	}
	return &n
}

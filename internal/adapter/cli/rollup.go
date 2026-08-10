package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/rollup"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// newRollupCmd 는 `prior rollup` 이다.
//
// **LLM 을 부르지 않는다.** 옛 rollup.sh 는 `claude -p` 로 요약문을 만들었는데, 그건
// 데몬에서 걷어낸 것과 같은 의존이다 — 키 등록이 오픈소스 진입 장벽이고, 어차피 세션에
// 도는 에이전트가 이미 전체 맥락을 갖고 있다.
//
// 그래서 세 걸음으로 나눈다. 기계적인 것은 우리가, 산문은 에이전트가 쓴다.
//
//	prior rollup                              어느 주가 남았나
//	prior rollup <project> <week>             그 주의 로그를 읽는다
//	prior rollup <project> <week> --body -    쓴 요약을 붙인다
func newRollupCmd() *cobra.Command {
	var bodyFile string

	cmd := &cobra.Command{
		Use:   "rollup [project] [week]",
		Short: "작업 로그를 주 단위로 묶는다 (요약문은 에이전트가 쓴다)",
		Long: "작업 로그를 주 단위로 묶어 요약 파일에 붙인다. **원본은 손대지 않는다.**\n\n" +
			"**요약문은 priorcase 가 만들지 않는다.** 어느 주가 남았는지 찾고, 그 주의 " +
			"로그를 뽑고, 중복 없이 붙이는 일만 한다. 무엇을 요약이라 부를지는 전체 맥락을 " +
			"가진 에이전트가 정한다 — prior capture 와 같은 구조다.\n\n" +
			"  prior rollup                              어느 주가 남았나\n" +
			"  prior rollup priorcase 2026-W31            그 주의 로그를 읽는다\n" +
			"  prior rollup priorcase 2026-W31 --body -   쓴 요약을 붙인다\n\n" +
			"진행 중인 주는 건너뛴다 — 주가 끝나기 전에 요약하면 반쪽이 된다.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			if len(args) == 0 {
				return listWeeks(out, l, time.Now())
			}
			if len(args) != 2 {
				return fmt.Errorf("프로젝트와 주를 함께 줘라 (예: prior rollup priorcase 2026-W31)")
			}
			prefix, week := args[0], args[1]

			if bodyFile == "" {
				block, err := rollup.Block(l, prefix, week)
				if err != nil {
					return err
				}
				fmt.Fprint(out, block)
				return nil
			}

			summary, err := readBody(cmd, bodyFile)
			if err != nil {
				return err
			}
			path, err := rollup.Append(l, prefix, week, summary)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "붙였다: %s (%s)\n", l.RelPath(path), week)
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body", "", "요약문 파일 경로. - 이면 표준입력")
	return cmd
}

// listWeeks 는 요약이 필요한 주를 보여 준다.
//
// 건너뛴 주도 **이유와 함께** 보여 준다. 목록에서 조용히 빠지면 왜 요약이 안 되는지
// 알 수 없고, 그게 이 시스템이 죄목으로 드는 침묵이다.
func listWeeks(out io.Writer, l *store.Layout, now time.Time) error {
	weeks, err := rollup.Scan(l, now)
	if err != nil {
		return err
	}
	if len(weeks) == 0 {
		fmt.Fprintln(out, "작업 로그에 날짜 헤딩이 없다 — 요약할 주가 없다.")
		return nil
	}

	todo := 0
	var last string
	for _, w := range weeks {
		if w.Prefix != last {
			fmt.Fprintf(out, "\n%s\n", w.Prefix)
			last = w.Prefix
		}
		switch {
		case w.Done:
			fmt.Fprintf(out, "  %s  이미 요약됨\n", w.Week)
		case w.Current:
			fmt.Fprintf(out, "  %s  진행 중인 주 — 끝나면 요약한다\n", w.Week)
		case w.Short:
			fmt.Fprintf(out, "  %s  내용 부족 (%dB) — 건너뛴다\n", w.Week, w.Bytes)
		default:
			todo++
			fmt.Fprintf(out, "  %s  → 요약 필요 (%dB)\n", w.Week, w.Bytes)
		}
	}
	if todo == 0 {
		fmt.Fprintln(out, "\n요약할 주가 없다.")
		return nil
	}
	fmt.Fprintf(out, "\n요약이 필요한 주 %d개. 한 주씩:\n", todo)
	fmt.Fprintln(out, "  1. prior rollup <프로젝트> <주>            로그를 읽는다")
	fmt.Fprintln(out, "  2. 읽고 요약문을 쓴다                   ← 여기는 에이전트가 한다")
	fmt.Fprintln(out, "  3. prior rollup <프로젝트> <주> --body -   붙인다")
	return nil
}

func readBody(cmd *cobra.Command, path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

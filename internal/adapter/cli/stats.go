package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/stats"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// `prior stats` 는 **볼트가 좋은 기억을 만들고 있는지** 본다 (core/stats 의 §).
func newStatsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "볼트가 좋은 기억을 만들고 있는지 잰다",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			// **한 번만 읽는다.** 결정·참고·규칙을 따로 읽으면 볼트를 세 번 훑는다.
			cp, err := search.LoadCorpus(l)
			if err != nil {
				return err
			}
			// 끊긴 링크는 **볼트 전체**를 알아야 셀 수 있다 (stats.Input 의 §).
			// 못 훑어도 통계 전체를 죽이지 않는다 — 그 지표만 0 으로 빠진다.
			vaultStems, serr := l.AllStems()
			if serr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"경고: 볼트를 훑지 못해 끊긴 링크는 세지 않는다: %v\n", serr)
			}
			r := stats.Compute(stats.Input{
				Decisions: cp.Notes, References: cp.Refs, Rules: cp.Rules,
				Skipped: cp.Skipped, VaultStems: vaultStems,
				// **오늘을 여기서 준다** (stats.Input.Today 의 §).
				Today: time.Now().Format("2006-01-02"),
			})
			if asJSON {
				// 앱이 이걸 쓴다. 앱이 직접 계산하지 않는다 — 계산이 두 벌이 되면
				// 화면의 수와 CLI 의 수가 갈리고, 갈린 것은 조용하다.
				return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
			}
			renderStats(cmd.OutOrStdout(), l, r)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "기계 판독용")
	return cmd
}

func renderStats(w io.Writer, l *store.Layout, r stats.Report) {
	pct := func(n, t int) string { return fmt.Sprintf("%.0f%%", stats.Pct(n, t)) }
	tot := r.Decisions.Total
	line := func(label string, v any, note string) {
		if note == "" {
			fmt.Fprintf(w, "  %-16s %v\n", label, v)
			return
		}
		fmt.Fprintf(w, "  %-16s %-10v %s\n", label, v, note)
	}

	fmt.Fprintf(w, "%s\n\n", l.Vault())

	fmt.Fprintln(w, "결정")
	line("전체", tot, "")
	line("active", r.Decisions.Active, pct(r.Decisions.Active, tot))
	line("superseded", r.Decisions.Superseded, pct(r.Decisions.Superseded, tot))
	line("regretted", r.Decisions.Regretted, pct(r.Decisions.Regretted, tot))
	line("retracted", r.Decisions.Retracted, pct(r.Decisions.Retracted, tot))
	if r.Decisions.Other > 0 {
		// 0 이 아니면 판이 갈렸다는 신호다 — 평소에는 줄 자체를 안 낸다.
		line("모르는 status", r.Decisions.Other, "더 새 판이 썼거나 손으로 적은 값이다")
	}

	// ★ 이 무리가 이 명령의 이유다. 결정을 쌓는 것은 쉽고 결과를 아는 것은 아무도 안 한다.
	fmt.Fprintln(w, "\n학습 — 판단이 학습으로 바뀌고 있나")
	line("결과 앎", r.Learning.OutcomeKnown, pct(r.Learning.OutcomeKnown, tot))
	line("결과 모름", r.Learning.OutcomePending, pct(r.Learning.OutcomePending, tot))
	line("회고 있음", r.Learning.Retrospectives, pct(r.Learning.Retrospectives, tot))
	line("기대 적음", r.Learning.WithExpected, pct(r.Learning.WithExpected, tot))
	line("성공조건 적음", r.Learning.WithSuccessMetric, pct(r.Learning.WithSuccessMetric, tot))
	line("다시볼날 적음", r.Learning.WithReviewAfter, pct(r.Learning.WithReviewAfter, tot))
	if r.Learning.ReviewsDue > 0 {
		line("밀린 예정 회고", r.Learning.ReviewsDue, "prior queue 가 목록을 준다")
	}

	fmt.Fprintln(w, "\n그래프 — 판단이 서로 이어져 있나")
	line("고아", r.Graph.Orphans, pct(r.Graph.Orphans, tot)+" (들어오지도 나가지도 않는다)")
	line("링크", r.Graph.Edges, "")
	line("평균 차수", fmt.Sprintf("%.1f", r.Graph.AvgDegree), fmt.Sprintf("중앙 %d", r.Graph.MedianDegree))
	line("도메인 넘는 링크", r.Graph.CrossDomain, "프로젝트를 잇는 유일한 수단이다")
	if r.Graph.Dangling > 0 {
		line("끊긴 링크", r.Graph.Dangling, "prior doctor 가 어느 것인지 알려 준다")
	}

	fmt.Fprintln(w, "\n자료")
	line("참고 문서", r.Quality.References, "")
	line("규칙", r.Quality.Rules, "")
	if r.Quality.NoTags > 0 {
		line("태그 없음", r.Quality.NoTags, pct(r.Quality.NoTags, tot)+" — 회수 어휘를 안 넓힌다")
	}
	if r.Quality.Unreadable > 0 {
		line("못 읽음", r.Quality.Unreadable, "prior doctor 가 이유를 알려 준다")
	}

	// **단일 점수를 안 만든다** (core/stats 의 §). 대신 무엇을 보라고만 말한다.
	fmt.Fprintln(w, "\n이 수들을 하나로 접지 않는다 — 고아와 결과 기록은 다른 문제이고 고치는 법도 다르다.")
}

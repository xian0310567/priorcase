package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

func newCaptureCmd() *cobra.Command {
	var r capture.Request
	var bodyFile string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "결정을 기록한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			if bodyFile == "-" {
				if r.Body, err = io.ReadAll(cmd.InOrStdin()); err != nil {
					return err
				}
			} else if bodyFile != "" {
				if r.Body, err = os.ReadFile(bodyFile); err != nil {
					return err
				}
			}
			// **여기서 채운다.** capture 는 core 라 "지금 어느 디렉토리인가" 를
			// 몰라야 하고, 그건 어댑터가 아는 것이다.
			if r.Author == "" {
				wd, _ := os.Getwd()
				r.Author = c.AuthorFor(wd)
			}
			res, err := capture.Do(l, c, r)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "기록됨: %s\n", l.RelPath(res.Path))
			// 편승 검색 실패는 기록을 실패시키지 않는다 — 노트는 이미 저장됐다.
			// 그래도 조용히 넘어가지는 않는다: 여기서 알리지 않으면 "관련 결정이
			// 없다" 와 "찾아보지 못했다" 가 구별되지 않는다.
			if res.RelatedErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"경고: 관련 과거 결정을 찾지 못했다 (기록은 됐다): %v\n", res.RelatedErr)
			}
			// 방금 쓴 노트는 색인에 들어갔지만 색인 자체가 불완전할 수 있다.
			warnSkipped(cmd.ErrOrStderr(), l, res.Skipped)
			warnDroppedRelated(cmd.ErrOrStderr(), res.DroppedRelated)
			// 태그가 회수 어휘를 안 넓히면 그 자리에서 알린다 (막지는 않는다).
			warnVocabulary(cmd.ErrOrStderr(), l, res.Path)
			if len(res.Related) > 0 {
				fmt.Fprintln(out, "\n관련 과거 결정:")
				for _, h := range res.Related {
					fmt.Fprintf(out, "  - %s %s\n", h.Note.Meta.Date, h.Note.Meta.Summary)
				}
				fmt.Fprint(out, linkNudge(l, res.Path, r.Related, res.Related))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Domain, "domain", "", "도메인 접두어 (필수)")
	f.StringVar(&r.Slug, "slug", "", "파일명 slug (필수)")
	f.StringVar(&r.Summary, "summary", "", "한 줄 요약 (필수)")
	f.StringVar(&r.Date, "date", "", "YYYY-MM-DD (기본: 오늘)")
	f.StringSliceVar(&r.Supersedes, "supersedes", nil,
		"뒤집는 결정의 stem (반복 가능 — 한 결정이 여럿을 뒤집을 수 있다)")
	// **--supersedes 만으로는 "왜" 가 안 남는다.** 옛 노트에는 status=superseded 와
	// 양방향 링크만 찍혔고, 실볼트 18노트 중 번복 사유가 기록된 것은 0건이었다.
	// 링크만 보고서는 다음 사람이 "이건 왜 버렸지" 를 처음부터 다시 판다.
	f.StringVar(&r.SupersedeReason, "reason", "", "뒤집는 이유 — 측정 결과·계기를 한 줄로 (--supersedes 와 짝)")
	f.StringVar(&r.SourceSession, "session", "", "출처 세션 ID")
	f.StringVar(&r.Author, "author", "", "이 결정을 내린 사람 (기본: 설정의 author 또는 git 신원)")
	f.StringSliceVar(&r.Tags, "tag", nil, "태그 (반복 가능)")
	f.StringSliceVar(&r.Related, "related", nil,
		"근거로 삼은 문서의 위키링크 [[stem]] — 다른 프로젝트 결정도 넣는다 (반복 가능)")
	f.StringVar(&bodyFile, "body", "", "본문 파일 경로. - 이면 표준입력")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("slug")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}

// linkNudge 는 **후보를 보여 준 그 자리에서 링크를 걸라고** 시킨다.
//
// # 고치려는 고장 (2026-09-02 실측)
//
// 볼트 결정 668건 중 **291건(43.6%)이 고아**고, 링크 작성률이 **떨어지고 있다**:
// 2026-08 53.8% → 2026-09 29.8%.
//
// 원인은 이 자리였다. capture 는 관련 과거 결정을 **이미 찾는다**. 그런데 화면에
// 출력만 하고 아무 일도 시키지 않았다 — 채우려면 `--related` 를 손으로 넣어야 하는데,
// 후보를 본 그 순간에 무엇을 하라는 말이 없으니 아무도 안 넣는다. 이 프로젝트가
// 오늘 하루 종일 만난 패턴과 같다: **계산해 놓고 적용하지 않는다.**
//
// 수단은 이미 있었다 — `prior review <stem> --related` 가 기존 값을 지우지 않고
// 덧붙인다. 없던 것은 그것을 쓰라는 말 한 줄이다.
//
// # 왜 자동으로 안 박는가
//
// 회수 품질이 중간이다 — 2026-09-02 실측으로 사람이 이어 둔 노트를 어휘 회수가
// 상위3에 넣는 것이 32.3%다. 자동으로 박으면 **틀린 링크가 굳고**, 그건 나중에
// 링크를 회수에 쓰기 시작할 때 그대로 오염이 된다. 후보를 주고 호스트가 판정한다.
//
// # 왜 링크를 늘려야 하는가
//
// 지금 `related` 는 회수가 안 읽는다. 그래도 늘려야 하는 이유가 둘이다.
// ① 옵시디언에서 사람이 따라가는 길이고 ② 링크 이웃 확장을 켜려면 밀도가 필요하다 —
// 실측으로 이웃의 81.6%가 1표라 순서를 매길 신호가 없었고, 그 원인이 평균 차수 2.2다.
func linkNudge(l *store.Layout, path string, asked []string, found []search.Hit) string {
	// **이미 걸었으면 조용하다.** 매번 뜨는 안내는 며칠이면 안 읽힌다.
	if len(asked) > 0 || len(found) == 0 {
		return ""
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")
	var b strings.Builder
	b.WriteString(l.Lang().T(
		"\n**위 후보를 읽고 진짜 관련 있는 것만 걸어라.** 회수가 낱말로 고른 것이라 그냥 스친 것이 섞여 있다.\n",
		"\n**Read the candidates above and link only the ones that truly relate.** Recall picked them by words, so some merely brushed past.\n"))
	fmt.Fprintf(&b, "  prior review %s --related <stem>\n", stem)
	b.WriteString(l.Lang().T(
		"관련 없으면 안 걸어도 된다 — 틀린 링크는 안 건 것보다 나쁘다.\n",
		"Skip it if none relate — a wrong link is worse than no link.\n"))
	return b.String()
}

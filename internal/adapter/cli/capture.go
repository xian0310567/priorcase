package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/capture"
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
			warnPreserved(cmd.ErrOrStderr(), l, res.IndexPreserved)
			if len(res.Related) > 0 {
				fmt.Fprintln(out, "\n관련 과거 결정:")
				for _, h := range res.Related {
					fmt.Fprintf(out, "  - %s %s\n", h.Note.Meta.Date, h.Note.Meta.Summary)
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Domain, "domain", "", "도메인 접두어 (필수)")
	f.StringVar(&r.Slug, "slug", "", "파일명 slug (필수)")
	f.StringVar(&r.Summary, "summary", "", "한 줄 요약 (필수)")
	f.StringVar(&r.Date, "date", "", "YYYY-MM-DD (기본: 오늘)")
	f.StringSliceVar(&r.Supersedes, "supersedes", nil, "뒤집는 결정의 stem (반복 가능 — 한 결정이 여럿을 뒤집을 수 있다)")
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

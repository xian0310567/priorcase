package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

func newCaptureCmd() *cobra.Command {
	var r capture.Request
	var bodyFile string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "결정을 기록한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("config")
			c, err := config.Load(path)
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
			l := store.NewLayout(c)
			res, err := capture.Do(l, c, r)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "기록됨: %s\n", l.RelPath(res.Path))
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
	f.StringVar(&r.Supersedes, "supersedes", "", "뒤집는 결정의 stem")
	f.StringVar(&r.SourceSession, "session", "", "출처 세션 ID")
	f.StringSliceVar(&r.Tags, "tag", nil, "태그 (반복 가능)")
	f.StringSliceVar(&r.Related, "related", nil, "관련 결정 위키링크 (반복 가능)")
	f.StringVar(&bodyFile, "body", "", "본문 파일 경로. - 이면 표준입력")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("slug")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}

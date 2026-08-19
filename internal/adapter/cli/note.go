package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/worklog"
)

// newNoteCmd 는 `prior note` 다 — **확정 전의 것**을 작업 로그에 덧붙인다.
//
// capture 와 짝이지만 문턱이 훨씬 낮다. 그 차이가 이 명령의 존재 이유다: 실측으로
// 최근 7일 판정 23건 중 자동 기록은 0건이었고, 기각 사유의 절반(11건)이 "아직 최종
// 결정이 내려지지 않았다" 였다. 그런데 같은 세션에서 사람이 손으로 결정 노트 8건을
// 썼다 — 판별기가 버린 바로 그 내용을. 문제는 판정이 아니라 담을 그릇이 없었다는 것이다.
//
// # capture 와 무엇이 다른가
//
//   - **--slug 가 없다.** 작업 로그는 한 파일에 덧붙이는 것이라 파일명을 지을 필요가
//     없다. 요구했다면 "이름 붙일 만큼 정리된 것" 만 남게 되어 문턱을 낮춘 의미가 사라진다.
//   - **--domain 이 필수가 아니다.** 비면 cwd 로 판정한다. 셸에서 한 줄 남기는 것이
//     이 명령의 주 용도인데, 매번 도메인을 치게 하면 그만큼 안 치게 된다.
//   - **편승(관련 과거 결정)을 붙이지 않는다.** capture 는 결정 시점이라 과거 결정이
//     가장 정확하게 닿는 순간이지만, note 는 자주 불리라고 만든 것이다. 매번 회수
//     결과가 딸려 나오면 그 자체가 부르기를 망설이게 만드는 비용이 된다.
func newNoteCmd() *cobra.Command {
	var e worklog.Entry
	var bodyFile string
	var tagsAlias []string

	cmd := &cobra.Command{
		Use:   "note",
		Short: "확정 전의 것을 작업 로그에 남긴다",
		Long: "검토한 대안과 각각을 왜 기각했는지, 측정값과 그 방법, 걸린 제약,\n" +
			"아직 못 정한 것과 그것이 풀리는 조건을 작업 로그에 덧붙인다.\n\n" +
			"회수에 자동 주입되지 않으므로 자주 남겨도 회수 품질이 나빠지지 않는다.\n" +
			"확정된 결정은 `prior capture` 가 결정 노트로 받는다.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			if bodyFile == "-" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				e.Body = string(b)
			} else if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				e.Body = string(b)
			}
			// **여기서 채운다.** worklog 는 core 라 "지금 어느 디렉토리인가" 를
			// 몰라야 하고, 그건 어댑터가 아는 것이다 (capture 와 같은 규칙).
			if e.Domain == "" {
				wd, _ := os.Getwd()
				e.Domain = c.DomainForCwd(wd)
			}
			if e.Domain == "" {
				// **여기까지 오는 경우는 하나뿐이다: 설정에 default_domain 이 없다.**
				//
				// config.DomainForCwd 는 어느 [[domain]] paths 에도 안 걸리면
				// DefaultDomain 을 준다(config.go 의 마지막 return). 그래서 매핑 안 된
				// 디렉토리에서 돌려도 보통은 폴백 도메인으로 간다 — 이 가지가 그것을
				// 막지는 못한다. 막으려 하지도 않는다: 폴백은 설정에 적어 둔 의도다.
				//
				// 그 폴백마저 비어 있으면 쓸 자리가 없다. 그때 아무 데나 쓰면 나중에
				// 그 항목을 찾을 사람이 엉뚱한 볼트를 뒤진다.
				return fmt.Errorf("도메인을 정할 수 없다 — cwd 가 설정의 어느 도메인에도 안 걸리고 " +
					"default_domain 도 비어 있다. --domain 을 주거나 설정에 " +
					"[[domain]] 경로 또는 default_domain 을 추가하라")
			}

			e.Tags = append(e.Tags, tagsAlias...)

			res, err := worklog.Append(l, e)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if res.Skipped {
				// 덮어쓰지 않았다는 사실을 말한다. "남겼다" 로 뭉뚱그리면 같은 구간을
				// 다시 훑는 것이 정상인 데몬 경로에서 중복 여부를 알 길이 없어진다.
				fmt.Fprintf(out, "이미 있어 건너뜀: %s\n", l.RelPath(res.Path))
				return nil
			}
			fmt.Fprintf(out, "작업 로그에 남겼다: %s\n", l.RelPath(res.Path))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&e.Domain, "domain", "", "도메인 접두어 (기본: cwd 로 판정)")
	f.StringVar(&e.Title, "summary", "", "한 줄 제목 — 무엇을 검토·측정·기각·보류했는지 (필수)")
	f.StringVar(&e.Date, "date", "", "YYYY-MM-DD (기본: 오늘)")
	f.StringVar(&e.Session, "session", "", "출처 세션 ID")
	f.StringSliceVar(&e.Tags, "tag", nil, "태그 (반복 가능)")
	f.StringVar(&bodyFile, "body", "", "본문 파일 경로. - 이면 표준입력")
	// `--tags` 도 받되 도움말에서는 감춘다.
	//
	// 정본은 capture 와 같은 단수 `--tag` 다 — 두 명령이 같은 것을 다르게 부르면
	// 매번 어느 쪽이었는지 헷갈린다. 그런데 복수형을 먼저 치는 것도 자연스럽고,
	// 그때 cobra 는 "unknown flag" 로 죽는다. **한 줄 남기려던 사람이 거기서 그만둔다.**
	// 문턱을 낮추려고 만든 명령이 문턱에서 사람을 돌려보내는 것이 이 명령에서는
	// 가장 나쁜 실패라, 받아 주고 조용히 합친다.
	f.StringSliceVar(&tagsAlias, "tags", nil, "--tag 의 별칭")
	_ = f.MarkHidden("tags")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}

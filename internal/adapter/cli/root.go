package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Version 은 릴리스 시 -ldflags 로 주입된다.
var Version = "dev"

// NewRootCmd 는 **cli 어댑터가 소유한** 서브커맨드만 붙인 루트를 만든다.
// 다른 어댑터의 서브커맨드(prior mcp 등)는 조립 루트인 cmd/prior 가 붙인다 —
// 어댑터끼리 서로를 import 하지 않기 위해서다 (§4.1, internal/arch 가 강제한다).
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "prior",
		Short:         "priorcase — 결정을 기록하고 회수한다",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.PersistentFlags().String("config", "", "설정 파일 경로 (기본: $XDG_CONFIG_HOME/priorcase/config.toml)")
	root.AddCommand(newRecallCmd())
	root.AddCommand(newNoteCmd())
	root.AddCommand(newCaptureCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newRollupCmd())
	root.AddCommand(newQueueCmd())
	root.AddCommand(newPromoteCmd())
	root.AddCommand(newPathCmd())
	root.AddCommand(newSettingsCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newVaultCmd())
	root.AddCommand(newDomainCmd())
	root.AddCommand(newHostsCmd())
	root.AddCommand(newBriefCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newEditCmd())
	return root
}

// loadFrom 은 --config 플래그로 설정을 읽고 그 설정과 Layout 을 함께 준다.
//
// 설정 로딩 진입점이 하나뿐이어야 하는 이유: Layout 은 config.Config 를 비공개
// 필드로 감추므로 Config 도 필요한 명령(capture·recall)은 Layout 만 받아서는
// 일을 못 한다. 그래서 예전에는 index·review 만 이 헬퍼를 쓰고 capture·recall
// 은 config.Load 를 직접 불렀는데, 그러면 설정 경로 해석 규칙(플래그 →
// PRIORCASE_CONFIG → XDG)이 두 자리에 생겨 한쪽만 고치는 사고가 난다.
// 둘을 같이 돌려주면 갈래가 없어진다.
func loadFrom(cmd *cobra.Command) (*config.Config, *store.Layout, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, nil, err
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	// **cwd 의 볼트를 쓴다.** 프로젝트가 볼트를 정하므로, 어느 디렉토리에서 명령을
	// 쳤는지가 곧 어느 볼트를 볼지다. 이걸 기본 볼트로 두면 다른 프로젝트에서
	// `prior recall` 을 쳤을 때 남의 볼트를 뒤진다.
	//
	// cwd 를 못 얻으면 기본 볼트로 간다 — 그건 드물고, 그때도 명령이 돌아야 한다.
	wd, werr := os.Getwd()
	if werr != nil {
		return c, store.NewLayout(c), nil
	}
	l, err := store.LayoutForCwd(c, wd)
	if err != nil {
		return nil, nil, err
	}
	return c, l, nil
}

// warnSkipped 는 읽지 못해 건너뛴 결정 노트를 알린다. 없으면 아무것도 안 낸다.
//
// **항상 stderr 로 낸다.** `prior recall --format inject` 의 stdout 은 훅이 그대로
// 에이전트 컨텍스트에 밀어넣는 순수 데이터다 — 거기에 경고가 한 줄이라도 섞이면
// "[과거 결정 참조]" 블록이 오염된다. 그래서 경고 경로를 명령마다 나누지 않고
// 여기 하나로 모아, 오염될 수 있는 자리를 아예 없앤다. 사람은 터미널에서 보고,
// 파이프는 받지 않는다.
//
// 파일 목록을 다 찍는다(자르지 않는다). "6건 건너뜀" 만으로는 사용자가 무엇을
// 고쳐야 할지 알 수 없고, 건너뛴 노트는 원래 흔해서는 안 되는 것이라 길어질
// 일이 정상 상태에는 없다.
func warnSkipped(w io.Writer, l *store.Layout, skipped []store.SkippedNote) {
	if len(skipped) == 0 {
		return
	}
	fmt.Fprintf(w, "경고: 결정 노트 %d건을 읽지 못해 건너뛰었다 — 색인·회수에서 빠진다:\n", len(skipped))
	for _, s := range skipped {
		// 원인이 여러 줄일 수 있다 (yaml 은 잉여 키를 한 줄에 하나씩 보고한다).
		// 이어지는 줄을 들여쓰지 않으면 목록의 "- " 항목 경계가 무너져 어느
		// 파일의 원인인지 눈으로 못 따라간다.
		reason := strings.ReplaceAll(strings.TrimRight(fmt.Sprint(s.Reason), "\n"), "\n", "\n      ")
		fmt.Fprintf(w, "  - %s\n      %s\n", l.RelPath(s.Path), reason)
	}
}

// warnDroppedRelated 는 **대상이 없어서 빼 버린 related** 를 알린다.
//
// 조용히 빼면 최악이다 — 에이전트는 링크를 걸었다고 믿고, 사람은 옵시디언에서
// 백링크가 없는 이유를 영영 모른다. 그래서 빼는 것 자체보다 알리는 것이 이
// 기능의 핵심이다 (capture/relatedcheck.go).
//
// 제안이 있으면 같이 낸다. **제안은 교정이 아니다** — 맞는지는 사람이 판단한다.
func warnDroppedRelated(w io.Writer, dropped []capture.DroppedLink) {
	if len(dropped) == 0 {
		return
	}
	fmt.Fprintf(w, "경고: related %d건은 볼트에 그런 문서가 없어 빼놨다:\n", len(dropped))
	for _, d := range dropped {
		if d.Suggest != "" {
			fmt.Fprintf(w, "  - %s\n      혹시 이것인가: %s\n", d.Value, d.Suggest)
		} else {
			fmt.Fprintf(w, "  - %s\n", d.Value)
		}
	}
	fmt.Fprintln(w, "  맞는 이름을 찾았으면 prior review <stem> --related \"[[이름]]\" 으로 걸어라")
}

// warnVocabulary 는 방금 쓴 노트의 태그가 **회수에 새 낱말을 하나도 안 더할 때** 알린다.
//
// 회수는 `파일명 + summary + tags` 만 본다. 태그의 낱말이 이미 제목이나 요약에
// 있으면 그 태그를 달든 안 달든 걸리는 질의가 똑같다 — 적는 사람은 회수 어휘를
// 넓혔다고 믿는데 아무 일도 안 일어난다. **조용하다.**
//
// 실볼트 실측(2026-08-23): 태그 달린 결정 노트 278건 중 12건(4%)이 그 상태였고,
// 태그가 더하는 새 낱말은 중앙값 2개였다. 그 대가는
// 같은 날 재현됐다 — "웹소설을 AI로 여러 편 찍어내면 경쟁력이 있나" 는 관련 규칙을
// 찾는데, 같은 질문을 바꿔 말한 "작품을 많이 만들어서 승부하면 되나" 는 0건이었다.
//
// **막지 않는다.** 노트는 이미 저장됐고, 무엇이 좋은 태그인지는 사람이 정한다.
// 여기서 거절하면 기록이 귀찮아지고, 귀찮아지면 안 남긴다.
//
// 디스크에서 다시 읽는다 — 요청이 아니라 **실제로 쓰인 것**을 봐야 한다.
func warnVocabulary(w io.Writer, l *store.Layout, path string) {
	n, err := l.Read(path)
	if err != nil {
		return
	}
	fresh, dead := search.TagVocabulary(n)
	if len(dead) == 0 || len(fresh) > 0 {
		return
	}
	fmt.Fprintf(w, "경고: 태그 %v 는 제목·summary 에 이미 있는 낱말뿐이라 "+
		"**회수에 아무것도 더하지 않는다**.\n"+
		"  이 결정을 다시 찾을 상황을 서너 개 떠올리고 그때 쓸 **동의어·상위어**를 넣어라.\n"+
		"  고치려면 파일의 tags 를 손보면 된다 — 회수는 노트를 직접 읽는다.\n", dead)
}

// Run 은 조립이 끝난 루트를 실행한다. 에러는 호출자가 종료 코드로 옮긴다.
//
// **ctx 를 받는 이유는 prior watch 다.** ExecuteContext 가 아니라 Execute 를 쓰면
// cmd.Context() 가 context.Background() 라서 데몬의 ctx.Done() 이 영원히 오지 않고,
// Ctrl-C 가 정리 없이 프로세스를 죽인다. 락은 커널이 놓아 주지만 진행 중인 스캔은
// 중간에 끊긴다.
func Run(ctx context.Context, root *cobra.Command) error {
	if err := root.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("prior: %w", err)
	}
	return nil
}

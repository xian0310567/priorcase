package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newPathCmd 는 `prior path <stem>` 이다.
//
// **경로 해석을 CLI 안에 둔다.** 앱이 볼트 경로를 조립하기 시작하면 볼트 선택
// 규칙이 둘이 되고, 다중 볼트에서 그 어긋남은 앱이 엉뚱한 파일을 열거나 못
// 여는 것으로 나타난다. 앱은 이 출력을 그대로 OS 에 넘긴다.
//
// 출력은 **경로 한 줄뿐**이다. 장식이 섞이면 앱이 파일을 못 연다.
func newPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path <stem>",
		Short: "결정 노트의 절대 경로를 낸다",
		Long: "결정 노트 stem 을 받아 절대 경로를 한 줄로 낸다.\n\n" +
			"데스크탑 앱이 노트를 열 때 쓴다 — 앱이 볼트 경로를 조립하면\n" +
			"볼트 선택 규칙이 둘이 된다.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			stem := args[0]
			// **볼트를 stem 의 도메인에서 고른다.** cwd 의 볼트로 풀면 다른
			// 프로젝트의 노트를 못 열거나, 더 나쁘게는 엉뚱한 볼트에 있는 같은
			// 이름의 파일을 연다 — 열리기는 열리므로 조용하다.
			//
			// PrefixOf 가 빈 값을 주면 For 가 기본 볼트를 주는데, 그러면 규약에
			// 안 맞는 stem 이 기본 볼트에서 조립된다. ResolveStem 이 그 다음에
			// 같은 판정으로 거부하므로 여기서 미리 끊을 필요는 없다 —
			// 판정을 두 곳에 두지 않는다.
			ll, err := l.For(l.PrefixOf(stem))
			if err != nil {
				return err
			}
			p, err := ll.ResolveStem(stem)
			if err != nil {
				return err
			}
			// **있는지 확인한다.** ResolveStem 은 경로를 만들 뿐 파일을 보지
			// 않는다. 없는 노트에 그럴듯한 절대 경로를 주면 앱은 그것을 그대로
			// OS 에 넘기고, 사람은 아무 일도 안 일어나는 버튼을 누르게 된다 —
			// 원인이 "노트가 없다" 인지 "앱이 못 연다" 인지 구별할 수 없다.
			if _, serr := os.Stat(p); serr != nil {
				return fmt.Errorf("그런 결정 노트가 없다: %s", p)
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	}
	return cmd
}

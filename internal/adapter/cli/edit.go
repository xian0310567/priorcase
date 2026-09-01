package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ── 본문만 고친다 ──────────────────────────────────────────────────────
//
// # 왜 본문만인가
//
// 앱에서 결정을 자유롭게 고칠 수 있게 하기로 했다(2026-09-01 결정). 그런데
// **frontmatter 는 스키마다.** 10키가 회수·색인·번복 사슬을 전부 지탱하고,
// `prior doctor` 가 그것을 지키는 검사를 여럿 갖고 있다(스키마·링크·뒤집기 대칭).
//
// 손으로 고치다 깨지면 그 노트는 회수에서 통째로 빠지는데 **조용하다** — doctor 를
// 돌릴 때까지 아무도 모른다. 2026-08-22 에 실제로 그 종류의 사고가 있었다.
//
// 그래서 통로를 가른다: 본문은 여기, 메타(요약·결과·회고·번복)는 `prior review`.
// 사람은 "아무 글자나 고칠 수 있다" 를 얻으면서 스키마를 못 깬다.
//
// # 왜 바이트 그대로 보존하나
//
// 파싱해서 다시 쓰면 키 순서·따옴표·줄바꿈이 미묘하게 바뀐다. 볼트는 git 으로
// 오가므로 그 차이가 전부 diff 로 나오고, 그러면 **무엇이 진짜 바뀌었는지**가
// 안 보인다. 앞부분을 자르지 않고 그대로 이어 붙인다.

// splitFrontmatter 는 `---` 로 둘러싸인 머리와 나머지를 가른다.
//
// 머리는 **여는 줄부터 닫는 줄까지 원문 그대로** 준다 — 파싱하지 않는다.
func splitFrontmatter(raw []byte) (head, body []byte, err error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return nil, nil, errors.New("frontmatter 가 없다 — 결정 노트가 아니다")
	}
	// 여는 `---` 다음부터 닫는 `---` 줄을 찾는다.
	rest := s[strings.Index(s, "\n")+1:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, nil, errors.New("frontmatter 가 안 닫혔다")
	}
	// 닫는 줄의 끝까지가 머리다.
	after := rest[idx+1:] // "---..." 로 시작
	nl := strings.Index(after, "\n")
	if nl < 0 {
		return []byte(s), nil, nil // 머리뿐이고 본문이 없다
	}
	cut := len(s) - len(after) + nl + 1
	return []byte(s[:cut]), []byte(s[cut:]), nil
}

func newEditCmd() *cobra.Command {
	var bodyFile string
	cmd := &cobra.Command{
		Use:   "edit <stem>",
		Short: "결정의 본문을 바꾼다 (frontmatter 는 안 건드린다)",
		Long: "새 본문을 표준입력으로 받아 그 결정의 본문만 바꾼다.\n\n" +
			"frontmatter 는 **바이트 그대로 보존**된다 — 요약·결과·회고·번복을 고치려면\n" +
			"`prior review` 를 써라. 거기가 스키마라 자유 편집으로 열어 두면 깨진다.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			notes, l, _, err := allNotes(c)
			if err != nil {
				return err
			}
			var target *store.Note
			for i := range notes {
				if notes[i].Stem == args[0] {
					target = &notes[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("그런 결정이 없다: %s", args[0])
			}

			var next []byte
			if bodyFile != "" {
				next, err = os.ReadFile(bodyFile)
			} else {
				next, err = io.ReadAll(cmd.InOrStdin())
			}
			if err != nil {
				return fmt.Errorf("새 본문을 읽을 수 없다: %w", err)
			}
			// **빈 본문은 거부한다.** 파이프가 끊기거나 앱이 빈 문자열을 보내면
			// 결정문이 통째로 사라지는데, 그건 되돌릴 수 없는 종류의 사고다.
			// 정말 비우고 싶으면 파일을 직접 열어라.
			if len(strings.TrimSpace(string(next))) == 0 {
				return errors.New("새 본문이 비었다 — 실수로 지우는 것을 막는다")
			}

			raw, err := os.ReadFile(target.Path)
			if err != nil {
				return err
			}
			head, _, err := splitFrontmatter(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", target.Stem, err)
			}
			// 본문 앞에 빈 줄 하나를 보장한다 — frontmatter 바로 뒤에 글이 붙으면
			// 옵시디언과 이 파서가 다르게 읽는다.
			b := strings.TrimLeft(string(next), "\n")
			out := append(append([]byte{}, head...), '\n')
			out = append(out, []byte(b)...)
			if !strings.HasSuffix(b, "\n") {
				out = append(out, '\n')
			}
			if err := store.WriteFileAtomic(target.Path, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "고쳤다: %s\n", l.RelPath(target.Path))
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "새 본문을 이 파일에서 읽는다 (기본: 표준입력)")
	return cmd
}

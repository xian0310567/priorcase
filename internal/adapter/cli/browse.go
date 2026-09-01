package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ── 볼트를 읽는 통로 ────────────────────────────────────────────────────
//
// **앱은 `prior` 명령만 부른다.** 그 규약 때문에 앱이 볼트를 보여 주려면 CLI 가
// 먼저 있어야 한다 — 2026-08-14 결정문이 같은 자리에서 이미 그렇게 적었다
// ("앱만 고쳐서는 아무것도 못 바꾼다").
//
// 여기 셋은 **읽기 전용**이다. 쓰기는 `prior review`(메타)와 `prior edit`(본문)이
// 따로 진다 — 읽는 명령이 쓰기도 하면 앱의 실수 하나가 볼트를 고칠 수 있다.

// noteOut 은 목록 한 줄이다. **본문을 안 담는다** — 목록에 본문을 실으면 결정
// 560건에 1.7MB 가 되고, 목록 화면은 그것을 한 글자도 안 쓴다.
type noteOut struct {
	Stem    string   `json:"stem"`
	Path    string   `json:"path"`
	Rel     string   `json:"rel"`
	Vault   string   `json:"vault"`
	Domain  []string `json:"domain"`
	Date    string   `json:"date"`
	Status  string   `json:"status"`
	Outcome string   `json:"outcome"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// showOut 은 노트 하나 전체다. 여기서만 본문이 나온다.
type showOut struct {
	noteOut
	Body       string   `json:"body"`
	Supersedes []string `json:"supersedes"`
	Related    []string `json:"related"`
	Author     string   `json:"author"`
	// **속성을 다 낸다.** 앱이 옵시디언처럼 frontmatter 를 표로 보여 주는데,
	// 일부만 주면 그 화면이 "속성 목록" 이라고 말하면서 거짓말을 한다.
	Type           string   `json:"type"`
	SourceSession  string   `json:"source_session"`
	SummaryHistory []string `json:"summary_history"`
	// SupersededReason 은 무엇이 이 결정을 뒤집었는지다. 뒤집힌 노트를 읽는
	// 사람에게 가장 먼저 필요한 한 줄이라 따로 낸다.
	SupersededReason string `json:"superseded_reason"`
}

func toNoteOut(l *store.Layout, c *config.Config, n store.Note) noteOut {
	dom := n.Meta.Domain
	if dom == nil {
		dom = []string{}
	}
	tags := n.Meta.Tags
	if tags == nil {
		tags = []string{}
	}
	v := ""
	if len(dom) > 0 {
		if vv, err := c.VaultFor(dom[0]); err == nil {
			v = vv.Name
		}
	}
	return noteOut{
		Stem: n.Stem, Path: n.Path, Rel: l.RelPath(n.Path), Vault: v,
		Domain: dom, Date: n.Meta.Date, Status: n.Meta.Status,
		Outcome: n.Meta.Outcome, Summary: n.Meta.Summary, Tags: tags,
	}
}

// allNotes 는 **선언된 볼트 전부**의 결정을 모은다.
//
// 앱에는 cwd 가 없으므로 한 볼트만 보면 셸이 마지막으로 있던 자리의 볼트를 보게
// 된다 — `retro.AllDue` 가 같은 이유로 볼트를 전부 돈다(그 § 의 실측: 같은 설정에서
// cwd 만 바꿔 부르니 큐가 0건과 43건으로 갈렸다).
func allNotes(c *config.Config) ([]store.Note, *store.Layout, []store.SkippedNote, error) {
	var all []store.Note
	var skipped []store.SkippedNote
	def := store.NewLayout(c)
	seen := map[string]bool{}
	for _, v := range c.Vaults {
		l := store.NewLayoutFor(c, v)
		notes, sk, err := l.List()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("볼트 %s: %w", v.Name, err)
		}
		for _, n := range notes {
			if seen[n.Path] {
				continue // 볼트 경로가 겹치면 같은 노트를 두 번 센다
			}
			seen[n.Path] = true
			all = append(all, n)
		}
		skipped = append(skipped, sk...)
	}
	return all, def, skipped, nil
}

func newListCmd() *cobra.Command {
	var asJSON bool
	var domain string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "볼트의 결정을 최근 것부터 낸다",
		Long: "선언된 볼트를 전부 훑어 결정 노트를 날짜 역순으로 낸다.\n\n" +
			"본문은 안 담는다 — 본문은 `prior show <stem>` 이다.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			notes, l, skipped, err := allNotes(c)
			if err != nil {
				return err
			}
			out := make([]noteOut, 0, len(notes))
			for _, n := range notes {
				if domain != "" && !containsFold(n.Meta.Domain, domain) {
					continue
				}
				out = append(out, toNoteOut(l, c, n))
			}
			// **최근 것이 먼저다.** 결정은 시간이 지나면 뒤집히므로 새것이 먼저
			// 보여야 하고, 사람이 찾는 것도 대개 최근이다.
			sort.Slice(out, func(i, j int) bool {
				if out[i].Date != out[j].Date {
					return out[i].Date > out[j].Date
				}
				return out[i].Stem < out[j].Stem
			})
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			for _, s := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 읽지 못해 빠졌다: %s (%v)\n", s.Path, s.Reason)
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
			}
			for _, n := range out {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %-10s %s\n", n.Date, strings.Join(n.Domain, ","), n.Summary)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON 으로 낸다")
	cmd.Flags().StringVar(&domain, "domain", "", "이 도메인의 것만 낸다")
	cmd.Flags().IntVar(&limit, "limit", 0, "최대 건수 (0 이면 전부)")
	return cmd
}

func newShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:           "show <stem>",
		Short:         "결정 하나를 본문까지 낸다",
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
			for _, n := range notes {
				if n.Stem != args[0] {
					continue
				}
				o := showOut{
					noteOut: toNoteOut(l, c, n), Body: string(n.Body),
					Supersedes: []string(n.Meta.Supersedes), Related: n.Meta.Related,
					Author: n.Meta.Author, SupersededReason: n.Meta.SupersededReason,
					Type: n.Meta.Type, SourceSession: n.Meta.SourceSession,
					SummaryHistory: n.Meta.SummaryHistory,
				}
				if o.Supersedes == nil {
					o.Supersedes = []string{}
				}
				if o.Related == nil {
					o.Related = []string{}
				}
				if o.SummaryHistory == nil {
					o.SummaryHistory = []string{}
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(o)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n%s\n", o.Summary, o.Body)
				return nil
			}
			// **없는 stem 을 조용히 넘기지 않는다.** 앱이 오타 난 이름을 보내면
			// 빈 화면이 뜨는데, 사람은 그것을 "결정이 비어 있다" 로 읽는다.
			return fmt.Errorf("그런 결정이 없다: %s", args[0])
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON 으로 낸다")
	return cmd
}

// searchOut 은 회수 결과 한 줄이다. 점수를 같이 낸다 — 왜 이 순서인지가 보여야 한다.
type searchOut struct {
	noteOut
	Score int `json:"score"`
}

// recallJSON 은 `prior recall --format json` 의 몸통이다.
//
// **이 앱이 옵시디언과 갈리는 자리다.** 파일 탐색기는 이름으로 찾지만 여기는
// 순위를 매긴다 — 2026-08-31 에 고친 그 랭킹이다.
func recallJSON(w io.Writer, l *store.Layout, c *config.Config, hits []search.Hit) error {
	out := make([]searchOut, 0, len(hits))
	for _, h := range hits {
		out = append(out, searchOut{noteOut: toNoteOut(l, c, h.Note), Score: h.Score})
	}
	return json.NewEncoder(w).Encode(out)
}

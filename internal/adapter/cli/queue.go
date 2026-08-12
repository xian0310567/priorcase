package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/health"
	"github.com/xian0310567/priorcase/internal/core/retro"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// Queue 는 감독 표면이 한 번에 받아 가는 스냅샷이다.
//
// **명령 하나로 주는 이유는 일관성이다.** 네 번 따로 부르면 그 사이에 훅이 돌아
// pending 이 늘거나 승격이 노트를 만들 수 있고, 그러면 화면 위에서 서로 어긋난 세
// 큐가 나란히 보인다. 사람은 그걸 버그로 읽는다.
//
// 그리고 유지해야 할 계약 표면이 하나로 줄어든다. 사람이 읽는 출력은 문구를 자유롭게
// 고칠 수 있어야 하는데, 앱이 그걸 파싱하면 문구를 못 고치게 된다.
type Queue struct {
	// Confirm 은 "여기 결정이 있는 것 같다" 고 표시된 구간이다. 사람이 맞나 아니나만 답한다.
	Confirm []QueuePending `json:"confirm"`
	// Review 는 판별기가 스스로 만든 노트다. **사람이 검증해야 한다** — 실제로
	// 판별기가 만든 첫 노트에 없는 어원을 지어낸 전력이 있다.
	Review []QueueReview `json:"review"`
	// Retro 는 결과를 물어볼 때가 된 결정이다.
	Retro []retro.Item `json:"retro"`
	// Health 는 지금 시스템이 제대로 도는가다. 큐가 셋 다 비었을 때 앱이 보여 줄 것이
	// 이것뿐이라, 비어 있어도 반드시 채운다.
	Health []QueueCheck `json:"health"`
	// Warnings 는 큐를 만들다 생긴 문제다. **비어 있지 않으면 큐가 불완전하다.**
	// 조용히 짧은 목록을 주면 앱이 "할 일이 없다" 로 그린다.
	Warnings []string `json:"warnings,omitempty"`
}

// QueuePending 은 확인 큐 한 줄이다. daemon.Pending 을 그대로 내보내지 않는다 —
// 그건 내부 상태 구조라 필드가 바뀌면 앱이 깨진다.
type QueuePending struct {
	ID      string   `json:"id"`
	Domain  string   `json:"domain"`
	When    string   `json:"when"`
	Signals []string `json:"signals"`
	Excerpt string   `json:"excerpt"`
}

// QueueReview 는 검토 큐 한 줄이다.
//
// daemon.Promotion 을 그대로 내보내지 않는다 — 그건 원장의 내부 레코드라 진단용
// 필드(reason·err)까지 달려 있고, 그 형태가 바뀌면 앱이 깨진다. 검토 큐에 필요한
// 것은 "판별기가 무엇을 만들었고 어느 구간에서 나왔나" 뿐이다.
type QueueReview struct {
	ID     string `json:"id"` // 어느 구간에서 나왔나
	Domain string `json:"domain"`
	At     string `json:"at"`   // RFC3339
	Path   string `json:"path"` // 만들어진 노트 (볼트 상대 경로)
	// Excerpt 는 판별기가 본 것이다. **이 화면의 존재 이유다** — 노트를 이것과
	// 나란히 놓고 사람이 대조한다. 판별기는 LLM 이라 근거를 지어낼 수 있고,
	// 지시문의 제약은 완화이지 보장이 아니다.
	//
	// 옛 원장 줄에는 없을 수 있다 (2026-08-12 이전 기록). 그때는 빈 문자열이고,
	// 앱은 "대조할 발췌가 없다" 고 말해야 한다 — 없는 것을 조용히 안 보여 주면
	// 사람은 노트만 보고 맞다고 누른다.
	Excerpt string `json:"excerpt"`
}

// QueueCheck 는 상태 검사 한 줄이다.
//
// health.Check 를 그대로 내보내지 않는다. 그 구조체에는 json 태그가 없어서 Go
// 필드명(Name·Level·Detail·Fix)이 그대로 나가고, 무엇보다 **Level 이 정수로 나간다** —
// 앱이 `0 == 정상` 을 외워야 하고, 상수 순서가 바뀌면 조용히 뒤집힌다.
type QueueCheck struct {
	Name   string `json:"name"`
	Level  string `json:"level"` // ok | warn | fail
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// levelName 은 등급을 스스로 설명하는 문자열로 바꾼다.
//
// 모르는 값이 오면 "unknown" 이다. 정상으로 뭉개지 않는다 — 새 등급이 생겼는데
// 앱이 그걸 초록불로 그리면, 하필 알아야 할 상태가 안 보인다.
func levelName(l health.Level) string {
	switch l {
	case health.OK:
		return "ok"
	case health.Warn:
		return "warn"
	case health.Fail:
		return "fail"
	}
	return "unknown"
}

func newQueueCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "사람이 확인해야 할 것을 한 번에 준다 (감독 표면용)",
		Long: "확인 큐·자동 기록 검토·회고 큐·상태를 한 스냅샷으로 준다.\n\n" +
			"데스크탑 앱이 쓰는 명령이다. 네 번 따로 부르면 그 사이에 상태가 바뀌어 " +
			"서로 어긋난 큐가 나란히 보이므로 한 번에 준다.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			q := Queue{
				Confirm: []QueuePending{},
				Review:  []QueueReview{},
				Retro:   []retro.Item{},
				Health:  []QueueCheck{},
			}

			sd, err := daemon.DefaultDir()
			if err != nil {
				q.Warnings = append(q.Warnings, "상태 디렉토리를 찾을 수 없다: "+err.Error())
			} else {
				items, perr := daemon.ReadPending(sd)
				if perr != nil {
					q.Warnings = append(q.Warnings, "미확인 구간을 읽지 못했다: "+perr.Error())
				}
				for _, p := range items {
					// **빈 배열은 [] 여야 한다. null 이면 앱이 순회하다 터진다.**
					//
					// 판별기가 있으면 시그널 필터를 건너뛰므로(D9) Signals 가 비는 것이
					// 정상이다. 즉 이건 예외가 아니라 흔한 경우다 — 실측으로 27건 중
					// 2건이 그랬고, jq 가 "Cannot iterate over null" 로 터졌다.
					sig := p.Signals
					if sig == nil {
						sig = []string{}
					}
					q.Confirm = append(q.Confirm, QueuePending{
						ID: p.ID(), Domain: p.Domain, When: p.When(),
						Signals: sig, Excerpt: p.Excerpt,
					})
				}
				// **판별기가 스스로 만든 것만 검토 대상이다.** 기록 안 함·실패는
				// 사람이 할 일이 없다 — 그건 doctor 가 볼 진단이다.
				recs, rerr := daemon.ReadPromotions(sd, time.Time{})
				if rerr != nil {
					q.Warnings = append(q.Warnings, "승격 원장을 읽지 못했다: "+rerr.Error())
				}
				for _, r := range recs {
					if r.Recorded {
						q.Review = append(q.Review, QueueReview{
							ID: r.ID, Domain: r.Domain,
							At: r.At.Format(time.RFC3339), Path: r.Path,
							Excerpt: r.Excerpt,
						})
					}
				}
			}

			due, skipped, derr := retro.Due(l, c)
			if derr != nil {
				q.Warnings = append(q.Warnings, "회고 큐를 만들지 못했다: "+derr.Error())
			}
			q.Retro = append(q.Retro, due...)
			for _, s := range skipped {
				q.Warnings = append(q.Warnings,
					fmt.Sprintf("읽지 못한 노트가 있어 회고 큐가 불완전하다: %s", l.RelPath(s.Path)))
			}

			for _, ck := range health.Vault(c, l).Checks {
				q.Health = append(q.Health, QueueCheck{
					Name: ck.Name, Level: levelName(ck.Level),
					Detail: ck.Detail, Fix: ck.Fix,
				})
			}

			if !asJSON {
				return fmt.Errorf("이 명령은 지금 --json 으로만 쓴다 (사람이 읽을 것은 prior pending·doctor 다)")
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(q)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON 으로 출력한다 (지금은 필수)")
	return cmd
}

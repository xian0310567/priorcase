package split

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// 도메인을 **다른 볼트로 통째로** 옮긴다.
//
// # 왜 Build/Apply 와 따로인가
//
// 그쪽은 폴백에 쌓인 노트에서 **새 도메인을 뽑아내는** 일이라 파일명과 frontmatter 를
// 다시 쓴다. 볼트 간 이동은 그럴 필요가 없다 — 도메인 이름이 그대로라 파일명도,
// `domain:` 도, 위키링크(옵시디언은 파일명으로 푼다)도 안 바뀐다. **폴더만 옮긴다.**
//
// 그래서 이 코드는 짧다. 짧은 것이 맞다 — 이름을 다시 쓰지 않는다는 것은
// 되돌리기도 쉽다는 뜻이다.
//
// # 고치려는 고장
//
// 2026-09-01: 앱에서 `editup` 을 회사 볼트로 옮겼더니 **설정만 바뀌고 파일 63건은
// 옛 볼트에 남았다.** 회수는 새 볼트의 빈 폴더를 보므로 그 프로젝트의 결정이 통째로
// 사라졌고, 화면에는 "결정 0건" 이라고만 떴다 — 사람이 알아챌 방법이 없었다.
// **설정을 바꾸는 길은 있는데 파일을 옮기는 길이 없었던 것**이 원인이다.
//
// # 볼트를 넘는 위키링크는 깨진다
//
// 옵시디언은 볼트 안에서만 링크를 푼다. 도메인을 다른 볼트로 옮기면 그 노트를
// 가리키던 다른 볼트의 링크는 대상을 잃는다. **그건 이 명령의 결함이 아니라 볼트를
// 가르는 것의 본질적 비용**이고, 코드주권 결정문(2026-08-31)이 "대가는 규칙·동의어가
// 볼트를 못 넘는 것" 으로 이미 적어 뒀다. 여기서는 그 사실을 계획에 담아 보여만 준다.

// MovePlan 은 볼트 간 이동 계획이다. 세우기만 하고 파일은 안 건드린다.
type MovePlan struct {
	Prefix string
	From   string // 원본 폴더의 절대 경로
	To     string // 목적지 폴더의 절대 경로
	Moves  []Move
	// Orphans 는 이 이동으로 **대상을 잃는 위키링크**다. 옵시디언은 볼트 안에서만
	// 링크를 풀기 때문이다. 조용히 넘기지 않는다 — 볼트를 가르는 대가를 사람이
	// 알고 치러야 한다.
	Orphans []string
}

// PlanMove 는 계획을 세운다.
func PlanMove(c *config.Config, src, dst *store.Layout, prefix string) (*MovePlan, error) {
	if src.Vault() == dst.Vault() {
		// 조용히 성공하면 사람은 뭔가 됐다고 믿는다.
		return nil, fmt.Errorf("같은 볼트다 (%s) — 옮길 곳이 아니다", src.Vault())
	}
	folder, ok := c.FolderFor(prefix)
	if !ok {
		return nil, fmt.Errorf("모르는 도메인 접두어: %q", prefix)
	}
	from := filepath.Join(src.Vault(), folder)
	to := filepath.Join(dst.Vault(), folder)

	entries, err := os.ReadDir(from)
	if err != nil {
		return nil, fmt.Errorf("옮길 폴더를 읽을 수 없다 (%s): %w", from, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s 에 옮길 것이 없다", from)
	}
	// **목적지에 이미 있으면 거부한다.** 덮어쓰면 남의 결정이 조용히 사라진다.
	// 병합은 여기서 안 한다 — 같은 파일명이 양쪽에 있을 때 무엇이 정본인지
	// 이 코드가 알 방법이 없다.
	if _, serr := os.Stat(to); serr == nil {
		return nil, fmt.Errorf("목적지에 이미 %s 가 있다 — 손으로 합쳐라", to)
	}

	p := &MovePlan{Prefix: prefix, From: from, To: to}
	for _, e := range entries {
		p.Moves = append(p.Moves, Move{
			From: filepath.Join(from, e.Name()),
			To:   filepath.Join(to, e.Name()),
		})
	}
	return p, nil
}

// ApplyMove 는 계획을 실행한다. 폴더를 통째로 옮긴다.
//
// **os.Rename 을 먼저 시도한다.** 같은 파일시스템이면 원자적이라 중간에 끊겨도
// 반쯤 옮겨진 상태가 안 남는다. 볼트가 다른 디스크에 있으면 실패하므로 그때만
// 복사 후 삭제로 내려간다.
func ApplyMove(p *MovePlan) error {
	if p == nil || len(p.Moves) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.To), 0o755); err != nil {
		return fmt.Errorf("목적지를 만들 수 없다: %w", err)
	}
	if err := os.Rename(p.From, p.To); err == nil {
		return nil
	}
	// 파일시스템이 다르다 — 하나씩 옮긴다.
	if err := os.MkdirAll(p.To, 0o755); err != nil {
		return fmt.Errorf("목적지를 만들 수 없다: %w", err)
	}
	for _, m := range p.Moves {
		if err := moveOne(m.From, m.To); err != nil {
			return err
		}
	}
	// **원본 폴더를 지운다.** 남으면 같은 결정이 두 볼트에 있고, 회수가 둘 다
	// 내면 어느 쪽이 정본인지 알 수 없다.
	if err := os.RemoveAll(p.From); err != nil {
		return fmt.Errorf("옮긴 뒤 원본을 지우지 못했다 (%s): %w", p.From, err)
	}
	return nil
}

func moveOne(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		// 디렉토리일 수 있다 — 통째로 옮긴다.
		if st, serr := os.Stat(from); serr == nil && st.IsDir() {
			return copyDir(from, to)
		}
		return fmt.Errorf("읽을 수 없다 (%s): %w", from, err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		return fmt.Errorf("쓸 수 없다 (%s): %w", to, err)
	}
	return nil
}

func copyDir(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := moveOne(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// FindSource 는 그 도메인의 파일이 **실제로 있는** 볼트를 찾는다.
//
// # 왜 설정을 안 보나
//
// 설정상 그 도메인의 볼트는 `config.VaultFor` 가 알려 준다. 그런데 이 명령이
// 고치려는 상태가 정확히 **설정과 파일이 어긋난 것**이다 — 앱에서 볼트를 바꾸면
// 설정이 먼저 바뀌고 파일은 옛 볼트에 남는다. 설정으로 원본을 찾으면 목적지와
// 같아져서 "옮길 곳이 아니다" 로 거부하고, 즉 고치려던 바로 그 상태에서만 안 돈다.
//
// 그래서 디스크를 본다. 목적지는 후보에서 뺀다.
func FindSource(c *config.Config, prefix, dstVault string) (string, error) {
	folder, ok := c.FolderFor(prefix)
	if !ok {
		return "", fmt.Errorf("모르는 도메인 접두어: %q", prefix)
	}
	var found []string
	for _, v := range c.Vaults {
		if v.Path == dstVault {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(v.Path, folder))
		if err != nil || len(entries) == 0 {
			continue
		}
		found = append(found, v.Path)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%s 폴더를 가진 볼트가 없다 — 옮길 것이 없다", folder)
	case 1:
		return found[0], nil
	default:
		// **여러 곳에 있으면 손으로 정해야 한다.** 어느 쪽이 정본인지 이 코드가
		// 알 방법이 없고, 잘못 고르면 남의 결정을 덮어쓴다.
		return "", fmt.Errorf("%s 가 여러 볼트에 있다 (%v) — 손으로 합쳐라", folder, found)
	}
}

// Package health 는 "지금 casebook 이 제대로 돌고 있는가" 를 검사한다.
//
// **이 패키지의 존재 이유는 조용한 무동작이다.** 이 시스템의 부품은 전부 실패해도
// 대화를 막지 않도록 설계됐다 — 훅은 무슨 일이 있어도 exit 0 이고, 회수는 못 찾으면
// 아무것도 안 내고, 데몬은 백그라운드다. 그 설계의 대가로 **고장이 정상과 구별되지
// 않는다.** 여기가 그걸 구별하는 유일한 자리다.
//
// core 만으로 할 수 있는 검사를 모은다. 훅 배선·데몬 상태처럼 호스트나 프로세스를
// 알아야 하는 검사는 어댑터가 자기 것을 덧붙인다 (§4.1).
package health

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/store"
)

// Level 은 검사 결과의 심각도다.
type Level int

const (
	OK   Level = iota // 정상
	Warn              // 동작하지만 손해가 있다
	Fail              // 동작하지 않는다
)

func (l Level) Mark() string {
	switch l {
	case OK:
		return "✓"
	case Warn:
		return "⚠"
	}
	return "✗"
}

// Check 는 검사 하나의 결과다.
//
// Fix 를 따로 두는 이유: 진단만 하고 무엇을 하라는 말이 없으면 사용자는 그 경고를
// 무시하는 법을 배운다. 고칠 방법을 모르면 고칠 수 없다.
type Check struct {
	Name   string
	Level  Level
	Detail string
	Fix    string
}

// Report 는 검사 묶음이다.
type Report struct{ Checks []Check }

func (r *Report) add(name string, lv Level, detail, fix string) {
	r.Checks = append(r.Checks, Check{Name: name, Level: lv, Detail: detail, Fix: fix})
}

// Worst 는 가장 나쁜 등급이다. 호출자가 종료 코드로 옮긴다.
func (r *Report) Worst() Level {
	worst := OK
	for _, c := range r.Checks {
		if c.Level > worst {
			worst = c.Level
		}
	}
	return worst
}

// Vault 는 볼트와 설정에 대한 검사를 돈다.
func Vault(c *config.Config, l *store.Layout) *Report {
	r := &Report{}
	checkVaultDir(r, c)
	checkDomainFolders(r, l)
	checkUndeclared(r, l)
	notes := checkNotes(r, l)
	checkIndex(r, l, notes)
	return r
}

func checkVaultDir(r *Report, c *config.Config) {
	fi, err := os.Stat(c.Vault)
	switch {
	case err != nil:
		r.add("볼트", Fail, fmt.Sprintf("%s 에 접근할 수 없다 (%v)", c.Vault, err),
			"설정의 vault 경로를 확인하라")
	case !fi.IsDir():
		r.add("볼트", Fail, c.Vault+" 가 디렉토리가 아니다", "설정의 vault 경로를 확인하라")
	default:
		r.add("볼트", OK, c.Vault, "")
	}
}

// checkDomainFolders 는 선언된 도메인 중 폴더가 아직 없는 것을 센다.
//
// **Fail 이 아니다.** 폴더는 그 도메인의 첫 결정을 쓸 때 만들어지므로, 결정이 아직
// 없는 프로젝트는 폴더가 없는 게 정상이다. 그래도 세어서 보여 주는 이유는 오타를
// 잡기 위해서다 — folder 이름을 잘못 적으면 영원히 "아직 없음" 으로 남는다.
func checkDomainFolders(r *Report, l *store.Layout) {
	dirs := l.DecisionDirs()
	var missing []string
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			missing = append(missing, l.RelPath(d))
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		r.add("도메인 폴더", OK, fmt.Sprintf("%d개 전부 있다", len(dirs)), "")
		return
	}
	r.add("도메인 폴더", OK,
		fmt.Sprintf("%d개 중 %d개는 아직 없다 %v — 첫 결정을 쓸 때 만들어진다",
			len(dirs), len(missing), missing),
		"이름을 잘못 적은 게 아닌지 설정의 folder 값을 확인하라")
}

// checkUndeclared 가 이 패키지의 핵심이다.
//
// 볼트에 결정 폴더가 있는데 설정에 없으면 그 프로젝트의 결정이 **전부** 색인과
// 회수에서 빠진다. 그런데 색인은 정상 생성되고 회수도 에러를 안 낸다 — 없는 것처럼 군다.
func checkUndeclared(r *Report, l *store.Layout) {
	dirs, err := l.UndeclaredDecisionDirs()
	if err != nil {
		r.add("미선언 도메인", Warn, "확인할 수 없다: "+err.Error(), "")
		return
	}
	if len(dirs) == 0 {
		r.add("미선언 도메인", OK, "없다", "")
		return
	}
	var rel []string
	for _, d := range dirs {
		rel = append(rel, l.RelPath(d))
	}
	r.add("미선언 도메인", Fail,
		fmt.Sprintf("%v — 이 폴더의 결정은 색인·회수에서 통째로 빠진다", rel),
		"설정에 [[domain]] 블록을 추가하고 cb index 를 다시 돌려라")
}

func checkNotes(r *Report, l *store.Layout) []store.Note {
	notes, skipped, err := l.List()
	if err != nil {
		r.add("결정 노트", Fail, "읽을 수 없다: "+err.Error(), "볼트 경로와 권한을 확인하라")
		return nil
	}
	if len(skipped) > 0 {
		var rel []string
		for _, s := range skipped {
			rel = append(rel, l.RelPath(s.Path))
		}
		r.add("결정 노트", Fail,
			fmt.Sprintf("%d건 중 %d건을 읽지 못했다 %v", len(notes)+len(skipped), len(skipped), rel),
			"frontmatter 를 정본 10키로 고쳐라. cb index 가 이유를 알려준다")
		return notes
	}
	r.add("결정 노트", OK, fmt.Sprintf("%d건 전부 읽힌다", len(notes)), "")
	return notes
}

// checkIndex 는 디스크의 색인이 지금 볼트와 맞는지 본다.
//
// **색인이 결정적이라서 가능한 검사다** — 같은 볼트면 같은 바이트가 나오므로,
// 다시 만들어 보고 비교하면 낡았는지 알 수 있다. 생성 시각을 안 넣기로 한 결정이
// 여기서 값을 한다.
func checkIndex(r *Report, l *store.Layout, notes []store.Note) {
	want, _, err := index.Build(l)
	if err != nil {
		r.add("색인", Fail, "생성할 수 없다: "+err.Error(), "")
		return
	}
	got, err := os.ReadFile(l.IndexPath())
	if os.IsNotExist(err) {
		r.add("색인", Fail, l.RelPath(l.IndexPath())+" 가 없다", "cb index")
		return
	}
	if err != nil {
		r.add("색인", Fail, "읽을 수 없다: "+err.Error(), "")
		return
	}
	if !bytes.Equal(want, got) {
		r.add("색인", Warn, "볼트와 어긋난다 — 노트를 손으로 고쳤거나 색인을 안 돌렸다", "cb index")
		return
	}
	r.add("색인", OK, fmt.Sprintf("%d건과 일치한다", len(notes)), "")
}

package split

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// wikiLink 는 `[[대상]]`·`[[대상|별칭]]`·`[[대상#절]]` 을 잡는다.
//
// **본문과 frontmatter 를 같은 정규식으로 훑는다.** frontmatter 의 related 도
// 보통 `"[[stem]]"` 문자열이라 모양이 같고, 두 벌로 나누면 한쪽만 고치는 사고가 난다.
var wikiLink = regexp.MustCompile(`\[\[([^\]\[|#]+)((?:[|#][^\]\[]*)?)\]\]`)

// bareLinkLine 은 `related:`·`supersedes:` 줄이다. **대괄호 없이 쓰인 값**을 잡으려고 있다.
//
// # 왜 필요한가 (2026-09-02 실볼트)
//
// 위 주석은 "related 도 `[[stem]]` 이라 모양이 같다" 고 전제했는데 **볼트 전체에
// 성립하지 않는다.** 이렇게 쓰인 노트가 실제로 있다:
//
//	related: ["common-결정-EWS는-…", "common-결정-코스봇-인프라는-…"]
//
// `store.parseLink` 가 맨 값도 링크로 읽어 주기 때문에(모양이 나빠도 버리지 않는다)
// 이런 노트가 생기고, doctor 는 그것을 링크로 센다. 그런데 개명 때는 대괄호만 고쳐서
// **옮기는 행위가 링크를 깨뜨렸다** — `젠틀파이` 9건을 옮긴 뒤 끊어진 링크 4개가 났다.
var bareLinkLine = regexp.MustCompile(`(?m)^(related|supersedes):.*$`)

// quotedValue 는 그 줄 안의 `"..."` 값 하나다.
var quotedValue = regexp.MustCompile(`"([^"]*)"`)

// skipDirs 는 볼트 안에서 안 훑는 자리다 (health/bodylinks.go 와 같다).
var skipDirs = map[string]bool{
	".obsidian": true, ".git": true, ".trash": true, "_derived": true,
}

// scanRelinks 는 옮겨질 stem 을 가리키는 문서를 **볼트 전부에서** 찾는다.
func scanRelinks(roots []string, renames map[string]string) []Relink {
	var out []Relink
	seen := map[string]bool{}
	for _, root := range roots {
		out = append(out, scanRelinksIn(root, renames, seen)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func scanRelinksIn(vault string, renames map[string]string, seen map[string]bool) []Relink {
	var out []Relink
	_ = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 못 읽는 자리는 건너뛴다
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if seen[p] {
			return nil // 볼트가 겹쳐 있으면 같은 파일을 두 번 고칠 뻔한다
		}
		if n := countLinks(string(b), renames); n > 0 {
			seen[p] = true
			out = append(out, Relink{Path: p, Count: n})
		}
		return nil
	})
	return out
}

// countLinks 는 고칠 링크 수를 센다.
//
// **rewriteLinks 와 같은 것을 세야 한다.** 여기서 안 세면 그 문서는 Relinks 에
// 안 들어가고, 그러면 rewriteLinks 가 아예 안 불린다 — 고칠 수 있는데 손도 안 대는
// 상태가 된다. 2026-09-02 에 대괄호 없는 값을 rewriteLinks 에만 넣고 여기를 안
// 고쳐서 그 판이 났다.
func countLinks(s string, renames map[string]string) int {
	n := 0
	for _, m := range wikiLink.FindAllStringSubmatch(s, -1) {
		if _, ok := renames[strings.TrimSpace(m[1])]; ok {
			n++
		}
	}
	for _, line := range bareLinkLine.FindAllString(s, -1) {
		for _, q := range quotedValue.FindAllStringSubmatch(line, -1) {
			v := strings.TrimSpace(q[1])
			if strings.Contains(v, "[[") {
				continue // 위에서 이미 셌다
			}
			if _, ok := renames[v]; ok {
				n++
			}
		}
	}
	return n
}

// rewriteLinks 는 옮겨진 stem 을 가리키는 링크를 고친다. 별칭·절 앵커는 보존한다.
func rewriteLinks(s string, renames map[string]string) string {
	out := wikiLink.ReplaceAllStringFunc(s, func(raw string) string {
		m := wikiLink.FindStringSubmatch(raw)
		if m == nil {
			return raw
		}
		to, ok := renames[strings.TrimSpace(m[1])]
		if !ok {
			return raw
		}
		return "[[" + to + m[2] + "]]"
	})
	return rewriteBareLinks(out, renames)
}

// rewriteBareLinks 는 `related:`·`supersedes:` 줄의 **대괄호 없는** 값을 고친다
// (bareLinkLine 의 §). 대괄호가 있는 값은 위에서 이미 고쳤으므로 여기서는 안 걸린다.
func rewriteBareLinks(s string, renames map[string]string) string {
	return bareLinkLine.ReplaceAllStringFunc(s, func(line string) string {
		return quotedValue.ReplaceAllStringFunc(line, func(q string) string {
			v := strings.TrimSpace(strings.Trim(q, `"`))
			// 대괄호가 붙은 값은 wikiLink 가 이미 처리했다. 두 번 고치지 않는다.
			if strings.Contains(v, "[[") {
				return q
			}
			to, ok := renames[v]
			if !ok {
				return q
			}
			return `"` + to + `"`
		})
	})
}

// domainLine 은 frontmatter 의 `domain: [...]` 줄이다.
var domainLine = regexp.MustCompile(`(?m)^domain:.*$`)

// Apply 는 계획을 실행한다.
//
// # 순서가 중요하다
//
// ① 폴더를 만들고 ② 노트를 옮기면서 그 노트의 `domain:` 을 새 접두어로 바꾸고
// ③ 마지막에 링크를 고친다. 링크를 먼저 고치면 아직 없는 파일을 가리키는
// 구간이 생기고, 중간에 실패하면 볼트가 그 상태로 남는다.
//
// **되돌리기는 제공하지 않는다.** 볼트는 git 아래 있고, 그것이 이 볼트의 되돌리기다
// — 여기서 사본을 따로 만들면 두 벌의 되돌리기가 서로를 모르게 된다. 부르는 쪽이
// 실행 전에 그 사실을 말해야 한다.
func Apply(p *Plan) error {
	if len(p.Moves) == 0 {
		return nil
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return fmt.Errorf("결정 폴더를 만들 수 없다: %w", err)
	}
	renames := make(map[string]string, len(p.Moves))
	for _, m := range p.Moves {
		renames[m.OldStem] = m.NewStem
	}
	for _, m := range p.Moves {
		b, err := os.ReadFile(m.From)
		if err != nil {
			return fmt.Errorf("%s 를 읽을 수 없다: %w", m.OldStem, err)
		}
		// **frontmatter 의 domain 도 같이 고친다.** 파일만 옮기면 회수가 그 노트를
		// 옛 도메인 것으로 계속 읽는다 — 폴더가 아니라 domain 배열이 회수 경로다
		// ([[common-결정-도메인배열이회수경로다-폴더와분리]]).
		out := domainLine.ReplaceAllString(string(b), "domain: ["+p.Prefix+"]")
		out = rewriteLinks(out, renames)
		if err := store.WriteFileAtomic(m.To, []byte(out), 0o644); err != nil {
			return fmt.Errorf("%s 를 쓸 수 없다: %w", m.NewStem, err)
		}
		if err := os.Remove(m.From); err != nil {
			return fmt.Errorf("%s 를 지울 수 없다 (새 파일은 이미 만들어졌다): %w", m.OldStem, err)
		}
	}
	for _, r := range p.Relinks {
		b, err := os.ReadFile(r.Path)
		if err != nil {
			continue // 옮기는 중에 사라진 파일 — 링크 고치기는 곁다리라 죽지 않는다
		}
		out := rewriteLinks(string(b), renames)
		if out == string(b) {
			continue
		}
		if err := store.WriteFileAtomic(r.Path, []byte(out), 0o644); err != nil {
			return fmt.Errorf("%s 의 링크를 고칠 수 없다: %w", filepath.Base(r.Path), err)
		}
	}
	return nil
}

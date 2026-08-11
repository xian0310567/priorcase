package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// RepoFor 는 dir 이 속한 git 저장소의 `owner/repo` 를 준다. 아니면 빈 문자열.
//
// **왜 필요한가.** 도메인을 경로로만 판정하면 팀에서 못 쓴다. 설정에 적는
// `paths` 는 절대 경로인데, 같은 저장소를 사람마다 다른 자리에 체크아웃한다
// (`~/project/priorcase` vs `~/dev/work/priorcase`). 그러면 새 팀원이
// `npm i -g priorcase` 직후 **설정 파일을 손으로 고쳐야** 하고, 그게 온보딩의
// 첫 경험이 된다. `owner/repo` 는 누구 기계에서든 같다.
//
// **왜 `git` 을 부르지 않고 파일을 읽는가.** 이 판정은 `user-prompt-submit` 훅에서
// **매 프롬프트마다** 돈다 (internal/adapter/hook/recall.go). 서브프로세스를 띄우면
// 그 지연이 사용자의 모든 프롬프트에 얹힌다. 그리고 git 이 깔려 있지 않아도
// 동작해야 한다 — 바이너리 하나로 끝난다는 것이 이 프로젝트의 약속이다.
func RepoFor(dir string) string {
	gitDir := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(gitDir, "config"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return NormalizeRemote(originURL(f))
}

// findGitDir 은 dir 에서 위로 올라가며 .git 을 찾는다.
//
// `.git` 이 **파일일 수도 있다** — worktree 와 submodule 이 그렇고, 그때 내용은
// `gitdir: <경로>` 다. 그 경우를 안 다루면 worktree 에서 도메인이 통째로 안 잡힌다.
func findGitDir(dir string) string {
	for d := filepath.Clean(dir); ; {
		p := filepath.Join(d, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return p
			}
			// .git 파일 — worktree/submodule
			b, err := os.ReadFile(p)
			if err != nil {
				return ""
			}
			line := strings.TrimSpace(string(b))
			rest, ok := strings.CutPrefix(line, "gitdir:")
			if !ok {
				return ""
			}
			g := strings.TrimSpace(rest)
			if !filepath.IsAbs(g) {
				g = filepath.Join(d, g)
			}
			// worktree 의 gitdir 은 <main>/.git/worktrees/<name> 이고 config 는
			// 거기 없다. commondir 이 본체를 가리킨다.
			if b, err := os.ReadFile(filepath.Join(g, "commondir")); err == nil {
				c := strings.TrimSpace(string(b))
				if !filepath.IsAbs(c) {
					c = filepath.Join(g, c)
				}
				return filepath.Clean(c)
			}
			return g
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "" // 루트까지 갔다
		}
		d = parent
	}
}

// originURL 은 git config 에서 remote "origin" 의 url 을 뽑는다.
//
// go-git 같은 의존성을 들이지 않는다 — 이 프로젝트는 런타임 의존성 0 이 규칙이고(D1),
// 우리가 필요한 것은 한 절의 한 줄뿐이다.
func originURL(r interface{ Read([]byte) (int, error) }) string {
	sc := bufio.NewScanner(r)
	inOrigin := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			// 절 제목. 공백 표기가 여러 가지다: [remote "origin"] · [remote"origin"]
			inOrigin = strings.HasPrefix(line, "[remote") && strings.Contains(line, `"origin"`)
			continue
		}
		if !inOrigin {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "url" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// NormalizeRemote 는 remote URL 을 `owner/repo` 로 줄인다. 아니면 빈 문자열.
//
// 같은 저장소가 형태를 여러 가지로 쓴다 — 사람마다 https 와 ssh 가 갈리는데
// **그건 도메인을 가를 이유가 아니다.**
//
//	https://github.com/o/r.git   → o/r
//	git@github.com:o/r.git       → o/r
//	ssh://git@github.com/o/r     → o/r
//	https://gitlab.com/g/s/p.git → g/s/p   (하위 그룹은 살린다)
//
// 소문자로 맞춘다. GitHub 은 대소문자를 구별하지 않는데 사람은 섞어 적는다.
func NormalizeRemote(url string) string {
	u := strings.TrimSpace(url)
	if u == "" {
		return ""
	}
	// scp 형식: git@host:owner/repo
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.Index(u, "/"); j >= 0 {
			u = u[j+1:] // host 를 버린다
		} else {
			return ""
		}
	} else if i := strings.Index(u, ":"); i >= 0 && !strings.HasPrefix(u, "/") {
		u = u[i+1:]
	}
	if i := strings.Index(u, "@"); i >= 0 && !strings.Contains(u[:i], "/") {
		u = u[i+1:] // 남아 있는 user@ 제거
	}
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	u = strings.TrimPrefix(u, "/")
	if u == "" || !strings.Contains(u, "/") {
		return "" // owner 가 없으면 저장소 이름이 아니다
	}
	return strings.ToLower(u)
}

// GitUser 는 dir 에서 통하는 git 신원을 `이름 <메일>` 로 준다. 없으면 빈 문자열.
//
// **기본값이 없으면 author 는 안 쓰인다.** 매번 `--author` 를 붙이라고 하면 아무도
// 안 붙이고, 그러면 팀에서 "누가 정했나" 가 영원히 비어 있다. git 을 쓰는 사람은
// 이미 신원을 적어 뒀으므로 그걸 쓴다.
//
// git 은 저장소 설정 → 전역 설정 순으로 본다. 여기도 같은 순서다 — 회사 저장소에서만
// 회사 메일을 쓰는 사람이 흔하고, 그 사람의 결정에 개인 메일이 박히면 안 된다.
func GitUser(dir string) string {
	name, email := "", ""
	take := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		n, e := userSection(f)
		if name == "" {
			name = n
		}
		if email == "" {
			email = e
		}
	}
	if g := findGitDir(dir); g != "" {
		take(filepath.Join(g, "config"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		take(filepath.Join(home, ".gitconfig"))
		take(filepath.Join(home, ".config", "git", "config"))
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		take(filepath.Join(x, "git", "config"))
	}

	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case name != "":
		return name
	case email != "":
		return email
	}
	return ""
}

// userSection 은 git config 의 [user] 절에서 name·email 을 뽑는다.
func userSection(r interface{ Read([]byte) (int, error) }) (name, email string) {
	sc := bufio.NewScanner(r)
	in := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexAny(line, "#;"); i == 0 {
			continue
		}
		if strings.HasPrefix(line, "[") {
			in = strings.HasPrefix(line, "[user")
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "name":
			if name == "" {
				name = strings.Trim(strings.TrimSpace(v), `"`)
			}
		case "email":
			if email == "" {
				email = strings.Trim(strings.TrimSpace(v), `"`)
			}
		}
	}
	return name, email
}

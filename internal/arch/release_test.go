package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ★★ 아카이브 이름을 유추에 맡기면 조용히 바뀐다.
//
// 한때 릴리스를 별도 저장소로 보내려고 `release.github.name` 을 적었더니 goreleaser 가
// 그것으로 project_name 을 유추해 `priorcase-releases_darwin_arm64.tar.gz` 를 만들었다.
// 빌드도 테스트도 `goreleaser check` 도 통과하고 **다운로드 링크만 죽는** 종류다.
//
// 지금은 npm 이 배포 경로라 그 링크가 README 에 없지만, `scripts/npm-pack.sh` 가
// 이 파일명에 의존한다 — 이름이 바뀌면 npm 패키징이 통째로 깨진다.
func TestArchiveNameIsPinned(t *testing.T) {
	root := repoRoot(t)
	cfg := readFile(t, filepath.Join(root, ".goreleaser.yaml"))
	if !regexp.MustCompile(`(?m)^project_name:\s*priorcase\s*$`).MatchString(cfg) {
		t.Error(".goreleaser.yaml 에 `project_name: priorcase` 이 없다 — " +
			"저장소 이름이 파일명에 새어 들어간다")
	}
	if !strings.Contains(cfg, `name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"`) {
		t.Error("아카이브 name_template 이 바뀌었다")
	}
	// npm-pack.sh 가 기대하는 이름과 실제로 맞는지 본다.
	pack := readFile(t, filepath.Join(root, "scripts", "npm-pack.sh"))
	if !strings.Contains(pack, `priorcase_${goos}_${goarch}.tar.gz`) {
		t.Error("npm-pack.sh 가 기대하는 아카이브 이름이 goreleaser 와 어긋난다")
	}
}

// 배포물에 EULA 와 제3자 고지가 반드시 들어가야 한다. 독점 소프트웨어라 라이선스
// 동봉이 의무이고, 링크된 오픈소스의 고지 의무도 여기서 진다.
func TestReleaseArchiveCarriesLicenses(t *testing.T) {
	root := repoRoot(t)
	cfg := readFile(t, filepath.Join(root, ".goreleaser.yaml"))
	// **줄 전체로 본다.** 부분 문자열로 보면 `- LICENSE.x` 가 `- LICENSE` 에
	// 걸려서 통과한다 — 회수 매칭에서 고친 것과 같은 종류의 버그다.
	lines := map[string]bool{}
	for _, l := range strings.Split(cfg, "\n") {
		lines[strings.TrimRight(l, " \t\r")] = true
	}
	for _, f := range []string{"LICENSE", "THIRD-PARTY-NOTICES.md"} {
		if !lines["      - "+f] {
			t.Errorf("아카이브에 %s 를 안 넣는다 — 라이선스 위반이다", f)
		}
		body := readFile(t, filepath.Join(root, f))
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s 가 비었다", f)
		}
	}
}

// 소스를 공개하지 않기로 했으므로 LICENSE 가 MIT 로 되돌아가면 안 된다.
// 되돌아가면 배포물에 "누구나 재배포 가능" 이 동봉된다.
func TestLicenseIsProprietary(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "LICENSE"))
	if !strings.Contains(body, "proprietary software") {
		t.Error("LICENSE 가 독점 라이선스가 아니다 — 소스 비공개 배포와 어긋난다")
	}
	if strings.Contains(body, "MIT License") {
		t.Error("LICENSE 가 MIT 로 되돌아갔다")
	}
}

// README 가 `go install` 을 다시 권하면 안 된다 — 소스가 private 이라 죽는 명령이다.
// 그리고 npm 이 1차 경로라는 것이 설치 절에 있어야 한다.
func TestReadmeInstallPathIsNpm(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "README.md"))
	install := body
	if i := strings.Index(body, "## 설치"); i >= 0 {
		install = body[i:]
		if j := strings.Index(install[6:], "\n## "); j >= 0 {
			install = install[:j+6]
		}
	}
	if strings.Contains(install, "go install github.com/") {
		t.Error("설치 절이 `go install` 을 권한다 — 소스가 private 이라 낯선 사람에게는 죽는다")
	}
	for _, want := range []string{"npm install -g priorcase", "npx", "mcpServers"} {
		if !strings.Contains(install, want) {
			t.Errorf("설치 절에 %q 가 없다 — npm 이 1차 경로다", want)
		}
	}
	// 훅이 npx 로 안 된다는 사실을 반드시 적어야 한다. 안 적으면 사용자가
	// mcpServers 만 걸고 훅이 도는 줄 안다.
	if !strings.Contains(install, "npx` 로는 훅이 안 된다") {
		t.Error("npx 로 훅이 안 된다는 경고가 없다 — 사용자가 자동 기록이 도는 줄 안다")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return string(b)
}

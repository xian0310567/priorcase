// Package testutil 은 테스트용 볼트 픽스처를 만든다.
// store 를 import 하지 않는다 — store 의 테스트가 이걸 쓰면 순환이 되기 때문이다.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
)

// VaultConfig 는 testdata/vault 를 임시 디렉토리로 복사하고 그것을 가리키는 설정을 준다.
// 도메인 경로는 /tmp/proj/<name> 으로 고정해 cwd 기반 테스트가 결정적이게 한다.
func VaultConfig(t *testing.T) *config.Config {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(fixtureSrc(t))); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		Vault: dst,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Paths: []string{"/tmp/proj/alpha"}},
			{Prefix: "beta", Folder: "beta", Paths: []string{"/tmp/proj/beta"}},
			{Prefix: "common", Folder: "common"},
		},
	}
}

// fixtureSrc 는 이 파일의 위치에서 저장소 루트를 거슬러 testdata/vault 를 찾는다.
// 상대 경로("../../testdata")를 쓰면 호출하는 패키지의 깊이에 따라 깨진다.
func fixtureSrc(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("호출자 정보를 얻을 수 없다")
	}
	// self = <repo>/internal/testutil/vault.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	return filepath.Join(root, "testdata", "vault")
}

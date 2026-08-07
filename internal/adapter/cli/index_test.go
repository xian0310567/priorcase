package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// fixtureNoteCount 는 픽스처 볼트(testdata/vault)의 결정 노트 수다.
// `cb index` 가 보고하는 행 수의 기대값이므로, 픽스처에 노트를 더하면 여기도 는다.
const fixtureNoteCount = 4

// TestIndexCmd 는 `cb index` 가 --config 로 지정된 설정 파일을 읽어 색인을 실제로
// 만들고, "색인 N행 생성" 형식으로 행 수를 보고하는지 확인한다.
func TestIndexCmd(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"index", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb index 실행 실패: %v", err)
	}

	want := fmt.Sprintf("색인 %d행 생성\n", fixtureNoteCount)
	if got := buf.String(); got != want {
		t.Errorf("출력 = %q, want %q", got, want)
	}

	idxPath := filepath.Join(c.Vault, c.Naming.Index)
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("색인 파일이 생기지 않았다 (%s): %v", idxPath, err)
	}
	if !strings.Contains(string(data), "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("색인 내용에 픽스처 노트의 summary 가 없다:\n%s", data)
	}
}

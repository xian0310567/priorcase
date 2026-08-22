package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// ★ **상태 디렉토리를 모를 때 작업 디렉토리를 더럽히면 안 된다.**
//
// filepath.Join("", "watch.lock") 은 "watch.lock" 이라 flock 이 **지금 있는 곳에**
// 파일을 만든다. 실제로 이것이 레포에 커밋될 뻔했다 — prior doctor 를 프로젝트
// 디렉토리에서 돌리면 거기에 락 파일이 떨어진다.
func TestIsRunningDoesNotTouchCwdWithoutStateDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if IsRunning("") {
		t.Error("상태 디렉토리를 모르는데 데몬이 돈다고 한다")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Errorf("작업 디렉토리에 %q 를 만들었다", filepath.Join(dir, e.Name()))
	}
}

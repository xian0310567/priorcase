package store

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic 는 path 에 data 를 원자적으로 쓴다.
//
// os.WriteFile 은 O_TRUNC 로 기존 파일을 먼저 비운 뒤 새 내용을 쓰기 때문에,
// 쓰는 도중 실패하면(디스크 풀, 파일 크기 제한, 프로세스 강제 종료 등) 기존
// 유효한 내용도 완전한 새 내용도 아닌 잘린 파일이 남는다. 여기서는 대상과
// 같은 디렉토리에 임시 파일을 만들어 내용을 다 쓴 뒤 os.Rename 으로 교체한다
// — 같은 파일시스템 안에서 rename 은 원자적이므로 중간 상태가 밖에서 보이지
// 않고, 실패해도 기존 파일이 그대로 남는다.
//
// 임시 파일을 대상과 같은 디렉토리에 두는 이유: 다른 파일시스템(예: /tmp 가
// 별도 마운트인 경우)이면 os.Rename 이 EXDEV 로 실패한다.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			// 실패 경로 — 남은 임시 파일을 지운다. 이미 지워졌거나
			// (예: 나중 단계에서 실패) 존재하지 않아도 조용히 무시한다.
			_ = os.Remove(tmpPath)
		}
	}()

	if _, werr := tmp.Write(data); werr != nil {
		tmp.Close()
		return werr
	}
	if cerr := tmp.Chmod(perm); cerr != nil {
		tmp.Close()
		return cerr
	}
	if cerr := tmp.Close(); cerr != nil {
		return cerr
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		return rerr
	}
	renamed = true
	return nil
}

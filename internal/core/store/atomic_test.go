package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicWritesContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")

	if err := WriteFileAtomic(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("내용이 다르다: got %q, want %q", got, "hello")
	}
}

// TestWriteFileAtomicOverwritesExisting 은 대상 자리에 이미 파일이 있어도
// 정상적으로 덮어써지고 그 결과가 새 내용인지 확인한다.
func TestWriteFileAtomicOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(p, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-content" {
		t.Errorf("덮어쓰기 후 내용이 새것이 아니다: got %q, want %q", got, "new-content")
	}
}

// TestWriteFileAtomicNoLeftoverTempFile 은 정상 쓰기 후 디렉토리에 임시
// 파일이 남지 않고 대상 파일 하나만 있는지 확인한다.
func TestWriteFileAtomicNoLeftoverTempFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")

	if err := WriteFileAtomic(p, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.txt" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("디렉토리에 예상 밖 항목이 있다(임시 파일이 남았을 수 있다): %v", names)
	}
}

// TestWriteFileAtomicPreservesExistingOnFailure 는 쓰기가 실패했을 때 기존
// 파일이 잘리거나 사라지지 않고 그대로 남는지 확인한다 — 원자적 쓰기가
// 존재하는 핵심 이유다.
//
// 대상 디렉토리를 읽기전용(0o555)으로 만들어 실패를 유도한다. 이러면
// os.CreateTemp 가 디렉토리 안에 새 항목을 만들지 못해 실패한다(디렉토리
// 쓰기 권한이 필요). 반대로 이미 존재하는 파일을 O_TRUNC 로 여는 것은
// 디렉토리 쓰기 권한이 아니라 파일 자체의 쓰기 권한만 있으면 되므로(직접
// 확인함: 같은 조건에서 os.WriteFile 은 성공해 버린다), 이 실패 유도 방식은
// "정상 쓰기 도중 실패"를 만드는 방법이지 그냥 통과 못 하게 막는 방법이
// 아니다 — 아래 뮤테이션 검증(리뷰 브리프)이 이걸 확인한다.
func TestWriteFileAtomicPreservesExistingOnFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	const original = "original-content-must-survive"
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// TempDir 정리가 디렉토리 안 항목을 지우려면 쓰기 권한이 필요하므로,
	// 테스트가 어떻게 끝나든 반드시 원복한다.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	writeErr := WriteFileAtomic(p, []byte("new-content-that-must-not-land"), 0o644)

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 원자적 구현이라면 디렉토리에 새 파일을 못 만들어 반드시 에러를 내야
	// 한다. 에러가 안 났다면(예: os.WriteFile 로 되돌린 뮤테이션) 아래
	// 내용 비교가 그 사실을 드러낸다.
	if writeErr == nil {
		t.Errorf("읽기전용 디렉토리에서도 쓰기가 성공했다 — 원자적 구현이 아닐 수 있다")
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("기존 파일을 다시 읽을 수 없다: %v", err)
	}
	if string(got) != original {
		t.Errorf("실패한 쓰기가 기존 파일을 훼손했다: got %q, want %q", got, original)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.txt" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("실패 후 디렉토리에 임시 파일이 남았거나 예상 밖 항목이 있다: %v", names)
	}
}

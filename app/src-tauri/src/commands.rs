use crate::prior::{run, PriorError};
use serde::Serialize;
use std::time::Duration;

/// READ_TIMEOUT 은 `prior queue` 의 상한이다.
///
/// 실측 1.0초(결정 98건 · 볼트 하나)다. 볼트가 늘면 곱해지므로 여유를 준다.
/// 이보다 오래 걸리면 앱이 멎은 것처럼 보이므로 짧게 잡는다.
pub const READ_TIMEOUT: Duration = Duration::from_secs(10);

/// WRITE_TIMEOUT 은 쓰기 명령의 상한이다.
///
/// **`prior promote` 가 판별기를 부르고 그 예산이 90초다.** 읽기 상한을 쓰면
/// 정상 승격이 타임아웃으로 보이고, 사람은 눌렀는데 아무 일도 안 난 줄 안다.
pub const WRITE_TIMEOUT: Duration = Duration::from_secs(120);

/// ENV_LOCK 은 환경변수를 만지는 테스트를 직렬화한다.
///
/// 테스트는 한 프로세스에서 병렬로 돈다 — 한쪽이 PRIORCASE_APP_BIN 을 지우는 동안
/// 다른 쪽이 그것을 읽으면 깜빡인다. **깜빡이는 테스트는 신호를 잃는다.**
#[cfg(test)]
pub(crate) static ENV_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());

/// CmdError 는 프런트가 **종류별로 다른 화면**을 그리기 위한 것이다.
///
/// kind 를 문자열로 내보내는 이유: 프런트가 한국어 문구를 파싱하게 하면 문구를
/// 고치는 순간 화면이 조용히 깨진다. 값 넷은 프런트와의 계약이다.
#[derive(Debug, Serialize)]
pub struct CmdError {
    pub kind: String,
    pub message: String,
}

pub fn to_cmd_error(e: PriorError) -> CmdError {
    let kind = match &e {
        PriorError::NotFound => "not_found",
        PriorError::Failed { .. } => "failed",
        PriorError::Timeout => "timeout",
        PriorError::Io(_) => "io",
    };
    CmdError {
        kind: kind.to_string(),
        message: e.to_string(),
    }
}

/// prior_bin 은 부를 바이너리 경로다.
///
/// 환경변수로 덮을 수 있게 두는 이유는 **테스트다** — 가짜 prior 로 각 오류 화면을
/// 재현하는 유일한 문이다. 기본은 PATH 의 prior 다.
pub fn prior_bin() -> String {
    if let Ok(p) = std::env::var("PRIORCASE_APP_BIN") {
        if !p.is_empty() {
            return p;
        }
    }
    "prior".to_string()
}

pub fn run_queue(bin: &str) -> Result<String, CmdError> {
    run(bin, &["queue", "--json"], READ_TIMEOUT).map_err(to_cmd_error)
}

#[tauri::command]
pub fn queue() -> Result<String, CmdError> {
    run_queue(&prior_bin())
}

#[tauri::command]
pub fn resolve_pending(id: String) -> Result<(), CmdError> {
    run(&prior_bin(), &["pending", "--resolve", &id], WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
}

#[tauri::command]
pub fn promote(id: String) -> Result<String, CmdError> {
    run(&prior_bin(), &["promote", &id, "--json"], WRITE_TIMEOUT).map_err(to_cmd_error)
}

/// mark_reviewed 는 "판별기가 사실대로 썼다" 는 검증 표시다.
///
/// **review(outcome) 과 다른 명령이다.** outcome 은 "그 결정이 결과적으로
/// 좋았나" 이고 회고 큐가 그 값이 정해진 노트를 영영 제외한다. 검토는 다른
/// 질문이므로 다른 자리에 남긴다 — 안 그러면 노트를 검증했을 뿐인데 나중에
/// 결과를 묻는 자리가 조용히 사라진다.
#[tauri::command]
pub fn mark_reviewed(id: String) -> Result<(), CmdError> {
    run(&prior_bin(), &["reviewed", &id], WRITE_TIMEOUT)
        .map(|_| ())
        .map_err(to_cmd_error)
}

/// path 는 결정 노트의 절대 경로를 준다.
///
/// **앱이 볼트 경로를 조립하지 않는다.** 조립하면 볼트 선택 규칙이 둘이 되고,
/// 다중 볼트에서 그 어긋남은 엉뚱한 파일을 열거나 못 여는 것으로 나타난다.
#[tauri::command]
pub fn note_path(stem: String) -> Result<String, CmdError> {
    run(&prior_bin(), &["path", &stem], READ_TIMEOUT).map_err(to_cmd_error)
}

/// open_note 는 노트를 OS 기본 앱으로 연다.
///
/// **셸을 거치지 않는다.** 볼트 경로에 공백이 있다 (실측: `Obsidian Vault`).
/// 셸 문자열로 만들면 거기서 쪼개지고, 따옴표로 감싸는 순간 경로에 따옴표가
/// 있는 경우가 뚫린다. 인자로 넘기면 그런 자리가 없다.
#[tauri::command]
pub fn open_note(stem: String) -> Result<(), CmdError> {
    let p = note_path(stem)?;
    let p = p.trim();
    if p.is_empty() {
        return Err(CmdError {
            kind: "failed".into(),
            message: "prior path 가 빈 줄을 냈다".into(),
        });
    }
    let opener = if cfg!(target_os = "macos") {
        "open"
    } else if cfg!(target_os = "windows") {
        "explorer"
    } else {
        "xdg-open"
    };
    run(opener, &[p], READ_TIMEOUT).map(|_| ()).map_err(to_cmd_error)
}

#[tauri::command]
pub fn review(stem: String, outcome: String) -> Result<(), CmdError> {
    run(
        &prior_bin(),
        &["review", &stem, "--outcome", &outcome],
        WRITE_TIMEOUT,
    )
    .map(|_| ())
    .map_err(to_cmd_error)
}

/// set_tray_title 은 메뉴바 아이콘 옆의 글자를 바꾼다.
///
/// **푸시 알림을 보내지 않는다.** 먼저 말을 거는 도구는 지워지고, 앱이 지워지면
/// 자동 기록까지 같이 잃는다. 숫자만 조용히 바꾼다.
///
/// 빈 문자열이면 글자를 아예 뗀다 — 할 일이 없을 때 "0" 이 떠 있으면 그것도
/// 늘 켜진 신호가 되어, 사람이 배지를 무시하는 법을 배운다.
#[tauri::command]
pub fn set_tray_title(app: tauri::AppHandle, title: String) {
    if let Some(tray) = app.tray_by_id("main") {
        let _ = tray.set_title(if title.is_empty() { None } else { Some(title) });
    }
}

#[cfg(test)]
#[allow(non_snake_case)]
mod tests {
    use super::*;

    fn fixture(name: &str) -> String {
        let mut p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        p.pop();
        p.push("fixtures");
        p.push(name);
        p.to_string_lossy().into_owned()
    }

    // ★★ **오류 종류가 문자열로 나가야 한다.**
    //
    // 프런트가 종류별로 다른 화면을 그리려면 kind 가 필요하다. Display 문자열만
    // 주면 프런트가 한국어 문구를 파싱하게 되고, **문구를 고치는 순간 화면이
    // 조용히 깨진다.**
    #[test]
    fn 오류_종류가_문자열로_나간다() {
        let e = to_cmd_error(crate::prior::PriorError::NotFound);
        assert_eq!(e.kind, "not_found");

        let e = to_cmd_error(crate::prior::PriorError::Timeout);
        assert_eq!(e.kind, "timeout");

        let e = to_cmd_error(crate::prior::PriorError::Io("파이프".into()));
        assert_eq!(e.kind, "io");

        let e = to_cmd_error(crate::prior::PriorError::Failed {
            code: Some(1),
            stderr: "설정을 읽을 수 없다".into(),
        });
        assert_eq!(e.kind, "failed");
        // stderr 가 message 에 살아 있어야 한다 — 사람이 고칠 유일한 단서다.
        assert!(
            e.message.contains("설정을 읽을 수 없다"),
            "message={}",
            e.message
        );
    }

    // ★★ **kind 는 프런트와의 계약이다.** 값이 바뀌면 화면이 조용히 안 그려진다.
    #[test]
    fn kind_값_넷이_고정이다() {
        let kinds: Vec<String> = vec![
            crate::prior::PriorError::NotFound,
            crate::prior::PriorError::Failed {
                code: None,
                stderr: String::new(),
            },
            crate::prior::PriorError::Timeout,
            crate::prior::PriorError::Io(String::new()),
        ]
        .into_iter()
        .map(|e| to_cmd_error(e).kind)
        .collect();
        assert_eq!(kinds, vec!["not_found", "failed", "timeout", "io"]);
    }

    #[test]
    fn queue_는_stdout_을_그대로_준다() {
        let out = run_queue(&fixture("fake-prior-ok.sh")).expect("성공해야 한다");
        assert!(out.contains("\"confirm\""), "out={out}");
    }

    // ★★ **쓰기 상한이 읽기 상한보다 훨씬 길어야 한다.**
    //
    // `prior promote` 는 판별기를 부르고 그 예산이 90초다. 읽기 상한(10초)을 쓰면
    // **정상 승격이 타임아웃으로 보인다** — 사람은 눌렀는데 아무 일도 안 난 줄 안다.
    #[test]
    fn 쓰기_상한이_판별기_예산을_담는다() {
        assert!(
            WRITE_TIMEOUT.as_secs() >= 90,
            "쓰기 상한={:?} — prior promote 의 판별기 예산 90초를 못 담는다",
            WRITE_TIMEOUT
        );
        assert!(
            READ_TIMEOUT.as_secs() <= 15,
            "읽기 상한={:?} — 큐가 이만큼 걸리면 앱이 멎은 것처럼 보인다",
            READ_TIMEOUT
        );
        assert!(
            WRITE_TIMEOUT > READ_TIMEOUT,
            "쓰기({WRITE_TIMEOUT:?})가 읽기({READ_TIMEOUT:?})보다 짧다"
        );
    }

    // ★★ **바이너리 경로를 환경변수로 덮을 수 있어야 한다.**
    //
    // 가짜 prior 로 각 오류 화면을 재현하는 유일한 문이다. 이게 없으면 오류 처리를
    // 손으로만 확인하게 되고, 그건 회귀를 못 잡는다.
    #[test]
    fn 바이너리_경로를_환경변수로_덮는다() {
        // 이 테스트만 환경변수를 만지므로 다른 테스트와 겹치지 않게 한다.
        let _g = ENV_LOCK.lock().unwrap();
        let saved = std::env::var("PRIORCASE_APP_BIN").ok();

        std::env::set_var("PRIORCASE_APP_BIN", "/어떤/자리/prior");
        assert_eq!(prior_bin(), "/어떤/자리/prior");

        std::env::remove_var("PRIORCASE_APP_BIN");
        assert_eq!(prior_bin(), "prior", "기본은 PATH 의 prior 여야 한다");

        if let Some(v) = saved {
            std::env::set_var("PRIORCASE_APP_BIN", v);
        }
    }

    // ★★ **빈 경로로 OS 를 부르면 안 된다.**
    //
    // prior path 가 어떤 이유로든 빈 줄을 내면(설정 오류·빈 볼트), 그대로
    // open 에 넘기면 OS 가 **현재 디렉토리를 연다** — 사람은 왜 파인더가 떴는지
    // 모른다. 여기서 끊고 무엇이 문제인지 말한다.
    #[test]
    fn 빈_경로로는_열지_않는다() {
        let _g = ENV_LOCK.lock().unwrap();
        let saved = std::env::var("PRIORCASE_APP_BIN").ok();
        std::env::set_var("PRIORCASE_APP_BIN", fixture("fake-prior-empty.sh"));

        let e = open_note("무엇이든".into()).expect_err("실패해야 한다");
        assert_eq!(e.kind, "failed");
        assert!(e.message.contains("빈 줄"), "message={}", e.message);

        match saved {
            Some(v) => std::env::set_var("PRIORCASE_APP_BIN", v),
            None => std::env::remove_var("PRIORCASE_APP_BIN"),
        }
    }

    // ★ 새 커맨드 둘도 인자를 그대로 넘겨야 한다. 이름이 어긋나면 조용히 안 먹는다.
    #[test]
    fn 새_커맨드들이_인자를_그대로_넘긴다() {
        let echo = fixture("fake-prior-echo-args.sh");
        let out = crate::prior::run(&echo, &["reviewed", "/경로 with space.jsonl@42"], READ_TIMEOUT)
            .expect("성공해야 한다");
        assert!(out.contains("reviewed"), "out={out}");
        assert!(out.contains("/경로 with space.jsonl@42"), "인자가 쪼개졌다: {out}");

        let out = crate::prior::run(&echo, &["path", "priorcase-결정-x-2026-08-13"], READ_TIMEOUT)
            .expect("성공해야 한다");
        assert!(out.contains("path"), "out={out}");
        assert!(out.contains("priorcase-결정-x-2026-08-13"), "out={out}");
    }

    // ★★★ **가짜 prior 가 진짜 하위 프로세스 경로를 통과해야 한다.**
    //
    // TS 쪽 시험은 픽스처의 stdout 을 파일에서 읽어 검사한다 — 그건 **JSON 이
    // 맞는가**를 본다. 여기서는 그 바이트가 실제로 spawn · 파이프 · 상한을 거쳐
    // 우리 손에 오는지를 본다. 둘은 다른 것이고, 이 경로에서만 나는 고장이
    // 있다 (파이프 교착 · 좀비 · 상한).
    #[test]
    fn 큐_픽스처들이_실제_실행_경로를_통과한다() {
        for name in ["ok", "warnings", "onebroken"] {
            let out = run_queue(&fixture(&format!("fake-prior-{name}.sh")))
                .unwrap_or_else(|e| panic!("{name}: {} ({})", e.message, e.kind));
            let v: serde_json::Value =
                serde_json::from_str(&out).unwrap_or_else(|e| panic!("{name}: JSON 아님 {e}"));
            for k in ["confirm", "review", "retro", "health"] {
                assert!(v[k].is_array(), "{name}: {k} 가 배열이 아니다");
            }
        }
    }

    // ★★ **깨진 JSON 은 성공으로 온다 — 실패로 오지 않는다.**
    //
    // prior 가 종료코드 0 으로 쓰레기를 내는 판이 있다 (패닉 뒤 부분 출력).
    // Rust 층은 그걸 그대로 넘기고, **판정은 api.ts 가 한다.** 여기서 미리
    // 실패로 바꾸면 "실행이 안 됐다" 와 "출력이 깨졌다" 가 같아 보인다.
    #[test]
    fn 깨진_JSON_은_Rust_층에서_성공으로_온다() {
        let out = run_queue(&fixture("fake-prior-broken-json.sh")).expect("종료코드는 0 이다");
        assert!(
            serde_json::from_str::<serde_json::Value>(&out).is_err(),
            "픽스처가 실제로 깨진 JSON 을 내야 한다: {out}"
        );
    }

    // ★ 인자를 그대로 넘기는지 본다. id 에 공백·한글이 섞여도 깨지면 안 된다.
    #[test]
    fn 인자를_그대로_넘긴다() {
        let echo = fixture("fake-prior-echo-args.sh");
        let out = crate::prior::run(
            &echo,
            &["pending", "--resolve", "/경로 with space.jsonl@42"],
            READ_TIMEOUT,
        )
        .expect("성공해야 한다");
        assert!(out.contains("--resolve"), "out={out}");
        assert!(
            out.contains("/경로 with space.jsonl@42"),
            "인자가 쪼개졌다: {out}"
        );
    }
}
